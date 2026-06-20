package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	exec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type buildConfig struct {
	Mode         string
	TcName       string
	Tc           *toolchain.Toolchain
	GlobalValues map[string]any
}

type BuildResult struct {
	AllTargets    map[string]map[string]*api.Target
	Graph         *build.BuildGraph
	PkgDirs       map[string]*api.PkgDirs
	PkgBuildKeys  map[string]string
	TcName        string
	Mode          string
	InstalledPkgs map[string]*api.InstalledPackage
}

type subGraphFrame struct {
	pkgs    map[string]bool
	targets map[string]map[string]*api.Target
}

type buildPhaseState struct {
	ctx          *RuntimeContext
	includeTests bool

	cfg           *buildConfig
	needed        map[string]bool
	pkgDirs       map[string]*api.PkgDirs
	remote        *remoteVersionState
	allPkgOptions map[string]map[string]any
	allTargets    map[string]map[string]*api.Target
	pkgMetaMap    map[string]build.PkgBuildMeta
	subBuildKeys  map[string]string

	subGraphBuilt map[string]bool
	subGraphStack []subGraphFrame
}

type remoteVersionState struct {
	entries map[string]*config.EntryConfig
}

func newBuildPhaseState(ctx *RuntimeContext, includeTests bool) *buildPhaseState {
	return &buildPhaseState{
		ctx:           ctx,
		includeTests:  includeTests,
		subGraphBuilt: make(map[string]bool),
	}
}

func runBuildPhase(ctx *RuntimeContext, includeTests bool) (*BuildResult, error) {
	s := newBuildPhaseState(ctx, includeTests)

	if err := s.resolveBuildConfig(); err != nil {
		return nil, err
	}

	s.filterNeeded()

	applyGlobalFlagsFromNeeded(ctx, s.needed)

	s.computeDirsAndOptions()

	if err := s.prepareAllPackages(); err != nil {
		return nil, err
	}

	if err := s.applyPatchesToNeeded(); err != nil {
		return nil, err
	}

	if err := s.restoreKConfigs(); err != nil {
		return nil, err
	}

	s.executeOnBuild()

	if s.includeTests {
		enableTestDefaults(s.allTargets)
	}

	s.logResults()

	return s.buildAndRunPipeline()
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
	mode := resolveMode(ctx.Config, modeFlag)

	tc, tcName, err := GetToolchain(ctx.Config)
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

func (s *buildPhaseState) filterNeeded() {
	s.needed = filterAndCollectNeeded(s.ctx)
}

func filterAndCollectNeeded(ctx *RuntimeContext) map[string]bool {
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
			vlog.Error("  %s: filter deps: %v", name, err)
		} else if len(node.Deps) > 0 {
			vlog.Info("  %s: deps=%v", name, node.Deps)
		}
	}
	if err := ctx.Resolver.UpdateOrder(); err != nil {
		vlog.Fatal("dependency cycle: %v", err)
	}

	needed := collectNeeded(ctx.DepGraph)

	return needed
}

func (s *buildPhaseState) computeDirsAndOptions() {
	s.allPkgOptions = collectAllPkgOptions(s.ctx, s.needed)
	s.pkgDirs = ResolveAllPackageDirs(s.ctx.DepGraph)
}

func (s *buildPhaseState) prepareAllPackages() error {
	remote := &remoteVersionState{
		entries: make(map[string]*config.EntryConfig),
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
		s.pkgDirs[name] = makeLocalPkgDirs(node.Source.Dir, resolvedTools.CC, s.cfg.Mode, opts)
	}

	depsDir := getDepsDir()

	vlog.Info("")
	vlog.Info("Downloading package sources...")

	if err := s.downloadRemoteSources(remote, depsDir); err != nil {
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

func (s *buildPhaseState) downloadRemoteSources(remote *remoteVersionState, depsDir string) error {
	subParents := s.ctx.Resolver.SubParents()
	sourceMgr := repo.NewSourceManager(depsDir, getSourcesDir())
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
			continue
		}
		pkg := newPkgRef(repoName, pkgName)
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
			info, err := ParseBuildGo(node.Source.Path)
			if err == nil {
				pkg.SetVersions(info.Versions)
				pkg.SetGit(info.GitURLs...)
			}
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
		sourceDir, err := sourceMgr.EnsureSource(pkg, entryCfg.Version)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", name, err)
		}
		vlog.Info("  %s@%s -> %s", name, entryCfg.Version, sourceDir)
		s.pkgDirs[name] = makeRemotePkgDirs(depsDir, name, s.cfg.Tc.Tools.CC, s.cfg.Mode, s.allPkgOptions[name], sourceDir)
	}
	return nil
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
		if detectExistingSrcDir(node) {
			vlog.Info("  %s (source exists)", name)
			continue
		}
		srcDir := filepath.Join(node.Source.Dir, "src")
		vlog.Info("  %s -> %s", name, srcDir)
		if err := repo.Clone(gitURLs[0], srcDir); err != nil {
			return fmt.Errorf("failed to download source for %s: %w", name, err)
		}
		node.Pkg.SetSrcDir(srcDir)
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

		parentDirs, ok := s.pkgDirs[rootParent]
		if !ok || parentDirs.SourceDir == "" {
			continue
		}

		relPath := strings.TrimPrefix(name, rootParent+"/")
		sourceDir := filepath.Join(parentDirs.SourceDir, relPath)

		opts := s.allPkgOptions[name]
		s.pkgDirs[name] = makeRemotePkgDirs(depsDir, name, s.cfg.Tc.Tools.CC, s.cfg.Mode, opts, sourceDir)
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
	for _, name := range s.ctx.Resolver.GetOrder() {
		node := s.ctx.DepGraph.Packages[name]
		if s.needed[name] && node.Pkg != nil {
			patchDir := node.Pkg.SrcDir()
			if err := applyPatches(node.Pkg, patchDir); err != nil {
				return fmt.Errorf("apply patches for %s: %w", name, err)
			}
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

	s.subBuildKeys = make(map[string]string)

	s.executeMainPackages(s.needed)

	for pkgName := range s.subGraphBuilt {
		delete(s.allTargets, pkgName)
	}
}

func (s *buildPhaseState) executeMainPackages(filter map[string]bool) {
	targetMap := s.allTargets
	if len(s.subGraphStack) > 0 {
		targetMap = s.subGraphStack[len(s.subGraphStack)-1].targets
	}
	for _, name := range s.ctx.Resolver.GetOrder() {
		node := s.ctx.DepGraph.Packages[name]
		if !filter[name] || node.Pkg == nil {
			continue
		}
		if _, done := targetMap[name]; done {
			continue
		}
		s.executeOnePackage(name, node)
		autoWireRequireDeps(node.Pkg, s.allTargets, s.allTargets[name], name, s.ctx.Resolver.SubParents())
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
		buildCtx.SetDefaultFlags(s.cfg.Tc.DefaultFlags.CFlags, s.cfg.Tc.DefaultFlags.CxxFlags)
		toolchain.GetManager().AddGlobalLdFlags(s.cfg.Tc.DefaultFlags.LdFlags...)
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
	if len(s.subGraphStack) > 0 {
		frame := &s.subGraphStack[len(s.subGraphStack)-1]
		frame.targets[name] = s.allTargets[name]
	}
}

func (s *buildPhaseState) buildSubGraph(rootPkg string) error {
	if s.subGraphBuilt[rootPkg] {
		return nil
	}
	s.subGraphBuilt[rootPkg] = true

	subAllTargets := make(map[string]map[string]*api.Target, len(s.allTargets))
	for k, v := range s.allTargets {
		subAllTargets[k] = v
	}

	subPkgs := build.CollectSubGraphPackages(rootPkg, s.pkgMetaMap, subAllTargets, s.needed)

	s.subGraphStack = append(s.subGraphStack, subGraphFrame{pkgs: subPkgs, targets: subAllTargets})
	defer func() { s.subGraphStack = s.subGraphStack[:len(s.subGraphStack)-1] }()

	s.executeMainPackages(subPkgs)

	subTcName := resolvePkgToolchain(s.ctx.Config, rootPkg, s.cfg.TcName)
	subTc, err := toolchain.GetManager().SelectToolchain(subTcName)
	if err != nil {
		return err
	}

	if subTcName != s.cfg.TcName {
		subResolvedTools, _ := build.ResolveTools(subTc)
		depsDir := getDepsDir()
		for name := range subPkgs {
			if meta, ok := s.pkgMetaMap[name]; ok && meta.IsRemote() {
				s.pkgDirs[name] = makeRemotePkgDirs(depsDir, name, subResolvedTools.CC, s.cfg.Mode, s.allPkgOptions[name], s.pkgDirs[name].SourceDir)
			}
		}
		for name := range subPkgs {
			opts := s.allPkgOptions[name]
			s.subBuildKeys[name] = build.BuildKey(subResolvedTools.CC, s.cfg.Mode, opts)
		}
	}

	params := &build.SubGraphParams{
		AllTargets: subAllTargets,
		PkgMeta:    s.pkgMetaMap,
		PkgDirs:    s.pkgDirs,
		Packages:   make(map[string]*api.Package),
		Needed:     s.needed,
		SubParents: s.ctx.Resolver.SubParents(),
	}
	for name, node := range s.ctx.DepGraph.Packages {
		if node.Pkg != nil && subPkgs[name] {
			params.Packages[name] = node.Pkg
		}
	}

	if s.includeTests {
		enableTestDefaults(subAllTargets)
	}

	if err := build.BuildSubGraph(rootPkg, subTc, subTcName, s.cfg.Mode, params, s.allPkgOptions); err != nil {
		return err
	}

	if targets, ok := subAllTargets[rootPkg]; ok {
		s.allTargets[rootPkg] = targets
	}

	return nil
}

func (s *buildPhaseState) resolveDepTargets(pkgName string) map[string]map[string]*api.Target {
	for i := len(s.subGraphStack) - 1; i >= 0; i-- {
		if s.subGraphStack[i].pkgs[pkgName] {
			return s.subGraphStack[i].targets
		}
	}
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
	targets := s.resolveDepTargets(pkgName)
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

	pipeline := build.NewBuildPipeline(graph, s.cfg.Tc, s.pkgDirs, s.cfg.Mode, s.allPkgOptions, s.subBuildKeys)

	for _, name := range s.ctx.Resolver.GetOrder() {
		node := s.ctx.DepGraph.Packages[name]
		if s.needed[name] && node.Pkg != nil {
			pipeline.SetPackage(name, node.Pkg)
		}
	}

	vlog.Info("")
	vlog.Info("Building...")
	scheduler, err := pipeline.Run()
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

func gitDescribe(dir string) string {
	out, err := exec.RunWithOptions("git", []string{"describe", "--tags", "--always", "--dirty"}, exec.RunOptions{Dir: dir, Quiet: true})
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return gitRevParse(dir)
}

func gitRevParse(dir string) string {
	return repo.GitRevParse(dir)
}
