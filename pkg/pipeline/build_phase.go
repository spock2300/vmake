package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type BuildResult struct {
	AllTargets    map[string]map[string]*api.Target
	Graph         *build.BuildGraph
	PkgDirs       map[string]*api.PkgDirs
	PkgBuildKeys  map[string]string
	TcName        string
	Mode          string
	InstalledPkgs map[string]*api.InstalledPackage
	BuildCtxs     map[string]*api.BuildContext
}

type buildPhaseState struct {
	ctx             *RuntimeContext
	includeTests    bool
	jobs            int
	keepGoing       bool
	globalFlagsHash string

	cfg           *buildConfig
	needed        map[string]bool
	pkgDirs       map[string]*api.PkgDirs
	remote        *remoteVersionState
	patchHashes   map[string]string
	scriptHashes  map[string]string
	allPkgOptions map[string]map[string]any
	allTargets    map[string]map[string]*api.Target
	pkgMetaMap    map[string]build.PkgBuildMeta

	subGraphBuilt map[string]bool
	buildCtxs     map[string]*api.BuildContext
}

type remoteVersionState struct {
	entries     map[string]*config.EntryConfig
	commits     map[string]string
	versionDirs map[string]string
}

type BuildOptions struct {
	IncludeTests bool
	Jobs         int
	KeepGoing    bool
}

func newBuildPhaseState(ctx *RuntimeContext, opts BuildOptions) *buildPhaseState {
	return &buildPhaseState{
		ctx:           ctx,
		includeTests:  opts.IncludeTests,
		jobs:          opts.Jobs,
		keepGoing:     opts.KeepGoing,
		patchHashes:   make(map[string]string),
		scriptHashes:  make(map[string]string),
		subGraphBuilt: make(map[string]bool),
	}
}

func (s *buildPhaseState) scriptHashFor(name string) (string, error) {
	if h, ok := s.scriptHashes[name]; ok {
		return h, nil
	}
	h, err := scriptHashForNode(name, s.ctx.DepGraph.Packages[name])
	if err != nil {
		return "", err
	}
	if h == "" {
		return "", nil
	}
	s.scriptHashes[name] = h
	return h, nil
}

func RunBuild(ctx *RuntimeContext, opts BuildOptions) (*BuildResult, error) {
	s := newBuildPhaseState(ctx, opts)

	if err := s.resolveBuildConfig(); err != nil {
		return nil, err
	}

	if err := s.filterNeeded(); err != nil {
		return nil, err
	}

	applyGlobalFlagsFromNeeded(ctx, s.needed)

	s.globalFlagsHash = build.GlobalFlagsHash()

	s.computeDirsAndOptions()

	if err := s.prepareAllPackages(); err != nil {
		return nil, err
	}

	if err := s.writeLockfile(); err != nil {
		return nil, err
	}

	if err := s.applyPatchesToNeeded(); err != nil {
		return nil, err
	}

	if err := s.restoreKConfigs(); err != nil {
		return nil, err
	}

	s.executeOnBuild()

	s.logResults()

	return s.buildAndRunPipeline()
}

func UpdateLock(ctx *RuntimeContext) error {
	s := newBuildPhaseState(ctx, BuildOptions{})

	if err := s.resolveBuildConfig(); err != nil {
		return err
	}

	if err := s.filterNeeded(); err != nil {
		return err
	}

	applyGlobalFlagsFromNeeded(ctx, s.needed)

	s.globalFlagsHash = build.GlobalFlagsHash()

	s.computeDirsAndOptions()

	if err := s.prepareAllPackages(); err != nil {
		return err
	}

	return s.writeLockfile()
}

func (s *buildPhaseState) resolveBuildConfig() error {
	cfg, err := resolveBuildConfig(s.ctx)
	if err != nil {
		return err
	}
	s.cfg = cfg
	return nil
}

func resolveBuildConfig(ctx *RuntimeContext) (*buildConfig, error) {
	mode := ResolveMode(ctx.Config, ctx.ModeOverride)

	tc, tcName, err := GetToolchain(ctx.Config, ctx.ToolchainOverride)
	if err != nil {
		return nil, err
	}

	globalValues := config.BuildGlobalValues(ctx.Config)
	if globalValues[api.ModeOptionName] == "" || globalValues[api.ModeOptionName] == nil {
		globalValues[api.ModeOptionName] = mode
	}
	if globalValues["toolchain"] == "" || globalValues["toolchain"] == nil {
		globalValues["toolchain"] = tcName
	}

	return &buildConfig{
		Mode:         mode,
		TcName:       tcName,
		Tc:           tc,
		GlobalValues: globalValues,
	}, nil
}

func (s *buildPhaseState) filterNeeded() error {
	needed, err := filterAndCollectNeeded(s.ctx)
	if err != nil {
		return err
	}
	s.needed = needed
	return nil
}

func filterAndCollectNeeded(ctx *RuntimeContext) (map[string]bool, error) {
	vlog.Info("")
	vlog.Info("Filtering dependencies...")
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.Pkg == nil {
			continue
		}
		entry := config.GetEntry(ctx.Config, name)
		opts := ctx.AllOptions[name]
		if err := ctx.Resolver.FilterDeps(name, entry.Options, opts); err != nil {
			return nil, fmt.Errorf("%s: filter deps: %w", name, err)
		} else if len(node.Deps) > 0 {
			vlog.Info("  %s: deps=%v", name, node.Deps)
		}
	}
	if err := ctx.Resolver.UpdateOrder(); err != nil {
		return nil, fmt.Errorf("dependency cycle: %w", err)
	}

	ctx.DepGraph.Freeze()

	needed, err := computeReachable(ctx.DepGraph)
	if err != nil {
		return nil, err
	}

	return needed, nil
}

func (s *buildPhaseState) computeDirsAndOptions() {
	s.allPkgOptions = collectAllPkgOptions(s.ctx, s.needed)
	s.pkgDirs = ResolveAllPackageDirs(s.ctx.DepGraph)
}

func (s *buildPhaseState) prepareAllPackages() error {
	remote := &remoteVersionState{
		entries:     make(map[string]*config.EntryConfig),
		commits:     make(map[string]string),
		versionDirs: make(map[string]string),
	}

	resolvedTools, err := build.ResolveTools(s.cfg.Tc)
	if err != nil {
		return fmt.Errorf("resolve tools: %w", err)
	}

	for _, name := range s.ctx.Resolver.GetOrder() {
		if !s.needed[name] {
			continue
		}
		if !s.ctx.DepGraph.Packages[name].IsLocal() {
			remote.entries[name] = config.GetEntry(s.ctx.Config, name)
		}
	}

	for _, name := range s.ctx.Resolver.GetOrder() {
		node := s.ctx.DepGraph.Packages[name]
		if !s.needed[name] || node.Source == nil || !node.IsLocal() {
			continue
		}
		opts := s.allPkgOptions[name]
		scriptHash, err := s.scriptHashFor(name)
		if err != nil {
			return err
		}
		s.pkgDirs[name] = makeLocalPkgDirs(node.Source.Dir, resolvedTools.CCKey(), s.cfg.Mode, opts, s.globalFlagsHash, scriptHash)
	}

	depsDir := s.ctx.Paths.DepsDir

	vlog.Info("")
	vlog.Info("Downloading package sources...")

	if err := s.downloadRemoteSources(remote, depsDir, resolvedTools.CCKey()); err != nil {
		return err
	}

	if err := s.setupSubPackageDirs(depsDir); err != nil {
		return err
	}

	if err := s.cloneLocalGitSources(); err != nil {
		return err
	}

	if err := s.cloneSubPackageGitSources(); err != nil {
		return err
	}

	s.remote = remote
	return nil
}

func (s *buildPhaseState) registryWrapperCommit(name string) (string, error) {
	repoName, _, ok := api.SplitPackageRef(name)
	if !ok {
		return "", nil
	}
	mgr := repo.NewRepoManager(s.ctx.Paths.ReposDir)
	if !mgr.Exists(repoName) {
		return "", nil
	}
	commit, err := repo.GetCurrentCommit(mgr.Path(repoName))
	if err != nil {
		return "", fmt.Errorf("read registry %s HEAD: %w", repoName, err)
	}
	return commit, nil
}

func (s *buildPhaseState) writeLockfile() error {
	if s.ctx.LockPath == "" {
		return nil
	}
	updated := lockfile.New()
	for _, name := range s.ctx.Resolver.GetOrder() {
		node := s.ctx.DepGraph.Packages[name]
		if node.IsLocal() {
			continue
		}
		entry := s.remote.entries[name]
		if entry == nil || entry.Version == "" {
			if s.ctx.Lock != nil {
				if old, ok := s.ctx.Lock.Get(name); ok {
					updated.Set(name, old)
				}
			}
			continue
		}
		source := "registry"
		if node.Native != nil {
			source = "native"
		}
		locked := &lockfile.LockedPkg{
			Version: entry.Version,
			Commit:  s.remote.commits[name],
			Source:  source,
		}
		if source == "registry" {
			wrapperCommit, err := s.registryWrapperCommit(name)
			if err != nil {
				return fmt.Errorf("pin wrapper commit for %s: %w", name, err)
			}
			locked.WrapperCommit = wrapperCommit
		}
		updated.Set(name, locked)
	}
	if s.ctx.Lock != nil {
		for _, name := range s.ctx.Lock.SortedNames() {
			if _, ok := updated.Get(name); !ok {
				vlog.Info("  lock: dropped stale entry %s", name)
			}
		}
	}
	if len(updated.Packages) == 0 {
		return nil
	}
	if s.ctx.Lock != nil && s.ctx.Lock.Equal(updated) {
		return nil
	}
	if err := updated.Save(s.ctx.LockPath); err != nil {
		return fmt.Errorf("write %s: %w", s.ctx.LockPath, err)
	}
	s.ctx.Lock = updated
	vlog.Info("Wrote %s (%d package(s))", s.ctx.LockPath, len(updated.Packages))
	return nil
}

func (s *buildPhaseState) downloadRemoteSources(remote *remoteVersionState, depsDir, resolvedCC string) error {
	subParents := s.ctx.Resolver.SubParents()
	sourceMgr := repo.NewSourceManager(depsDir, s.ctx.Paths.CacheDir)
	for _, name := range s.ctx.Resolver.GetOrder() {
		node := s.ctx.DepGraph.Packages[name]
		if !s.needed[name] || node.IsLocal() {
			continue
		}
		if _, isSub := subParents[name]; isSub {
			continue
		}
		entryCfg := remote.entries[name]
		repoName, pkgName, ok := api.SplitPackageRef(name)
		if !ok {
			return fmt.Errorf("invalid package ref %q", name)
		}
		pkg := api.NewPackage().SetRepo(repoName).SetName(pkgName)
		if node.IsNative() {
			if entryCfg.Version == "" {
				entryCfg.Version = node.Native.Selected
			}
			pkg.SetGit(node.Native.GitURL)
			pkg.SetVersions(node.Native.Versions)
		} else if node.Pkg != nil {
			pkg.SetGit(node.Pkg.GitURLs()...)
			pkg.SetVersions(node.Pkg.Versions())
			pkg.SetSubmodules(node.Pkg.Submodules())
		} else if node.Source != nil && node.Source.Path != "" {
			interpreted, err := buildscript.LoadBuildScriptWithTrust(*node.Source, s.ctx.TrustChecker)
			if err != nil {
				return fmt.Errorf("load %s for download: %w", name, err)
			}
			pkg.SetVersions(interpreted.Versions())
			pkg.SetGit(interpreted.GitURLs()...)
		}

		if locked, isLocked := s.lockedVersion(name); isLocked && entryCfg.Version == "" {
			entryCfg.Version = locked
		}
		if entryCfg.Version == "" && len(pkg.GetVersions()) > 0 {
			var selected string
			var err error
			if len(node.Constraints) > 0 {
				selected, err = pkg.SelectVersionMulti(node.Constraints)
			} else {
				selected, err = pkg.SelectVersion("")
			}
			if err != nil {
				return err
			}
			entryCfg.Version = selected
		}
		expectedCommit := ""
		if s.ctx.Lock != nil && !s.ctx.IgnoreLock {
			if locked, ok := s.ctx.Lock.Get(name); ok && locked.Commit != "" && locked.Version == entryCfg.Version {
				expectedCommit = locked.Commit
			}
		}
		res, err := sourceMgr.EnsureVersion(pkg, entryCfg.Version, expectedCommit)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", name, err)
		}
		patchHash, err := patchHashForNode(name, node)
		if err != nil {
			return err
		}
		remote.commits[name] = res.Commit
		remote.versionDirs[name] = res.VersionDir
		s.patchHashes[name] = patchHash
		vlog.Info("  %s@%s -> %s", name, entryCfg.Version, res.LocalSrc)
		scriptHash, err := s.scriptHashFor(name)
		if err != nil {
			return err
		}
		s.pkgDirs[name] = makeRemotePkgDirs(res.VersionDir, res.LocalSrc, resolvedCC, s.cfg.Mode, s.allPkgOptions[name],
			entryCfg.Version, res.Commit, s.globalFlagsHash, patchHash, scriptHash)
	}
	return nil
}

func (s *buildPhaseState) lockedVersion(name string) (string, bool) {
	if s.ctx.Lock == nil || s.ctx.IgnoreLock {
		return "", false
	}
	if locked, ok := s.ctx.Lock.Get(name); ok && locked.Version != "" {
		return locked.Version, true
	}
	return "", false
}

func (s *buildPhaseState) cloneLocalGitSources() error {
	for _, name := range s.ctx.Resolver.GetOrder() {
		node := s.ctx.DepGraph.Packages[name]
		if !s.needed[name] || !node.IsLocal() || node.Pkg == nil {
			continue
		}
		gitURLs := node.Pkg.GitURLs()
		if len(gitURLs) == 0 {
			continue
		}
		if isLegacyRealSrcDir(node) {
			vlog.Info("  %s (source exists)", name)
			continue
		}
		sourceMgr := repo.NewSourceManager(s.ctx.Paths.DepsDir, s.ctx.Paths.CacheDir)
		srcDir, err := sourceMgr.EnsureURL(gitURLs[0])
		if err != nil {
			return fmt.Errorf("failed to download source for %s: %w", name, err)
		}
		localSrc := filepath.Join(node.Source.Dir, "src")
		if err := fs.EnsureSymlink(localSrc, srcDir); err != nil {
			return fmt.Errorf("link source for %s: %w", name, err)
		}
		vlog.Info("  %s -> %s", name, localSrc)
		node.Pkg.SetSrcDir(localSrc)
	}
	return nil
}

func (s *buildPhaseState) setupSubPackageDirs(depsDir string) error {
	subParents := s.ctx.Resolver.SubParents()
	if len(subParents) == 0 {
		return nil
	}

	resolvedTools, err := build.ResolveTools(s.cfg.Tc)
	if err != nil {
		return fmt.Errorf("resolve tools: %w", err)
	}

	for _, name := range s.ctx.Resolver.GetOrder() {
		rootParent, isSub := subParents[name]
		if !isSub {
			continue
		}
		if !s.needed[name] {
			continue
		}
		node := s.ctx.DepGraph.Packages[name]
		if node.Source == nil {
			continue
		}

		parentDirs, ok := s.pkgDirs[rootParent]
		if !ok || parentDirs.SourceDir == "" {
			continue
		}

		relPath := strings.TrimPrefix(name, rootParent+"/")
		sourceDir := filepath.Join(parentDirs.SourceDir, relPath)

		opts := s.allPkgOptions[name]
		scriptHash, err := s.scriptHashFor(name)
		if err != nil {
			return err
		}
		s.pkgDirs[name] = makeLocalPkgDirs(sourceDir, resolvedTools.CCKey(), s.cfg.Mode, opts, s.globalFlagsHash, scriptHash)
	}
	return nil
}

func (s *buildPhaseState) cloneSubPackageGitSources() error {
	subParents := s.ctx.Resolver.SubParents()
	if len(subParents) == 0 {
		return nil
	}

	for _, name := range s.ctx.Resolver.GetOrder() {
		if _, isSub := subParents[name]; !isSub {
			continue
		}
		node := s.ctx.DepGraph.Packages[name]
		if !s.needed[name] || node.Pkg == nil {
			continue
		}
		gitURLs := node.Pkg.GitURLs()
		if len(gitURLs) == 0 {
			continue
		}
		dirs, ok := s.pkgDirs[name]
		if !ok || dirs.SourceDir == "" {
			continue
		}
		srcDir := filepath.Join(dirs.SourceDir, "src")
		if _, err := os.Stat(filepath.Join(srcDir, ".git")); err == nil {
			node.Pkg.SetSrcDir(srcDir)
			vlog.Info("  %s (source exists)", name)
			continue
		}
		vlog.Info("  %s -> %s", name, srcDir)
		if err := repo.Clone(gitURLs[0], srcDir); err != nil {
			return fmt.Errorf("failed to download source for %s: %w", name, err)
		}
		node.Pkg.SetSrcDir(srcDir)
	}
	return nil
}

func (s *buildPhaseState) applyPatchesToNeeded() error {
	sourceMgr := repo.NewSourceManager(s.ctx.Paths.DepsDir, s.ctx.Paths.CacheDir)
	for _, name := range s.ctx.Resolver.GetOrder() {
		node := s.ctx.DepGraph.Packages[name]
		if !s.needed[name] || node.Pkg == nil || node.Source == nil {
			continue
		}
		if len(node.Pkg.GetPatches()) == 0 {
			continue
		}
		if node.IsLocal() {
			if err := applyPatches(node.Pkg, node.Pkg.SrcDir()); err != nil {
				return fmt.Errorf("apply patches for %s: %w", name, err)
			}
			continue
		}
		versionDir := s.remote.versionDirs[name]
		if versionDir == "" {
			return fmt.Errorf("apply patches for %s: no materialized version dir", name)
		}
		patchedSrc, err := sourceMgr.EnsurePatched(node.Pkg, versionDir)
		if err != nil {
			return fmt.Errorf("apply patches for %s: %w", name, err)
		}
		vlog.Info("  %s: patched source at %s", name, patchedSrc)
		node.Pkg.SetSrcDir(patchedSrc)
		if dirs := s.pkgDirs[name]; dirs != nil {
			dirs.SourceDir = patchedSrc
		}
	}
	return nil
}

func (s *buildPhaseState) restoreKConfigs() error {
	return restoreKConfigFiles(s.ctx, s.pkgDirs, s.needed)
}

func (s *buildPhaseState) executeOnBuild() {
	vlog.Info("")
	vlog.Info("Executing OnBuild...")

	s.allTargets = make(map[string]map[string]*api.Target)
	s.buildCtxs = make(map[string]*api.BuildContext)

	s.pkgMetaMap = make(map[string]build.PkgBuildMeta)
	for _, name := range s.ctx.Resolver.GetOrder() {
		node := s.ctx.DepGraph.Packages[name]
		if !s.needed[name] || node.Source == nil {
			continue
		}
		s.pkgMetaMap[name] = build.PkgBuildMeta{
			Origin: node.Source.Origin,
			Deps:   node.Deps,
		}
	}

	s.executeMainPackages(s.needed)

	for pkgName := range s.subGraphBuilt {
		delete(s.allTargets, pkgName)
	}
}

func (s *buildPhaseState) executeMainPackages(filter map[string]bool) {
	for _, name := range s.ctx.Resolver.GetOrder() {
		node := s.ctx.DepGraph.Packages[name]
		if !filter[name] || node.Pkg == nil {
			continue
		}
		if _, done := s.allTargets[name]; done {
			continue
		}
		s.executeOnePackage(name, node)
	}
}

func (s *buildPhaseState) executeOnePackage(name string, node *resolver.PackageNode) {
	buildCtx := newBuildContext(s.ctx, name, s.cfg.GlobalValues)
	buildCtx.SetBuildSubGraphFunc(func(pkgName string) error {
		return s.buildSubGraph(pkgName)
	})
	buildCtx.SetDepOutputFunc(func(depRef string) string {
		return s.computeDepOutput(depRef)
	})

	if node.Pkg != nil && s.cfg.Tc != nil {
		buildCtx.SetDefaultFlags(s.cfg.Tc.DefaultFlags.CFlags, s.cfg.Tc.DefaultFlags.CxxFlags, s.cfg.Tc.DefaultFlags.LdFlags)
		pkg := node.Pkg
		allOpts := s.ctx.AllOptions[name]
		if allOpts == nil {
			allOpts = pkg.GetOptions()
		}
		cfgVals := mergeCfgVals(name, node, s.ctx, s.cfg.GlobalValues, s.allPkgOptions)
		pkg.SetDirs(*s.pkgDirs[name])
		pkg.SetOptions(allOpts)
		pkg.SetCfgVals(cfgVals)
		pkg.SetToolchain(s.cfg.Tc)
	}

	buildCtx.SetPackage(node.Pkg)

	node.Pkg.ExecBuildFuncs(s.pkgDirs[name].SourceDir, func(fn api.BuildFunc) {
		fn(buildCtx)
	})

	applyBuildContextConfig(buildCtx, node, s.ctx, name)

	s.allTargets[name] = buildCtx.GetTargets()
	s.buildCtxs[name] = buildCtx
}

func (s *buildPhaseState) buildSubGraph(rootPkg string) error {
	if s.subGraphBuilt[rootPkg] {
		return nil
	}
	s.subGraphBuilt[rootPkg] = true

	subPkgs := build.CollectSubGraphPackages(rootPkg, s.pkgMetaMap, s.allTargets, s.needed)

	s.executeMainPackages(subPkgs)

	subTcName := resolvePkgToolchain(s.ctx.Config, rootPkg, s.cfg.TcName)
	subTc, err := toolchain.GetManager().SelectToolchain(subTcName)
	if err != nil {
		return err
	}

	if subTcName != s.cfg.TcName {
		subResolvedTools, err := build.ResolveTools(subTc)
		if err != nil {
			return fmt.Errorf("resolve subgraph tools for %s: %w", rootPkg, err)
		}
		for name := range subPkgs {
			if meta, ok := s.pkgMetaMap[name]; ok && meta.IsRemote() {
				versionDir := s.remote.versionDirs[name]
				dirs := s.pkgDirs[name]
				if versionDir == "" {
					scriptHash, err := s.scriptHashFor(name)
					if err != nil {
						return err
					}
					s.pkgDirs[name] = makeLocalPkgDirs(dirs.SourceDir, subResolvedTools.CCKey(), s.cfg.Mode, s.allPkgOptions[name], s.globalFlagsHash, scriptHash)
					continue
				}
				version, commit := s.remotePkgKeyMaterial(name)
				scriptHash, err := s.scriptHashFor(name)
				if err != nil {
					return err
				}
				s.pkgDirs[name] = makeRemotePkgDirs(versionDir, dirs.SourceDir, subResolvedTools.CCKey(), s.cfg.Mode, s.allPkgOptions[name],
					version, commit, s.globalFlagsHash, s.patchHashes[name], scriptHash)
			}
		}
	}

	keyExtra, err := s.buildPkgKeyExtra()
	if err != nil {
		return err
	}

	params := &build.SubGraphParams{
		AllTargets:   s.allTargets,
		PkgMeta:      s.pkgMetaMap,
		PkgDirs:      s.pkgDirs,
		Packages:     make(map[string]*api.Package),
		Needed:       s.needed,
		SubParents:   s.ctx.Resolver.SubParents(),
		IncludeTests: s.includeTests,
		PkgKeyExtra:  keyExtra,
		PkgLockDir:   s.ctx.Paths.LocksDir,
		RootDir:      s.ctx.Paths.ProjectDir,
		NumWorkers:   s.jobs,
		KeepGoing:    s.keepGoing,
	}
	for name, node := range s.ctx.DepGraph.Packages {
		if node.Pkg != nil && subPkgs[name] {
			params.Packages[name] = node.Pkg
		}
	}

	if err := build.BuildSubGraph(rootPkg, subTc, subTcName, s.cfg.Mode, params, s.allPkgOptions); err != nil {
		return err
	}

	return nil
}

func (s *buildPhaseState) resolveDepTargets() map[string]map[string]*api.Target {
	return s.allTargets
}

func (s *buildPhaseState) computeDepOutput(depRef string) string {
	pkgName, targetName, ok := strings.Cut(depRef, ":")
	if !ok {
		pkgName = depRef
		targetName = ""
	}
	pd := s.pkgDirs[pkgName]
	if pd == nil {
		return ""
	}
	targets := s.resolveDepTargets()
	if targetName == "" {
		pkgTargets := targets[pkgName]
		if len(pkgTargets) == 1 {
			for name := range pkgTargets {
				targetName = name
			}
		}
	}
	if targetName == "" {
		return ""
	}
	target := targets[pkgName][targetName]
	if target == nil {
		return ""
	}
	if pd.BuildDir != "" {
		filename := target.Kind().Prefix() + targetName + target.Kind().Ext()
		return filepath.Join(pd.BuildDir, filename)
	}
	return ""
}

func (s *buildPhaseState) logResults() {
	vlog.Info("")
	vlog.Info("Targets found:")
	for pkgName, targets := range s.allTargets {
		for _, t := range targets {
			defaultMark := ""
			testMark := ""
			if !t.IsDefault() {
				defaultMark = " [disabled]"
			}
			if t.IsTest() {
				testMark = " [test]"
			}
			vlog.Info("  - %s:%s (%s)%s%s", pkgName, t.Name(), t.Kind(), defaultMark, testMark)
		}
	}

	vlog.Info("")
	vlog.Info("Using toolchain: %s, mode: %s", s.cfg.TcName, s.cfg.Mode)
}

func (s *buildPhaseState) remotePkgKeyMaterial(name string) (string, string) {
	if s.remote == nil {
		return "", ""
	}
	if entry := s.remote.entries[name]; entry != nil {
		return entry.Version, s.remote.commits[name]
	}
	return "", ""
}

func (s *buildPhaseState) buildPkgKeyExtra() (map[string]string, error) {
	extra := make(map[string]string)
	for name := range s.needed {
		node := s.ctx.DepGraph.Packages[name]
		if node == nil || node.IsLocal() {
			scriptHash, err := s.scriptHashFor(name)
			if err != nil {
				return nil, err
			}
			extra[name] = localKeyExtra(s.globalFlagsHash, scriptHash)
			continue
		}
		version, commit := s.remotePkgKeyMaterial(name)
		if version == "" && node.Native != nil {
			version = node.Native.Selected
		}
		if commit == "" && node.Native != nil {
			commit = node.Native.Commit
		}
		scriptHash, err := s.scriptHashFor(name)
		if err != nil {
			return nil, err
		}
		extra[name] = build.JoinKeyExtra(version, commit, s.globalFlagsHash, s.patchHashes[name], scriptHash)
	}
	return extra, nil
}

func (s *buildPhaseState) buildAndRunPipeline() (*BuildResult, error) {
	graph, err := build.NewBuildGraph(s.allTargets, s.pkgMetaMap, s.ctx.Resolver.SubParents())
	if err != nil {
		return nil, err
	}

	vlog.Info("")
	vlog.Info("Build order:")
	for _, fullName := range graph.Order {
		vlog.Info("  - %s", fullName)
	}

	bp := build.NewBuildPipeline(graph, s.cfg.Tc, s.pkgDirs, s.cfg.Mode, s.allPkgOptions)
	bp.SetRootDir(s.ctx.Paths.ProjectDir)
	bp.SetIncludeTests(s.includeTests)
	keyExtra, err := s.buildPkgKeyExtra()
	if err != nil {
		return nil, err
	}
	bp.SetPkgKeyExtra(keyExtra)
	bp.SetPkgLockDir(s.ctx.Paths.LocksDir)
	bp.SetNumWorkers(s.jobs)
	bp.SetParallelPkgs(s.jobs)
	bp.SetKeepGoing(s.keepGoing)

	for _, name := range s.ctx.Resolver.GetOrder() {
		node := s.ctx.DepGraph.Packages[name]
		if s.needed[name] && node.Pkg != nil {
			bp.SetPackage(name, node.Pkg)
		}
	}

	vlog.Info("")
	vlog.Info("Building...")
	scheduler, err := bp.Run()
	if err != nil {
		return nil, err
	}

	vlog.Info("")
	vlog.Info("Build succeeded!")

	pkgBuildKeys := make(map[string]string)
	for _, name := range s.ctx.Resolver.GetOrder() {
		if node := s.ctx.DepGraph.Packages[name]; node != nil && s.needed[name] {
			if info, ok := scheduler.GetPkgInfo(name); ok {
				pkgBuildKeys[name] = info.BuildKey
			}
		}
	}

	return &BuildResult{
		AllTargets:    s.allTargets,
		Graph:         graph,
		PkgDirs:       s.pkgDirs,
		PkgBuildKeys:  pkgBuildKeys,
		TcName:        s.cfg.TcName,
		Mode:          s.cfg.Mode,
		InstalledPkgs: s.remote.installedPkgs(s.pkgDirs),
		BuildCtxs:     s.buildCtxs,
	}, nil
}

func (r *remoteVersionState) installedPkgs(pkgDirs map[string]*api.PkgDirs) map[string]*api.InstalledPackage {
	if len(pkgDirs) == 0 {
		return nil
	}
	result := make(map[string]*api.InstalledPackage)
	for name, d := range pkgDirs {
		if d.InstallDir != "" {
			if rc, ok := r.entries[name]; ok {
				result[name] = api.NewInstalledPackage(name, rc.Version, d.InstallDir, nil)
			}
		}
	}
	return result
}
