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
	buildCmd.RegisterFlagCompletionFunc("toolchain", completeToolchain)
	buildCmd.RegisterFlagCompletionFunc("mode", completeMode)
}

func runBuild(cmd *cobra.Command, args []string) {
	opts := pipelineOptions{force: forceFlag, installAfter: installFlag}
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
			sourceDir := result.PkgDirs[name]
			relPath, _ := filepath.Rel(ctx.WorkDir, sourceDir)
			packages = append(packages, installManifestEntry{
				Name:    name,
				Version: gitDescribe(sourceDir),
				Source:  "local",
				Ref:     gitRevParse(sourceDir),
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
			entry.URL = node.NativeGitURL
			if ref, ok := node.NativeVersions[ip.Version]; ok {
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
	PkgDirs       map[string]string
	PkgBuildKeys  map[string]string
	TcName        string
	Mode          string
	InstalledPkgs map[string]*api.InstalledPackage
}

func runBuildPhase(ctx *RuntimeContext) (*BuildResult, error) {
	cfg, err := resolveBuildConfig(ctx)
	if err != nil {
		return nil, err
	}

	needed := filterAndCollectNeeded(ctx)

	localPkgOptions := collectLocalPkgOptions(ctx)

	remote, err := prepareRemotePackages(ctx, cfg.Tc, needed)
	if err != nil {
		return nil, err
	}

	pkgDirs := GetPackageDirs(ctx.DepGraph)
	allTargets, pkgMetaMap := executeAllOnBuild(ctx, needed, remote, pkgDirs, cfg, localPkgOptions)

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() && needed[name] && node.Pkg != nil {
			if err := applyPatches(node.Pkg, remote.dirs[name].SourceDir); err != nil {
				return nil, fmt.Errorf("apply patches for %s: %w", name, err)
			}
		}
	}

	vlog.Info("")
	vlog.Info("Targets found:")
	for pkgName, targets := range allTargets {
		for _, t := range targets {
			defaultMark := ""
			if !t.IsDefault() {
				defaultMark = " [disabled]"
			}
			vlog.Info("  - %s:%s (%s)%s", pkgName, t.Name(), t.Kind(), defaultMark)
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

	scheduler, err := build.NewScheduler(graph, cfg.Tc, pkgDirs, cfg.Mode, localPkgOptions)
	if err != nil {
		return nil, err
	}

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.IsLocal() {
			scheduler.SetPkgDirs(name, &api.PkgDirs{SourceDir: pkgDirs[name]})
		} else if needed[name] {
			scheduler.SetPkgDirs(name, remote.dirs[name])
		}
	}

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() && needed[name] && node.Pkg != nil {
			scheduler.SetPackage(name, node.Pkg)
		}
	}

	vlog.Info("")
	vlog.Info("Building...")
	if err := scheduler.BuildAll(); err != nil {
		return nil, err
	}

	vlog.Info("")
	vlog.Info("Build succeeded!")

	pkgBuildKeys := make(map[string]string)
	for _, name := range ctx.Resolver.GetOrder() {
		if node := ctx.DepGraph.Packages[name]; node != nil && node.IsLocal() {
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
		InstalledPkgs: remote.installedPkgs(),
	}, nil
}

func resolveBuildConfig(ctx *RuntimeContext) (*buildConfig, error) {
	globalValues := config.BuildGlobalValues(ctx.Config)

	mode := modeFlag
	if mode == "" {
		if m, ok := globalValues["mode"].(string); ok {
			mode = m
		}
	}
	if mode == "" {
		mode = api.ModeDebug
	}

	tc, tcName, err := GetToolchain(ctx.Config)
	if err != nil {
		return nil, err
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
			result[name] = entry.Options
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
	ctx.Resolver.UpdateOrder()

	needed := collectNeeded(ctx.DepGraph)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() && !needed[name] {
			vlog.Info("  %s: skipped (not needed)", name)
		}
	}

	return needed
}

type remotePkgState struct {
	configs map[string]*config.EntryConfig
	dirs    map[string]*api.PkgDirs
}

func (r *remotePkgState) installedPkgs() map[string]*api.InstalledPackage {
	if len(r.dirs) == 0 {
		return nil
	}
	result := make(map[string]*api.InstalledPackage)
	for name, d := range r.dirs {
		if d.InstallDir != "" {
			result[name] = api.NewInstalledPackage(name, r.configs[name].Version, d.InstallDir, nil)
		}
	}
	return result
}

func prepareRemotePackages(ctx *RuntimeContext, tc *toolchain.Toolchain, needed map[string]bool) (*remotePkgState, error) {
	hasDeps := false
	for _, name := range ctx.Resolver.GetOrder() {
		if needed[name] && !ctx.DepGraph.Packages[name].IsLocal() {
			hasDeps = true
			break
		}
	}

	remote := &remotePkgState{
		configs: make(map[string]*config.EntryConfig),
		dirs:    make(map[string]*api.PkgDirs),
	}

	if !hasDeps {
		return remote, nil
	}

	packagesDir := getPackagesDir()
	cacheDir := getCacheDir()
	repoMgr := getRepoManager()

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if needed[name] && !node.IsLocal() {
			remote.configs[name] = config.GetEntry(ctx.Config, name)
		}
	}

	sourceMgr := repo.NewSourceManager(cacheDir)
	installer := repo.NewPackageInstaller(sourceMgr, packagesDir, cacheDir)
	installer.SetRepoManager(repoMgr)
	installer.SetConfigs(remote.configs)
	installer.SetToolchain(tc)

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if needed[name] && !node.IsLocal() && node.Pkg != nil {
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

		cfg := remote.configs[name]
		repoName, pkgName, ok := api.SplitPackageRef(name)
		if !ok {
			continue
		}

		pkg := newPkgRef(repoName, pkgName)

		if node.IsNative() {
			if cfg.Version == "" {
				cfg.Version = node.NativeSelected
			}
			pkg.SetGit(node.NativeGitURL)
			pkg.SetVersions(node.NativeVersions)
		} else if node.Pkg != nil {
			pkg.SetGit(node.Pkg.GitURLs()...)
			pkg.SetVersions(node.Pkg.Versions())
		} else if node.Source != nil && node.Source.Path != "" {
			info, err := ParseBuildGo(node.Source.Path)
			if err == nil {
				pkg.SetVersions(info.Versions)
				pkg.SetGit(info.GitURLs...)
			}
		}

		if cfg.Version == "" && len(pkg.GetVersions()) > 0 {
			selected, err := pkg.SelectVersion("")
			if err != nil {
				return nil, err
			}
			cfg.Version = selected
		}

		sourceDir, err := sourceMgr.EnsureSource(pkg, cfg.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to download %s: %w", name, err)
		}
		vlog.Info("  %s@%s -> %s", name, cfg.Version, sourceDir)

		cacheHash := build.BuildKey(tc.Tools.CC, "release", cfg.Options)
		remote.dirs[name] = &api.PkgDirs{
			SourceDir:  sourceDir,
			BuildDir:   filepath.Join(packagesDir, name, cfg.Version, cacheHash, "build"),
			InstallDir: filepath.Join(packagesDir, name, cfg.Version, cacheHash, "install"),
		}
	}

	return remote, nil
}

func executePackageOnBuild(ctx *RuntimeContext, name string, node *resolver.PackageNode, pkgDirs map[string]string,
	allTargets map[string]map[string]*api.Target, remote *remotePkgState, globalValues map[string]any,
	buildSubGraphFn func(string) error, depOutputFn func(string) string, tc *toolchain.Toolchain) {

	buildCtx := newBuildContext(ctx, name, globalValues)
	buildCtx.SetBuildSubGraphFunc(buildSubGraphFn)
	buildCtx.SetDepOutputFunc(depOutputFn)

	node.Pkg.ExecBuildFuncs(pkgDirs[name], func(fn api.BuildFunc) {
		fn(buildCtx)
	})

	allTargets[name] = buildCtx.GetTargets()

	if !node.IsLocal() && node.Pkg != nil && tc != nil {
		pkg := node.Pkg
		cfg := remote.configs[name]
		cfgVals := make(map[string]any)
		for optName, opt := range pkg.GetOptions() {
			if opt.Default() != nil {
				cfgVals[optName] = opt.Default()
			}
		}
		for k, v := range cfg.Options {
			cfgVals[k] = v
		}
		pkg.SetDirs(*remote.dirs[name])
		pkg.SetCfgVals(cfgVals)
		pkg.SetToolchain(tc)
	}
}

func executeAllOnBuild(ctx *RuntimeContext, needed map[string]bool, remote *remotePkgState, pkgDirs map[string]string, cfg *buildConfig, localPkgOptions map[string]map[string]any) (map[string]map[string]*api.Target, map[string]build.PkgBuildMeta) {
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
			IsRemote: !node.IsLocal(),
			Deps:     node.Deps,
		}
	}

	packagesDir := getPackagesDir()
	subGraphBuilt := make(map[string]bool)
	subGraphPkgs := make(map[string]bool)

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

		for _, name := range ctx.Resolver.GetOrder() {
			node := ctx.DepGraph.Packages[name]
			if !subPkgs[name] || node.Pkg == nil {
				continue
			}
			if _, done := subAllTargets[name]; done {
				continue
			}
			executePackageOnBuild(ctx, name, node, pkgDirs, subAllTargets, remote, cfg.GlobalValues, buildSubGraphFn, depOutputFn, nil)
		}

		subTcName := resolvePkgToolchain(ctx.Config, rootPkg, cfg.TcName)
		subTc, err := toolchain.GetManager().SelectToolchain(subTcName)
		if err != nil {
			return err
		}

		subRemoteDirs := remote.dirs
		if subTcName != cfg.TcName {
			subRemoteDirs = make(map[string]*api.PkgDirs, len(remote.dirs))
			for name := range subPkgs {
				if meta, ok := pkgMetaMap[name]; ok && meta.IsRemote {
					rcfg := remote.configs[name]
					subHash := build.BuildKey(subTc.Tools.CC, "release", rcfg.Options)
					subRemoteDirs[name] = &api.PkgDirs{
						SourceDir:  remote.dirs[name].SourceDir,
						BuildDir:   filepath.Join(packagesDir, name, rcfg.Version, subHash, "build"),
						InstallDir: filepath.Join(packagesDir, name, rcfg.Version, subHash, "install"),
					}
				} else {
					subRemoteDirs[name] = remote.dirs[name]
				}
			}
		}

		params := &build.SubGraphParams{
			AllTargets:    subAllTargets,
			PkgMeta:       pkgMetaMap,
			PkgDirs:       pkgDirs,
			Packages:      make(map[string]*api.Package),
			PkgRemoteDirs: subRemoteDirs,
			Needed:        needed,
		}
		for name, node := range ctx.DepGraph.Packages {
			if node.Pkg != nil && subPkgs[name] {
				params.Packages[name] = node.Pkg
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
		pkgName, targetName, ok := strings.Cut(depRef, ":")
		if !ok {
			pkgName = depRef
			targetName = ""
		}
		subTcName := resolvePkgToolchain(ctx.Config, pkgName, cfg.TcName)
		pkgDir := pkgDirs[pkgName]
		if pkgDir == "" {
			if d, ok := remote.dirs[pkgName]; ok {
				pkgDir = d.SourceDir
			}
		}
		if targetName == "" {
			targets := allTargets[pkgName]
			if len(targets) == 1 {
				for name := range targets {
					targetName = name
				}
			}
		}
		if targetName == "" {
			return ""
		}
		target := allTargets[pkgName][targetName]
		var buildKey string
		if node := ctx.DepGraph.Packages[pkgName]; node != nil && node.IsLocal() {
			opts := localPkgOptions[pkgName]
			subTc, err := toolchain.GetManager().SelectToolchain(subTcName)
			ccPath := cfg.Tc.Tools.CC
			if err == nil {
				resolvedTools, err := build.ResolveTools(subTc)
				if err == nil {
					ccPath = resolvedTools.CC
				}
			}
			buildKey = build.BuildKey(ccPath, cfg.Mode, opts)
		} else if d, ok := remote.dirs[pkgName]; ok {
			buildKey = filepath.Base(d.BuildDir)
		}
		return build.TargetOutputPath(pkgDir, subTcName, cfg.Mode, buildKey, target.Kind(), targetName)
	}

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil {
			continue
		}
		if !node.IsLocal() && !needed[name] {
			continue
		}
		if node.Pkg == nil {
			continue
		}
		if _, done := allTargets[name]; done {
			continue
		}

		executePackageOnBuild(ctx, name, node, pkgDirs, allTargets, remote, cfg.GlobalValues, buildSubGraphFn, depOutputFn, cfg.Tc)
	}

	for pkgName := range subGraphBuilt {
		delete(allTargets, pkgName)
	}

	return allTargets, pkgMetaMap
}

func collectNeeded(graph *resolver.Graph) map[string]bool {
	needed := make(map[string]bool)
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

	os.RemoveAll(effectivePrefix)
	fs.EnsureDir(effectivePrefix)

	installer := build.NewArtifactInstaller(result.Graph, result.PkgDirs, effectivePrefix)
	installer.SetInstallType(installTypeFlag)

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() {
			continue
		}

		entry := config.GetEntry(ctx.Config, name)

		installCtx := api.NewInstallContext(name, entry.Options)
		installCtx.SetOptions(ctx.AllOptions[name])
		installCtx.MergeGlobals(ctx.GlobalOptions, globalValues)

		node.Pkg.ExecInstallFuncs(result.PkgDirs[name], func(fn api.InstallFunc) {
			fn(installCtx)
		})

		buildCtx := newBuildContext(ctx, name, globalValues)
		buildCtx.SetDryRun(true)
		node.Pkg.ExecBuildFuncs(result.PkgDirs[name], func(fn api.BuildFunc) {
			fn(buildCtx)
		})

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
			BuildDir:      result.PkgDirs[name],
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
		vlog.Info("  (manifest write failed: %v)", err)
	}

	vlog.Info("")
	vlog.Info("Install succeeded!")
	return nil
}
