package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	exec "gitee.com/spock2300/vmake/internal/exec"
	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/internal/jsonio"
	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/build"
	"gitee.com/spock2300/vmake/pkg/config"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/repo"
	"gitee.com/spock2300/vmake/pkg/resolver"
	"gitee.com/spock2300/vmake/pkg/toolchain"
	"gitee.com/spock2300/vmake/pkg/version"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the project",
	Long:  `Compile and link all targets defined in build.go files.`,
	Run:   runBuild,
}

func init() {
	RootCmd.AddCommand(buildCmd)
	addInstallFlags(buildCmd)
	buildCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "force buildscript recompilation")
	buildCmd.Flags().StringVar(&toolchainFlag, "toolchain", "", "override toolchain")
	buildCmd.Flags().StringVar(&modeFlag, "mode", "", "override build mode")
	buildCmd.Flags().StringVar(&manifestFlag, "manifest", "", "pin versions from manifest file")
	buildCmd.Flags().BoolVar(&testsFlag, "tests", false, "build test targets")
	buildCmd.RegisterFlagCompletionFunc("toolchain", completeToolchain)
	buildCmd.RegisterFlagCompletionFunc("mode", completeMode)
}

func runBuild(cmd *cobra.Command, args []string) {
	opts := pipelineOptions{force: forceFlag, tests: testsFlag, installAfter: installFlag}
	if manifestFlag != "" {
		opts.afterPhase1 = func(ctx *RuntimeContext) {
			applyManifestVersions(ctx, manifestFlag)
		}
	}
	runPipeline(opts)
}

func applyManifestVersions(ctx *RuntimeContext, manifestPath string) {
	var mf installManifest
	fatalErr(jsonio.Load(manifestPath, &mf))

	cwd, err := os.Getwd()
	fatalErr(err)

	for _, entry := range mf.Packages {
		switch entry.Source {
		case "local":
			if entry.Ref == "" || entry.Ref == "unknown" {
				continue
			}
			fatalErr(repo.Checkout(filepath.Join(cwd, entry.Path), entry.Ref))
			vlog.Info("  checkout %s -> %s", entry.Name, entry.Ref[:12]+"...")
		case "native", "registry":
			if entry.Ref == "" {
				continue
			}
			existing := config.GetEntry(ctx.Config, entry.Name)
			existing.Version = entry.Ref
			config.SetEntry(ctx.Config, entry.Name, existing)
			vlog.Info("  pin %s -> %s", entry.Name, entry.Ref)
		}
	}
}

type installManifestEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Source  string `json:"source"`
	URL     string `json:"url,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Path    string `json:"path,omitempty"`
}

type installManifest struct {
	VMake     string                 `json:"vmake"`
	Toolchain string                 `json:"toolchain"`
	Mode      string                 `json:"mode"`
	Generated string                 `json:"generated"`
	Packages  []installManifestEntry `json:"packages"`
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

func writeManifest(ctx *RuntimeContext, result *BuildResult, effectivePrefix string) error {
	var packages []installManifestEntry
	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if node.IsLocal() {
			sourceDir := result.PkgDirs[name].SourceDir
			relPath, _ := filepath.Rel(ctx.WorkDir, sourceDir)
			gitDir := sourceDir
			if node.Pkg != nil {
				if srcDir := node.Pkg.SrcDir(); srcDir != "" {
					gitDir = srcDir
				}
			}
			packages = append(packages, installManifestEntry{
				Name:    name,
				Version: gitDescribe(gitDir),
				Source:  "local",
				Ref:     gitRevParse(gitDir),
				Path:    relPath,
			})
			continue
		}
		ip, ok := result.InstalledPkgs[name]
		if !ok {
			continue
		}
		entry := installManifestEntry{
			Name:    name,
			Version: ip.Version,
			Source:  "registry",
		}
		if node.IsNative() {
			entry.Source = "native"
			entry.URL = node.Native.GitURL
			if ref, ok := node.Native.Versions[ip.Version]; ok {
				entry.Ref = ref
			}
		} else if node.Pkg != nil {
			urls := node.Pkg.GitURLs()
			if len(urls) > 0 {
				entry.URL = urls[0]
			}
			if ref, ok := node.Pkg.Versions()[ip.Version]; ok {
				entry.Ref = ref
			}
		}
		packages = append(packages, entry)
	}
	mf := installManifest{
		VMake:     version.Version,
		Toolchain: result.TcName,
		Mode:      result.Mode,
		Generated: time.Now().UTC().Format(time.RFC3339),
		Packages:  packages,
	}
	path := filepath.Join(effectivePrefix, "manifest.json")
	return jsonio.Save(path, mf)
}

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

func runBuildPhase(ctx *RuntimeContext, includeTests bool) (*BuildResult, error) {
	cfg, err := resolveBuildConfig(ctx)
	if err != nil {
		return nil, err
	}

	needed := filterAndCollectNeeded(ctx)

	localPkgOptions := collectLocalPkgOptions(ctx)

	pkgDirs := ResolveAllPackageDirs(ctx.DepGraph)
	remote, err := prepareAllPackages(ctx, cfg, needed, pkgDirs, localPkgOptions)
	if err != nil {
		return nil, err
	}

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if needed[name] && node.Pkg != nil {
			patchDir := pkgDirs[name].SourceDir
			if srcDir := node.Pkg.SrcDir(); srcDir != "" {
				patchDir = srcDir
			}
			if err := applyPatches(node.Pkg, patchDir); err != nil {
				return nil, fmt.Errorf("apply patches for %s: %w", name, err)
			}
		}
	}

	if err := restoreKConfigFiles(ctx, pkgDirs, needed); err != nil {
		return nil, err
	}

	allTargets, pkgMetaMap, subBuildKeys := executeAllOnBuild(ctx, needed, remote, pkgDirs, cfg, localPkgOptions, includeTests)

	if includeTests {
		for _, targets := range allTargets {
			for _, t := range targets {
				if t.IsTest() {
					t.SetDefault(true)
				}
			}
		}
	}

	vlog.Info("")
	vlog.Info("Targets found:")
	for pkgName, targets := range allTargets {
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
	vlog.Info("Using toolchain: %s, mode: %s", cfg.TcName, cfg.Mode)

	graph, err := build.NewBuildGraph(allTargets, pkgMetaMap)
	if err != nil {
		return nil, err
	}

	vlog.Info("")
	vlog.Info("Build order:")
	for _, fullName := range graph.Order {
		vlog.Info("  - %s", fullName)
	}

	pipeline := build.NewBuildPipeline(graph, cfg.Tc, pkgDirs, cfg.Mode, localPkgOptions, subBuildKeys)

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if needed[name] && node.Pkg != nil {
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
	for _, name := range ctx.Resolver.GetOrder() {
		if node := ctx.DepGraph.Packages[name]; node != nil && needed[name] {
			if info, ok := scheduler.GetPkgInfo(name); ok {
				pkgBuildKeys[name] = info.BuildKey
			}
		}
	}

	return &BuildResult{
		AllTargets:    allTargets,
		Graph:         graph,
		PkgDirs:       pkgDirs,
		PkgBuildKeys:  pkgBuildKeys,
		TcName:        cfg.TcName,
		Mode:          cfg.Mode,
		InstalledPkgs: remote.installedPkgs(pkgDirs),
	}, nil
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

func collectLocalPkgOptions(ctx *RuntimeContext) map[string]map[string]any {
	result := make(map[string]map[string]any)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.IsLocal() {
			entry := config.GetEntry(ctx.Config, name)
			opts := make(map[string]any, len(entry.Options))
			for k, v := range entry.Options {
				opts[k] = v
			}
			result[name] = opts
		}
	}
	return result
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

type remotePkgState struct {
	configs map[string]*config.EntryConfig
}

func makeLocalPkgDirs(scriptDir, ccPath, mode string, opts map[string]any) *api.PkgDirs {
	buildKey := build.BuildKey(ccPath, mode, opts)
	return &api.PkgDirs{
		SourceDir: scriptDir,
		BuildDir:  filepath.Join(scriptDir, "build", buildKey),
	}
}

func makeRemotePkgDirs(depsDir, name, ccPath, mode string, opts map[string]any, sourceDir string) *api.PkgDirs {
	buildKey := build.BuildKey(ccPath, mode, opts)
	return &api.PkgDirs{
		SourceDir:  sourceDir,
		BuildDir:   filepath.Join(depsDir, name, "out", buildKey, "build"),
		InstallDir: filepath.Join(depsDir, name, "out", buildKey, "install"),
	}
}

func (r *remotePkgState) installedPkgs(pkgDirs map[string]*api.PkgDirs) map[string]*api.InstalledPackage {
	if len(pkgDirs) == 0 {
		return nil
	}
	result := make(map[string]*api.InstalledPackage)
	for name, d := range pkgDirs {
		if d.InstallDir != "" {
			if rc, ok := r.configs[name]; ok {
				result[name] = api.NewInstalledPackage(name, rc.Version, d.InstallDir, nil)
			}
		}
	}
	return result
}

func prepareAllPackages(ctx *RuntimeContext, cfg *buildConfig, needed map[string]bool, pkgDirs map[string]*api.PkgDirs, localPkgOptions map[string]map[string]any) (*remotePkgState, error) {
	remote := &remotePkgState{
		configs: make(map[string]*config.EntryConfig),
	}

	resolvedTools, err := build.ResolveTools(cfg.Tc)
	if err != nil {
		return nil, fmt.Errorf("resolve tools: %w", err)
	}

	for _, name := range ctx.Resolver.GetOrder() {
		if !needed[name] {
			continue
		}
		if !ctx.DepGraph.Packages[name].IsLocal() {
			remote.configs[name] = config.GetEntry(ctx.Config, name)
		}
	}

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if !needed[name] || node.Source == nil {
			continue
		}
		opts := localPkgOptions[name]
		pkgDirs[name] = makeLocalPkgDirs(node.Source.Dir, resolvedTools.CC, cfg.Mode, opts)
	}

	depsDir := getDepsDir()
	repoMgr := getRepoManager()

	sourceMgr := repo.NewSourceManager(depsDir, getSourcesDir())
	installer := repo.NewPackageInstaller(sourceMgr, depsDir)
	installer.SetRepoManager(repoMgr)
	installer.SetConfigs(remote.configs)
	installer.SetToolchain(cfg.Tc)

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if !needed[name] || node.IsLocal() {
			continue
		}
		if node.Pkg != nil {
			installer.SetPackage(name, node.Pkg)
		}
	}

	vlog.Info("")
	vlog.Info("Downloading package sources...")

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if !needed[name] || node.IsLocal() {
			continue
		}

		entryCfg := remote.configs[name]
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
				return nil, err
			}
			entryCfg.Version = selected
		}

		sourceDir, err := sourceMgr.EnsureSource(pkg, entryCfg.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to download %s: %w", name, err)
		}
		vlog.Info("  %s@%s -> %s", name, entryCfg.Version, sourceDir)

		pkgDirs[name] = makeRemotePkgDirs(depsDir, name, cfg.Tc.Tools.CC, cfg.Mode, entryCfg.Options, sourceDir)
	}

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if !needed[name] || !node.IsLocal() || node.Pkg == nil {
			continue
		}
		gitURLs := node.Pkg.GitURLs()
		if len(gitURLs) == 0 {
			continue
		}
		srcDir := filepath.Join(node.Source.Dir, "src")
		if info, err := os.Stat(srcDir); err == nil && info.IsDir() {
			if _, err2 := os.Stat(filepath.Join(srcDir, ".git")); err2 == nil {
				vlog.Info("  %s (source exists)", name)
				node.Pkg.SetSrcDir(srcDir)
				continue
			}
		}
		vlog.Info("  %s -> %s", name, srcDir)
		if err := repo.Clone(gitURLs[0], srcDir); err != nil {
			return nil, fmt.Errorf("failed to download source for %s: %w", name, err)
		}
		node.Pkg.SetSrcDir(srcDir)
	}

	return remote, nil
}

func executePackageOnBuild(ctx *RuntimeContext, name string, node *resolver.PackageNode, pkgDirs map[string]*api.PkgDirs,
	allTargets map[string]map[string]*api.Target, remote *remotePkgState, globalValues map[string]any,
	buildSubGraphFn func(string) error, depOutputFn func(string) string, tc *toolchain.Toolchain, localPkgOptions map[string]map[string]any) {

	buildCtx := newBuildContext(ctx, name, globalValues)
	buildCtx.SetBuildSubGraphFunc(buildSubGraphFn)
	buildCtx.SetDepOutputFunc(depOutputFn)

	if node.Pkg != nil && tc != nil {
		buildCtx.SetDefaultFlags(tc.DefaultFlags.CFlags, tc.DefaultFlags.CxxFlags, tc.DefaultFlags.LdFlags)
		pkg := node.Pkg
		cfgVals := make(map[string]any)
		allOpts := ctx.AllOptions[name]
		if allOpts == nil {
			allOpts = pkg.GetOptions()
		}
		for optName, opt := range allOpts {
			if opt.Default() != nil {
				cfgVals[optName] = opt.Default()
			}
		}
		if remoteCfg, ok := remote.configs[name]; ok {
			for k, v := range remoteCfg.Options {
				cfgVals[k] = v
			}
		}
		if localOpts, ok := localPkgOptions[name]; ok {
			for k, v := range localOpts {
				cfgVals[k] = v
			}
		}
		for k, v := range globalValues {
			if _, exists := cfgVals[k]; !exists {
				cfgVals[k] = v
			}
		}
		pkg.SetDirs(*pkgDirs[name])
		pkg.SetOptions(allOpts)
		pkg.SetCfgVals(cfgVals)
		pkg.SetToolchain(tc)
	}

	node.Pkg.ExecBuildFuncs(pkgDirs[name].SourceDir, func(fn api.BuildFunc) {
		fn(buildCtx)
	})

	if buildCtx.GenConfigDefinesFlag() && node.Pkg != nil {
		defines := api.ConfigToDefines(node.Pkg.Options, node.Pkg.CfgVals)
		args := make([]any, len(defines))
		for i, d := range defines {
			args[i] = d
		}
		for _, t := range buildCtx.GetTargets() {
			t.AddDefines(args...)
		}
	}
	if buildCtx.GenConfigHeaderFlag() && node.Pkg != nil {
		node.Pkg.SetGenConfigHeader(true)
	}

	allTargets[name] = buildCtx.GetTargets()
}

func computeDepOutput(depRef string, targets map[string]map[string]*api.Target, ctx *RuntimeContext, cfg *buildConfig, pkgDirs map[string]*api.PkgDirs, localPkgOptions map[string]map[string]any) string {
	pkgName, targetName, ok := strings.Cut(depRef, ":")
	if !ok {
		pkgName = depRef
		targetName = ""
	}
	pd := pkgDirs[pkgName]
	if pd == nil {
		return ""
	}
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

func executeAllOnBuild(ctx *RuntimeContext, needed map[string]bool, remote *remotePkgState, pkgDirs map[string]*api.PkgDirs, cfg *buildConfig, localPkgOptions map[string]map[string]any, includeTests bool) (map[string]map[string]*api.Target, map[string]build.PkgBuildMeta, map[string]string) {
	vlog.Info("")
	vlog.Info("Executing OnBuild...")

	allTargets := make(map[string]map[string]*api.Target)

	pkgMetaMap := make(map[string]build.PkgBuildMeta)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if !needed[name] {
			continue
		}
		pkgMetaMap[name] = build.PkgBuildMeta{
			Origin: node.Source.Origin,
			Deps:   node.Deps,
		}
	}

	depsDir := getDepsDir()
	subGraphBuilt := make(map[string]bool)
	subGraphPkgs := make(map[string]bool)
	subBuildKeys := make(map[string]string)

	var buildSubGraphFn func(string) error
	var depOutputFn func(string) string

	buildSubGraphFn = func(rootPkg string) error {
		if subGraphBuilt[rootPkg] {
			return nil
		}
		subGraphBuilt[rootPkg] = true

		subAllTargets := make(map[string]map[string]*api.Target, len(allTargets))
		for k, v := range allTargets {
			subAllTargets[k] = v
		}

		subPkgs := build.CollectSubGraphPackages(rootPkg, pkgMetaMap, subAllTargets, needed)

		for pkgName := range subPkgs {
			subGraphPkgs[pkgName] = true
		}

		subDepOutputFn := func(depRef string) string {
			pkgName, _, _ := strings.Cut(depRef, ":")
			if subPkgs[pkgName] {
				return computeDepOutput(depRef, subAllTargets, ctx, cfg, pkgDirs, localPkgOptions)
			}
			return depOutputFn(depRef)
		}

		for _, name := range ctx.Resolver.GetOrder() {
			node := ctx.DepGraph.Packages[name]
			if !subPkgs[name] || node.Pkg == nil {
				continue
			}
			if _, done := subAllTargets[name]; done {
				continue
			}
			executePackageOnBuild(ctx, name, node, pkgDirs, subAllTargets, remote, cfg.GlobalValues, buildSubGraphFn, subDepOutputFn, cfg.Tc, localPkgOptions)
			autoWireRequireDeps(node.Pkg, subAllTargets, subAllTargets[name])
		}

		subTcName := resolvePkgToolchain(ctx.Config, rootPkg, cfg.TcName)
		subTc, err := toolchain.GetManager().SelectToolchain(subTcName)
		if err != nil {
			return err
		}

		subPkgDirs := make(map[string]*api.PkgDirs, len(pkgDirs))
		for k, v := range pkgDirs {
			subPkgDirs[k] = v
		}
		if subTcName != cfg.TcName {
			subResolvedTools, _ := build.ResolveTools(subTc)
			for name := range subPkgs {
				if meta, ok := pkgMetaMap[name]; ok && meta.IsRemote() {
					rcfg := remote.configs[name]
					subPkgDirs[name] = makeRemotePkgDirs(depsDir, name, subResolvedTools.CC, cfg.Mode, rcfg.Options, pkgDirs[name].SourceDir)
				}
			}
			for name := range subPkgs {
				opts := localPkgOptions[name]
				subBuildKeys[name] = build.BuildKey(subResolvedTools.CC, cfg.Mode, opts)
			}
		}

		params := &build.SubGraphParams{
			AllTargets: subAllTargets,
			PkgMeta:    pkgMetaMap,
			PkgDirs:    subPkgDirs,
			Packages:   make(map[string]*api.Package),
			Needed:     needed,
		}
		for name, node := range ctx.DepGraph.Packages {
			if node.Pkg != nil && subPkgs[name] {
				params.Packages[name] = node.Pkg
			}
		}

		if includeTests {
			for _, targets := range subAllTargets {
				for _, t := range targets {
					if t.IsTest() {
						t.SetDefault(true)
					}
				}
			}
		}

		if err := build.BuildSubGraph(rootPkg, subTc, subTcName, cfg.Mode, params, localPkgOptions); err != nil {
			return err
		}

		if targets, ok := subAllTargets[rootPkg]; ok {
			allTargets[rootPkg] = targets
		}

		return nil
	}

	depOutputFn = func(depRef string) string {
		return computeDepOutput(depRef, allTargets, ctx, cfg, pkgDirs, localPkgOptions)
	}

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || !needed[name] {
			continue
		}
		if node.Pkg == nil {
			continue
		}
		if _, done := allTargets[name]; done {
			continue
		}

		executePackageOnBuild(ctx, name, node, pkgDirs, allTargets, remote, cfg.GlobalValues, buildSubGraphFn, depOutputFn, cfg.Tc, localPkgOptions)
		autoWireRequireDeps(node.Pkg, allTargets, allTargets[name])
	}

	for pkgName := range subGraphBuilt {
		delete(allTargets, pkgName)
	}

	return allTargets, pkgMetaMap, subBuildKeys
}

func autoWireRequireDeps(pkg *api.Package, allTargets map[string]map[string]*api.Target, localTargets map[string]*api.Target) {
	if pkg == nil || localTargets == nil || allTargets == nil {
		return
	}
	for _, t := range localTargets {
		for _, req := range pkg.GetRequires().Get() {
			depPkgName := req.Name
			depTargets := allTargets[depPkgName]
			if depTargets == nil {
				continue
			}
			for _, dt := range depTargets {
				depRef := depPkgName + ":" + dt.Name()
				if !t.HasDep(depRef) {
					t.AddDeps(depRef)
				}
			}
		}
	}
}

func collectNeeded(graph *resolver.Graph) map[string]bool {
	needed := make(map[string]bool, len(graph.Packages))
	var queue []string
	for id, node := range graph.Packages {
		if node.IsLocal() {
			needed[id] = true
			queue = append(queue, id)
		}
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if node, ok := graph.Packages[name]; ok {
			for _, dep := range node.Deps {
				if !needed[dep] {
					needed[dep] = true
					queue = append(queue, dep)
				}
			}
		}
	}
	return needed
}

func applyPatches(pkg *api.Package, sourceDir string) error {
	patches := pkg.GetPatches()
	if len(patches) == 0 {
		return nil
	}

	scriptDir := pkg.ScriptDir()
	vlog.Info("Applying patches for %s", pkg.FullName())

	for _, patch := range patches {
		absPath := filepath.Join(scriptDir, patch)
		if repo.IsPatchApplied(sourceDir, absPath) {
			vlog.Info("  %s (already applied)", patch)
			continue
		}
		vlog.Info("  %s", patch)
		if err := repo.ApplyPatch(sourceDir, absPath); err != nil {
			return err
		}
	}

	return nil
}

func restoreKConfigFiles(ctx *RuntimeContext, pkgDirs map[string]*api.PkgDirs, needed map[string]bool) error {
	for _, name := range ctx.Resolver.GetOrder() {
		if !needed[name] {
			continue
		}
		kconfigs := ctx.AllKConfigs[name]
		if len(kconfigs) == 0 {
			continue
		}

		entry := config.GetEntry(ctx.Config, name)
		kconfigContent := entry.KConfig
		hasEntry := ctx.Config.Entries != nil && ctx.Config.Entries[name] != nil

		k := kconfigs[0]
		srcDir := k.SrcDir()
		if srcDir == "" {
			srcDir = pkgDirs[name].SourceDir
		}
		configPath := filepath.Join(srcDir, k.ConfigPath())

		if hasEntry && kconfigContent == "" {
			os.Remove(configPath)
			continue
		}

		if kconfigContent == "" {
			continue
		}

		existing, err := os.ReadFile(configPath)
		if err == nil && string(existing) == kconfigContent {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return fmt.Errorf("restore kconfig %s: %w", name, err)
		}
		if err := os.WriteFile(configPath, []byte(kconfigContent), 0644); err != nil {
			return fmt.Errorf("restore kconfig %s: %w", name, err)
		}
		api.ApplyKConfigPatches(configPath, k.Patches())
		vlog.Info("Restored .config for %s (%d bytes)", name, len(kconfigContent))
	}
	return nil
}

type BuildGoInfo struct {
	Versions map[string]string
	GitURLs  []string
}

func ParseBuildGo(path string) (*BuildGoInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)
	info := &BuildGoInfo{
		Versions: make(map[string]string),
	}
	for _, call := range []struct {
		name    string
		handler func([]string)
	}{
		{"AddVersion", func(args []string) {
			if len(args) >= 2 {
				info.Versions[args[0]] = args[1]
			}
		}},
		{"SetGit", func(args []string) {
			if len(args) > 0 {
				info.GitURLs = append(info.GitURLs, args[0])
			}
		}},
	} {
		prefix := call.name + "("
		for i := 0; i+len(prefix) <= len(content); i++ {
			if content[i:i+len(prefix)] == prefix {
				call.handler(extractCallArgs(content[i+len(prefix):]))
			}
		}
	}
	return info, nil
}

func extractCallArgs(s string) []string {
	var args []string
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			i++
			start := i
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' {
					i++
				}
				i++
			}
			args = append(args, s[start:i])
			if len(args) >= 3 {
				break
			}
		} else if s[i] == ')' {
			break
		}
	}
	return args
}

func executeInstall(ctx *RuntimeContext, result *BuildResult) error {
	globalValues := config.BuildGlobalValues(ctx.Config)

	effectivePrefix := prefixFlag
	if effectivePrefix == "" {
		effectivePrefix = filepath.Join(ctx.WorkDir, "install")
	}

	vlog.Info("")
	vlog.Info("Installing...")

	fs.RemoveIfExists(effectivePrefix)
	fs.EnsureDir(effectivePrefix)

	installer := build.NewArtifactInstaller(result.Graph, result.PkgDirs, effectivePrefix)
	installer.SetInstallType(installTypeFlag)

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if node.Pkg == nil {
			continue
		}

		entry := config.GetEntry(ctx.Config, name)

		installCtx := api.NewInstallContext(name, entry.Options)
		installCtx.SetOptions(ctx.AllOptions[name])
		installCtx.MergeGlobals(ctx.GlobalOptions, globalValues)

		node.Pkg.ExecInstallFuncs(result.PkgDirs[name].SourceDir, func(fn api.InstallFunc) {
			fn(installCtx)
		})

		buildCtx := newBuildContext(ctx, name, globalValues)
		buildCtx.SetDryRun(true)
		buildCtx.SetBuildSubGraphFunc(func(string) error { return nil })
		buildCtx.SetDepOutputFunc(func(string) string { return "" })
		node.Pkg.SetDryRun(true)
		node.Pkg.ExecBuildFuncs(result.PkgDirs[name].SourceDir, func(fn api.BuildFunc) {
			fn(buildCtx)
		})
		node.Pkg.SetDryRun(false)

		installItems := installCtx.GetInstallItems()
		installItems = append(installItems, buildCtx.GetInstallItems()...)

		var installFilter api.InstallFilterFunc
		if installCtx.GetInstallFilter() != nil {
			installFilter = installCtx.GetInstallFilter()
		} else if buildCtx.GetInstallFilter() != nil {
			installFilter = buildCtx.GetInstallFilter()
		}

		installer.SetPackageInfo(name, &build.PkgInstallInfo{
			Targets:       result.AllTargets[name],
			InstallItems:  installItems,
			BuildDir:      result.PkgDirs[name].BuildDir,
			Mode:          result.Mode,
			TcName:        result.TcName,
			BuildKey:      result.PkgBuildKeys[name],
			InstallFilter: installFilter,
		})
	}

	if err := installer.InstallAll(); err != nil {
		return err
	}

	if err := writeManifest(ctx, result, effectivePrefix); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	vlog.Info("")
	vlog.Info("Install succeeded!")
	return nil
}
