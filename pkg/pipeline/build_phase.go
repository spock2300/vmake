package pipeline

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spock2300/vmake/internal/buildruntime"
	"github.com/spock2300/vmake/internal/scriptcall"
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
	SubGraphRoots map[string]bool
	AllTargets    map[string]map[string]*api.Target
	Graph         *build.BuildGraph
	PkgDirs       map[string]*api.PkgDirs
	PkgBuildKeys  map[string]string
	PkgPlatforms  map[string]api.Platform
	GlobalValues  map[string]any
	TcName        string
	TargetOS      string
	Mode          string
	InstalledPkgs map[string]*api.InstalledPackage
	BuildCtxs     map[string]*api.BuildContext
}

type buildPhaseState struct {
	session          *build.Session
	budget           *buildruntime.Budget
	bindings         map[string]*packageBinding
	declarations     map[string]uint8
	declarationPath  []string
	scopeValues      map[string]any
	subGraphErrors   map[string]error
	subGraphRequests map[string]string
	globalFlags      [4][]string
	ctx              *RuntimeContext
	includeTests     bool
	jobs             int
	keepGoing        bool
	globalFlagsHash  string

	cfg            *buildConfig
	needed         map[string]bool
	pkgDirs        map[string]*api.PkgDirs
	remote         *remoteVersionState
	patchHashes    map[string]string
	sourceSeeds    map[string]string
	sourceCommits  map[string]string
	sourceVersions map[string]string
	scriptHashes   map[string]string
	allPkgOptions  map[string]map[string]any
	allTargets     map[string]map[string]*api.Target
	pkgMetaMap     map[string]build.PkgBuildMeta

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
		ctx:            ctx,
		session:        build.NewSession(ctx.Context),
		bindings:       make(map[string]*packageBinding),
		declarations:   make(map[string]uint8),
		subGraphErrors: make(map[string]error),
		includeTests:   opts.IncludeTests,
		jobs:           opts.Jobs,
		keepGoing:      opts.KeepGoing,
		patchHashes:    make(map[string]string),
		sourceSeeds:    make(map[string]string),
		sourceCommits:  make(map[string]string),
		sourceVersions: make(map[string]string),
		scriptHashes:   make(map[string]string),
		subGraphBuilt:  make(map[string]bool),
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

func RunBuild(ctx *RuntimeContext, opts BuildOptions) (_ *BuildResult, err error) {
	defer scriptcall.Recover(&err)
	if ctx.Context != nil && ctx.Context.Err() != nil {
		return nil, ctx.Context.Err()
	}
	if opts.Jobs < 0 {
		return nil, fmt.Errorf("jobs must be non-negative")
	}
	if opts.Jobs == 0 {
		opts.Jobs = runtime.NumCPU()
	}
	s := newBuildPhaseState(ctx, opts)
	s.budget = &buildruntime.Budget{Jobs: opts.Jobs}

	if err := s.resolveBuildConfig(); err != nil {
		return nil, err
	}

	if err := s.filterNeeded(); err != nil {
		return nil, err
	}
	if err := s.acquireStorageOwners(); err != nil {
		return nil, err
	}

	applyGlobalFlagsFromNeeded(ctx, s.needed)
	mgr := toolchain.GetManager()
	s.globalFlags = [4][]string{mgr.GetGlobalCFlags(), mgr.GetGlobalCxxFlags(), mgr.GetGlobalLdFlags(), mgr.GetGlobalLinks()}

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
	if err := os.Remove(filepath.Join(ctx.Paths.ProjectDir, "build", "compile_commands.json")); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if err := s.executeOnBuild(); err != nil {
		return nil, err
	}

	s.logResults()

	return s.buildAndRunPipeline()
}

func UpdateLock(ctx *RuntimeContext) (err error) {
	defer scriptcall.Recover(&err)
	s := newBuildPhaseState(ctx, BuildOptions{})

	cfg, err := resolveExistingBuildConfig(ctx)
	if err != nil {
		return err
	}
	s.cfg = cfg

	if err := s.filterNeeded(); err != nil {
		return err
	}
	if err := s.acquireStorageOwners(); err != nil {
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
	if _, err := ProjectPlatform(ctx); err != nil {
		return nil, err
	}
	tc, tcName, err := GetToolchain(ctx.Config, ctx.ToolchainOverride)
	if err != nil {
		return nil, err
	}
	return makeBuildConfig(ctx, tc, tcName), nil
}

func makeBuildConfig(ctx *RuntimeContext, tc *toolchain.Toolchain, tcName string) *buildConfig {
	mode := ResolveMode(ctx.Config, ctx.ModeOverride)
	globalValues := projectGlobalValues(ctx)
	globalValues[api.ModeOptionName] = mode
	globalValues[api.ToolchainOptionName] = tcName

	return &buildConfig{
		Mode:         mode,
		TcName:       tcName,
		Tc:           tc,
		Platform:     platformFromValues(globalValues),
		GlobalValues: globalValues,
	}
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

func (s *buildPhaseState) toolsForPackage(name string) (*build.ResolvedTools, error) {
	tcName := resolvePkgToolchain(s.ctx.Config, name, s.cfg.TcName)
	tc := s.cfg.Tc
	if tcName != s.cfg.TcName {
		var err error
		tc, err = toolchain.GetManager().SelectToolchain(tcName)
		if err != nil {
			return nil, err
		}
	}
	platform, err := PackagePlatform(s.ctx, name)
	if err != nil {
		return nil, err
	}
	return s.session.ResolveTools(tc, platform)
}

func (s *buildPhaseState) prepareAllPackages() error {
	remote := &remoteVersionState{
		entries:     make(map[string]*config.EntryConfig),
		commits:     make(map[string]string),
		versionDirs: make(map[string]string),
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
		resolvedTools, err := s.toolsForPackage(name)
		if err != nil {
			return err
		}
		opts := s.allPkgOptions[name]
		scriptHash, err := s.scriptHashFor(name)
		if err != nil {
			return err
		}
		if err := s.selectPackageSource(name); err != nil {
			return fmt.Errorf("select source for %s: %w", name, err)
		}
		s.pkgDirs[name] = makeLocalPkgDirs(node.Source.Dir, resolvedTools.CCKey(), s.cfg.Mode, opts, packageFlagsHash(s.globalFlagsHash, node), scriptHash, s.sourceCommits[name])
	}

	depsDir := s.ctx.Paths.DepsDir

	vlog.Info("")
	vlog.Info("Downloading package sources...")

	if err := s.downloadRemoteSources(remote, depsDir); err != nil {
		return err
	}
	s.remote = remote

	if err := s.setupSubPackageDirs(depsDir); err != nil {
		return err
	}

	if err := s.cloneLocalGitSources(); err != nil {
		return err
	}

	if err := s.cloneSubPackageGitSources(); err != nil {
		return err
	}

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
	commit, err := repo.GetCurrentCommitContext(s.ctx.Context, mgr.Path(repoName))
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
			if version := s.sourceVersions[name]; version != "" {
				updated.Set(name, &lockfile.LockedPkg{Version: version, Commit: s.sourceCommits[name], Source: "git"})
			} else if node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 && len(node.Pkg.Versions()) > 0 && s.ctx.Lock != nil {
				if old, ok := s.ctx.Lock.Get(name); ok && old.Source == "git" {
					updated.Set(name, old)
				}
			}
			continue
		}
		if _, isSub := s.ctx.Resolver.SubParents()[name]; isSub {
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

func (s *buildPhaseState) downloadRemoteSources(remote *remoteVersionState, depsDir string) error {
	subParents := s.ctx.Resolver.SubParents()
	sourceMgr := repo.NewSourceManager(depsDir, s.ctx.Paths.CacheDir).WithSession(s.ctx.Locks).WithContext(s.ctx.Context)
	for _, name := range s.ctx.Resolver.GetOrder() {
		node := s.ctx.DepGraph.Packages[name]
		if !s.needed[name] || node.IsLocal() {
			continue
		}
		if _, isSub := subParents[name]; isSub {
			continue
		}
		resolvedTools, err := s.toolsForPackage(name)
		if err != nil {
			return err
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
		s.pkgDirs[name] = makeRemotePkgDirs(res.VersionDir, res.LocalSrc, resolvedTools.CCKey(), s.cfg.Mode, s.allPkgOptions[name],
			entryCfg.Version, res.Commit, packageFlagsHash(s.globalFlagsHash, node), patchHash, scriptHash)
		if err := prepareRemoteWorkspace(sourceMgr, node, s.pkgDirs[name], res.VersionDir, res.Commit, patchHash); err != nil {
			return fmt.Errorf("prepare workspace for %s: %w", name, err)
		}
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
		if err := s.preparePackageWorkspace(name); err != nil {
			return fmt.Errorf("prepare workspace for %s: %w", name, err)
		}
	}
	return nil
}

func (s *buildPhaseState) setupSubPackageDirs(depsDir string) error {
	subParents := s.ctx.Resolver.SubParents()
	if len(subParents) == 0 {
		return nil
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
		resolvedTools, err := s.toolsForPackage(name)
		if err != nil {
			return err
		}

		relPath := strings.TrimPrefix(name, rootParent+"/")
		parentNode := s.ctx.DepGraph.Packages[rootParent]
		parentEntry := config.GetEntry(s.ctx.Config, rootParent)
		version, commit, ok := remoteVersionKey(s.ctx, rootParent, parentNode, parentEntry)
		if !ok {
			return fmt.Errorf("native parent %s has no resolved version", rootParent)
		}
		if recorded := s.remote.commits[rootParent]; recorded != "" {
			commit = recorded
		}
		versionDir := s.remote.versionDirs[rootParent]
		if versionDir == "" {
			versionDir = remoteVersionDir(s.ctx, rootParent, version)
		}
		s.remote.versionDirs[rootParent] = versionDir
		s.remote.commits[rootParent] = commit
		s.remote.versionDirs[name] = versionDir
		s.remote.commits[name] = commit
		opts := s.allPkgOptions[name]
		scriptHash, err := s.scriptHashFor(name)
		if err != nil {
			return err
		}
		patchHash, err := patchHashForNode(name, node)
		if err != nil {
			return err
		}
		s.patchHashes[name] = patchHash
		if err := s.selectPackageSource(name); err != nil {
			return fmt.Errorf("select source for %s: %w", name, err)
		}
		s.pkgDirs[name] = makeRemotePkgDirs(versionDir, node.Source.Dir, resolvedTools.CCKey(), s.cfg.Mode, opts,
			version, s.packageCommitKey(name, commit), packageFlagsHash(s.globalFlagsHash, node), patchHash, scriptHash, relPath)
		manager := repo.NewSourceManager(depsDir, s.ctx.Paths.CacheDir).WithSession(s.ctx.Locks).WithContext(s.ctx.Context)
		if err := prepareRemoteWorkspace(manager, node, s.pkgDirs[name], versionDir, commit, patchHash); err != nil {
			return fmt.Errorf("prepare workspace for %s: %w", name, err)
		}
	}
	return nil
}

func (s *buildPhaseState) cloneSubPackageGitSources() error {
	for _, name := range s.ctx.Resolver.GetOrder() {
		if _, isSub := s.ctx.Resolver.SubParents()[name]; !isSub || !s.needed[name] {
			continue
		}
		if err := s.preparePackageWorkspace(name); err != nil {
			return fmt.Errorf("prepare workspace for %s: %w", name, err)
		}
	}
	return nil
}

func (s *buildPhaseState) applyPatchesToNeeded() error {
	for _, name := range s.ctx.Resolver.GetOrder() {
		node := s.ctx.DepGraph.Packages[name]
		if !s.needed[name] || node.Pkg == nil || node.Source == nil {
			continue
		}
		if len(node.Pkg.GetPatches()) == 0 {
			continue
		}
		if err := applyPatchesContext(s.ctx.Context, node.Pkg, node.Pkg.SrcDir()); err != nil {
			return fmt.Errorf("apply patches for %s: %w", name, err)
		}
	}
	return nil
}

func (s *buildPhaseState) restoreKConfigs() error {
	return restoreKConfigFiles(s.ctx, s.pkgDirs, s.needed)
}

func (s *buildPhaseState) executeOnBuild() error {
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

	if err := s.executeMainPackages(s.needed); err != nil {
		return err
	}

	return nil
}

func (s *buildPhaseState) executeMainPackages(filter map[string]bool) error {
	for _, name := range s.ctx.Resolver.GetOrder() {
		if s.ctx.Context != nil && s.ctx.Context.Err() != nil {
			return s.ctx.Context.Err()
		}
		node := s.ctx.DepGraph.Packages[name]
		if !filter[name] || node.Pkg == nil {
			continue
		}
		if s.declarations[name] == 2 {
			if s.scopeValues != nil {
				if _, err := s.bindPackage(name); err != nil {
					return err
				}
			}
			continue
		}
		if s.declarations[name] != 0 {
			return fmt.Errorf("recursive or failed OnBuild declaration: %s -> %s", strings.Join(s.declarationPath, " -> "), name)
		}
		if err := s.executeOnePackage(name, node); err != nil {
			return err
		}
	}
	return nil
}

func (s *buildPhaseState) executeOnePackage(name string, node *resolver.PackageNode) (err error) {
	s.declarations[name] = 1
	s.declarationPath = append(s.declarationPath, name)
	defer func() {
		s.declarationPath = s.declarationPath[:len(s.declarationPath)-1]
		if err != nil {
			s.declarations[name] = 3
		}
	}()
	defer scriptcall.Recover(&err)
	bound, err := s.bindPackage(name)
	if err != nil {
		return err
	}
	buildCtx, err := newBuildContext(s.ctx, name, bound.values)
	if err != nil {
		return err
	}
	platform := platformFromValues(buildCtx.CfgVals)
	buildCtx.SetBuildSubGraphFunc(func(pkgName string) error { return s.buildSubGraph(pkgName) })
	buildCtx.SetDepOutputFunc(func(depRef string) string { return s.computeDepOutput(depRef) })
	flags := api.DefaultBuildFlags(bound.toolchain.Name, platform.OSOrHost())
	buildCtx.SetDefaultFlags(flags.CFlags, flags.CxxFlags, flags.LdFlags)
	pkg := node.Pkg
	allOpts := s.ctx.AllOptions[name]
	if allOpts == nil {
		allOpts = pkg.GetOptions()
	}
	pkg.SetDirs(*s.pkgDirs[name])
	pkg.SetOptions(allOpts)
	pkg.SetCfgVals(maps.Clone(buildCtx.CfgVals))
	pkg.SetToolchain(bound.toolchain)
	pkg.SetPlatform(platform)
	buildCtx.SetPackage(pkg)
	pkg.ExecBuildFuncs(s.pkgDirs[name].SourceDir, func(fn api.BuildFunc) { fn(buildCtx) })
	applyBuildContextConfig(buildCtx, node, s.ctx, name)
	targets := make(map[string]*api.Target)
	for targetName, target := range buildCtx.GetTargets() {
		targets[targetName] = api.SnapshotTarget(target)
	}
	s.allTargets[name] = targets
	s.buildCtxs[name] = buildCtx
	s.declarations[name] = 2
	return nil
}

func (s *buildPhaseState) buildSubGraph(rootPkg string) (err error) {
	if active := s.session.ActiveTarget(); active != "" {
		return fmt.Errorf("BuildSubGraph(%s) is not allowed while target %s is running", rootPkg, active)
	}
	previous, completed := s.subGraphErrors[rootPkg]
	if s.subGraphBuilt[rootPkg] && !completed {
		return fmt.Errorf("recursive BuildSubGraph(%s)", rootPkg)
	}
	if !s.needed[rootPkg] {
		return fmt.Errorf("subgraph package %s not found in required packages", rootPkg)
	}
	previousScope := s.scopeValues
	base := previousScope
	if base == nil {
		base = s.cfg.GlobalValues
	}
	values, err := s.packageBindingValues(rootPkg, base)
	if err != nil {
		return err
	}
	scope := maps.Clone(base)
	for _, key := range []string{api.ToolchainOptionName, api.TargetOSOptionName, api.TargetTripleOptionName} {
		scope[key] = values[key]
	}
	request, err := s.subGraphRequestSignature(rootPkg, values, scope)
	if err != nil {
		return err
	}
	subPkgs := build.CollectSubGraphPackages(rootPkg, s.pkgMetaMap, s.allTargets, s.needed)
	for _, name := range s.ctx.Resolver.GetOrder() {
		if !subPkgs[name] || s.bindings[name] == nil {
			continue
		}
		values, err := s.packageBindingValues(name, scope)
		if err != nil {
			return err
		}
		if _, err := s.matchingPackageBinding(name, values); err != nil {
			return err
		}
	}
	if previous, ok := s.subGraphRequests[rootPkg]; ok && previous != request {
		return fmt.Errorf("subgraph package %s was already requested with an incompatible build configuration", rootPkg)
	}
	if completed {
		return previous
	}
	if s.subGraphRequests == nil {
		s.subGraphRequests = make(map[string]string)
	}
	s.subGraphRequests[rootPkg] = request
	s.subGraphBuilt[rootPkg] = true
	defer func() { s.subGraphErrors[rootPkg] = err }()
	defer scriptcall.Recover(&err)
	s.scopeValues = scope
	defer func() { s.scopeValues = previousScope }()
	for {
		if err := s.executeMainPackages(subPkgs); err != nil {
			return err
		}
		expanded := build.CollectSubGraphPackages(rootPkg, s.pkgMetaMap, s.allTargets, s.needed)
		if len(expanded) == len(subPkgs) {
			break
		}
		subPkgs = expanded
	}
	bound := s.bindings[rootPkg]
	if bound == nil {
		return fmt.Errorf("subgraph package %s has no build declaration", rootPkg)
	}
	keyExtra, err := s.buildPkgKeyExtra()
	if err != nil {
		return err
	}
	params := &build.SubGraphParams{
		Session:           s.session,
		GlobalFlags:       &s.globalFlags,
		Platform:          platformFromValues(bound.values),
		AllTargets:        s.allTargets,
		PkgMeta:           s.pkgMetaMap,
		PkgDirs:           s.pkgDirs,
		Packages:          make(map[string]*api.Package),
		PackageToolchains: s.packageToolchains(),
		Needed:            s.needed,
		SubParents:        s.ctx.Resolver.SubParents(),
		IncludeTests:      s.includeTests,
		PkgKeyExtra:       keyExtra,
		RootDir:           s.ctx.Paths.ProjectDir,
		NumWorkers:        s.jobs,
		KeepGoing:         s.keepGoing,
	}
	for name := range subPkgs {
		if node := s.ctx.DepGraph.Packages[name]; node != nil && node.Pkg != nil {
			params.Packages[name] = node.Pkg
		}
	}
	return build.BuildSubGraph(rootPkg, bound.toolchain, bound.toolchain.Name, s.cfg.Mode, params, s.allPkgOptions)
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
		platform := s.cfg.Platform
		if node := s.ctx.DepGraph.Packages[pkgName]; node != nil && node.Pkg != nil {
			platform.OS = node.Pkg.TargetOS()
		}
		filename := api.TargetFilename(target.Kind(), targetName, platform.OSOrHost())
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
	name = remoteOwnerName(s.ctx, name)
	if entry := s.remote.entries[name]; entry != nil {
		return entry.Version, s.remote.commits[name]
	}
	return "", ""
}

func (s *buildPhaseState) buildPkgKeyExtra() (map[string]string, error) {
	extra := make(map[string]string)
	for name := range s.needed {
		node := s.ctx.DepGraph.Packages[name]
		flagsHash := packageFlagsHash(s.globalFlagsHash, node)
		if node == nil || node.IsLocal() {
			scriptHash, err := s.scriptHashFor(name)
			if err != nil {
				return nil, err
			}
			extra[name] = localKeyExtra(flagsHash, scriptHash, s.sourceCommits[name])
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
		extra[name] = build.JoinKeyExtra(version, s.packageCommitKey(name, commit), flagsHash, s.patchHashes[name], scriptHash)
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

	bp := build.NewBuildPipeline(graph, s.cfg.Tc, s.pkgDirs, s.cfg.Mode, s.allPkgOptions, s.cfg.Platform)
	bp.SetRootDir(s.ctx.Paths.ProjectDir)
	bp.SetIncludeTests(s.includeTests)
	keyExtra, err := s.buildPkgKeyExtra()
	if err != nil {
		return nil, err
	}
	bp.SetPkgKeyExtra(keyExtra)
	bp.Session = s.session
	bp.GlobalFlags = &s.globalFlags
	bp.PackageToolchains = s.packageToolchains()
	bp.SetNumWorkers(s.jobs)
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
	pkgPlatforms := make(map[string]api.Platform)
	for _, name := range s.ctx.Resolver.GetOrder() {
		if node := s.ctx.DepGraph.Packages[name]; node != nil && s.needed[name] {
			if node.Pkg != nil {
				pkgPlatforms[name] = api.Platform{OS: node.Pkg.TargetOS(), Triple: node.Pkg.TargetTriple()}
			}
			if info, ok := scheduler.GetPkgInfo(name); ok {
				pkgBuildKeys[name] = info.BuildKey
			}
		}
	}

	return &BuildResult{
		SubGraphRoots: maps.Clone(s.subGraphBuilt),
		AllTargets:    s.allTargets,
		Graph:         graph,
		PkgDirs:       s.pkgDirs,
		PkgBuildKeys:  pkgBuildKeys,
		PkgPlatforms:  pkgPlatforms,
		GlobalValues:  maps.Clone(s.cfg.GlobalValues),
		TcName:        s.cfg.TcName,
		TargetOS:      s.cfg.Platform.OSOrHost(),
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
