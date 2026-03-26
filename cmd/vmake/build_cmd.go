package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/build"
	"gitee.com/spock2300/vmake/pkg/config"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/repo"
	"gitee.com/spock2300/vmake/pkg/resolver"
	"gitee.com/spock2300/vmake/pkg/toolchain"

	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the project",
	Long:  `Compile and link all targets defined in build.go files.`,
	Run:   runBuild,
}

func init() {
	RootCmd.AddCommand(buildCmd)
	buildCmd.Flags().BoolVarP(&installFlag, "install", "i", false, "install after build")
	buildCmd.Flags().StringVarP(&prefixFlag, "prefix", "p", "", "installation prefix (default: ./install)")
	buildCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "force buildscript recompilation")
	buildCmd.Flags().StringVar(&toolchainFlag, "toolchain", "", "override toolchain")
	buildCmd.Flags().StringVar(&modeFlag, "mode", "", "override build mode")
}

func runBuild(cmd *cobra.Command, args []string) {
	runPipeline(pipelineOptions{force: forceFlag, installAfter: installFlag})
}

type BuildResult struct {
	AllTargets    map[string]map[string]*api.Target
	Graph         *build.BuildGraph
	PkgDirs       map[string]string
	TcName        string
	Mode          string
	InstalledPkgs map[string]*api.InstalledPackage
}

func runBuildPhase(ctx *RuntimeContext) (*BuildResult, error) {
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

	apiTC := &api.Toolchain{
		CC:       tc.Tools.CC,
		CXX:      tc.Tools.CXX,
		AR:       tc.Tools.AR,
		Target:   "",
		Prefix:   tc.Prefix,
		SysRoot:  "",
		CFlags:   strings.Join(tc.DefaultFlags.CFlags, " "),
		CXXFlags: strings.Join(tc.DefaultFlags.CxxFlags, " "),
		LDFlags:  strings.Join(tc.DefaultFlags.LdFlags, " "),
	}
	if toolchain.IsCrossCompiling(tc) {
		apiTC.Target = tc.Host
	}

	// Filter deps with actual config values
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
	// Recompute order after filtering
	ctx.Resolver.UpdateOrder()

	needed := collectNeeded(ctx.DepGraph)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() && !needed[name] {
			vlog.Info("  %s: skipped (not needed)", name)
		}
	}

	hasDeps := false
	for _, name := range ctx.Resolver.GetOrder() {
		if needed[name] && !ctx.DepGraph.Packages[name].IsLocal() {
			hasDeps = true
			break
		}
	}

	packagesDir := filepath.Join(vmakeDir, "packages")
	cacheDir := filepath.Join(vmakeDir, "cache")

	pkgSourceDirs := make(map[string]string)
	pkgBuildDirs := make(map[string]string)
	pkgInstallDirs := make(map[string]string)
	configs := make(map[string]*repo.InstallConfig)
	var repoInstaller *repo.Installer

	if hasDeps {
		reposDir := filepath.Join(vmakeDir, "repos")
		repoMgr := repo.NewRepoManager(reposDir)

		for _, name := range ctx.Resolver.GetOrder() {
			node := ctx.DepGraph.Packages[name]
			if needed[name] && !node.IsLocal() {
				entry := config.GetEntry(ctx.Config, name)
				configs[name] = &repo.InstallConfig{
					Version: entry.Version,
					Options: entry.Options,
				}
			}
		}

		sourceMgr := repo.NewSourceManager(cacheDir)
		repoInstaller = repo.NewInstaller(sourceMgr, packagesDir, cacheDir)
		repoInstaller.SetRepoManager(repoMgr)
		repoInstaller.SetConfigs(configs)
		repoInstaller.SetToolchain(apiTC)

		for _, name := range ctx.Resolver.GetOrder() {
			node := ctx.DepGraph.Packages[name]
			if needed[name] && !node.IsLocal() && node.Pkg != nil {
				repoInstaller.SetPackage(name, node.Pkg)
			}
		}

		vlog.Info("")
		vlog.Info("Downloading package sources...")

		for _, name := range ctx.Resolver.GetOrder() {
			node := ctx.DepGraph.Packages[name]
			if needed[name] && !node.IsLocal() {
				cfg := configs[name]
				parts := splitPackagePath(name)
				pkg := api.NewPackage()
				pkg.SetRepo(parts[0]).SetName(parts[1])
				if node.Pkg != nil {
					pkg.SetGit(node.Pkg.GitURLs()...)
					pkg.SetVersions(node.Pkg.Versions())
				} else if node.Source != nil && node.Source.BuildGo != "" {
					pkg.SetVersions(extractVersionsFromBuildGo(node.Source.BuildGo))
					pkg.SetGit(extractGitURLs(node.Source.BuildGo)...)
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

				cacheHash := repo.CacheHash(apiTC.CC, "release", cfg.Options)
				pkgSourceDirs[name] = sourceDir
				pkgBuildDirs[name] = filepath.Join(packagesDir, name, cfg.Version, cacheHash, "build")
				pkgInstallDirs[name] = filepath.Join(packagesDir, name, cfg.Version, cacheHash, "install")
			}
		}
	}

	vlog.Info("")
	vlog.Info("Executing OnBuild...")
	allTargets := make(map[string]map[string]*api.Target)
	subBuildClaimed := make(map[string]bool)

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

		entry := config.GetEntry(ctx.Config, name)
		buildCtx := api.NewBuildContext(name, entry.Options)
		buildCtx.SetOptions(ctx.AllOptions[name])
		buildCtx.SetGlobalOptions(ctx.GlobalOptions)
		buildCtx.SetGlobalValues(globalValues)
		buildCtx.SetSubBuildFunc(func(tcName, dir string) error {
			claimedPkg := filepath.Base(filepath.Clean(dir))
			subBuildClaimed[claimedPkg] = true
			return build.SubBuild(tcName, dir)
		})

		for _, fn := range node.Pkg.GetBuildFuncs() {
			fn(buildCtx)
		}

		allTargets[name] = buildCtx.GetTargets()

		if !node.IsLocal() && node.Pkg != nil {
			pkg := node.Pkg
			cfg := configs[name]
			cfgVals := make(map[string]any)
			for optName, opt := range pkg.GetOptions() {
				if opt.Default() != nil {
					cfgVals[optName] = opt.Default()
				}
			}
			for k, v := range cfg.Options {
				cfgVals[k] = v
			}

			pkg.SetDirs(pkgSourceDirs[name], pkgBuildDirs[name], pkgInstallDirs[name])
			pkg.SetCfgVals(cfgVals)
			pkg.SetToolchain(apiTC)
		}
	}

	for pkgName := range subBuildClaimed {
		if targets, ok := allTargets[pkgName]; ok {
			for _, t := range targets {
				t.SetDefault(false)
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
	vlog.Info("Using toolchain: %s, mode: %s", tcName, mode)

	resolvePackageDeps(allTargets)

	graph, err := build.NewBuildGraph(allTargets)
	if err != nil {
		return nil, err
	}

	vlog.Info("")
	vlog.Info("Build order:")
	for _, fullName := range graph.Order {
		vlog.Info("  - %s", fullName)
	}

	pkgDirs := GetPackageDirs(ctx.DepGraph)

	scheduler, err := build.NewScheduler(graph, tc, pkgDirs, mode)
	if err != nil {
		return nil, err
	}

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.IsLocal() {
			scheduler.SetPkgDirs(name, pkgDirs[name], "", "")
		} else if needed[name] {
			scheduler.SetPkgDirs(name, pkgSourceDirs[name], pkgBuildDirs[name], pkgInstallDirs[name])
		}
	}

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() && needed[name] && node.Pkg != nil {
			scheduler.SetPackage(name, node.Pkg)
		}
	}

	versions := make(map[string]string)
	for name, cfg := range configs {
		versions[name] = cfg.Version
	}

	pkgProvider := &packageProvider{
		config:   ctx.Config,
		tc:       apiTC,
		versions: versions,
		pkgDirs:  pkgInstallDirs,
		pkgDefs:  make(map[string]*api.Package),
		pkgDeps:  make(map[string][]string),
	}
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if !needed[name] {
			continue
		}
		if !node.IsLocal() && node.Pkg != nil {
			pkgProvider.pkgDefs[name] = node.Pkg
		}
		if len(node.Deps) > 0 {
			pkgProvider.pkgDeps[name] = node.Deps
		}
	}
	scheduler.SetPackageProvider(pkgProvider)

	vlog.Info("")
	vlog.Info("Building...")
	if err := scheduler.BuildAll(); err != nil {
		return nil, err
	}

	vlog.Info("")
	vlog.Info("Build succeeded!")

	installedPkgs := make(map[string]*api.InstalledPackage)
	for name, installDir := range pkgInstallDirs {
		installedPkgs[name] = api.NewInstalledPackage(name, configs[name].Version, installDir, nil)
	}

	return &BuildResult{
		AllTargets:    allTargets,
		Graph:         graph,
		PkgDirs:       pkgDirs,
		TcName:        tcName,
		Mode:          mode,
		InstalledPkgs: installedPkgs,
	}, nil
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

type packageProvider struct {
	config   *config.ConfigFile
	tc       *api.Toolchain
	versions map[string]string
	pkgDirs  map[string]string
	pkgDefs  map[string]*api.Package
	pkgDeps  map[string][]string
}

func (p *packageProvider) GetInstalledPackage(name string) *api.InstalledPackage {
	if installDir, ok := p.pkgDirs[name]; ok {
		version := p.versions[name]
		var libs []string
		if pkg, ok := p.pkgDefs[name]; ok {
			libs = pkg.Libs()
		}
		installedPkg := api.NewInstalledPackage(name, version, installDir, libs)
		installedPkg.Deps = p.pkgDeps[name]
		return installedPkg
	}
	return nil
}

func (p *packageProvider) GetTransitivePackageNames(name string) []string {
	visited := make(map[string]bool)
	var order []string

	var collect func(string)
	collect = func(n string) {
		if visited[n] {
			return
		}
		visited[n] = true

		pkg := p.GetInstalledPackage(n)
		if pkg != nil {
			for _, dep := range pkg.Deps {
				collect(dep)
			}
		}
		order = append(order, n)
	}
	collect(name)

	var result []string
	for i := len(order) - 1; i >= 0; i-- {
		result = append(result, order[i])
	}
	return result
}

func splitPackagePath(path string) []string {
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			return []string{path[:i], path[i+1:]}
		}
	}
	return nil
}

func extractVersionsFromBuildGo(buildGoPath string) map[string]string {
	data, err := os.ReadFile(buildGoPath)
	if err != nil {
		return nil
	}
	versions := make(map[string]string)
	content := string(data)
	for i := 0; i < len(content); i++ {
		if i+11 < len(content) && content[i:i+11] == "AddVersion(" {
			args := extractCallArgs(content[i+11:])
			if len(args) >= 2 {
				versions[args[0]] = args[1]
			}
		}
	}
	return versions
}

func extractGitURLs(buildGoPath string) []string {
	data, err := os.ReadFile(buildGoPath)
	if err != nil {
		return nil
	}
	content := string(data)
	for i := 0; i < len(content); i++ {
		if i+7 < len(content) && content[i:i+7] == "SetGit(" {
			args := extractCallArgs(content[i+7:])
			if len(args) > 0 {
				return args
			}
		}
	}
	return nil
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

func resolvePackageDeps(allTargets map[string]map[string]*api.Target) {
	pkgTargets := make(map[string][]string)
	for pkgName, targets := range allTargets {
		for tName := range targets {
			pkgTargets[pkgName] = append(pkgTargets[pkgName], tName)
		}
	}
	for _, targets := range allTargets {
		for _, t := range targets {
			for _, pkgRef := range t.Packages() {
				for _, tName := range pkgTargets[pkgRef] {
					t.AddDeps(pkgRef + ":" + tName)
				}
			}
		}
	}
}

func hasInstalledFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

func executeInstall(ctx *RuntimeContext, result *BuildResult) error {
	globalValues := config.BuildGlobalValues(ctx.Config)

	vlog.Info("")
	vlog.Info("Installing...")

	installer := build.NewInstaller(result.Graph, result.PkgDirs, prefixFlag)

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() {
			continue
		}

		entry := config.GetEntry(ctx.Config, name)

		installCtx := api.NewInstallContext(name, entry.Options)
		installCtx.SetOptions(ctx.AllOptions[name])
		installCtx.SetGlobalOptions(ctx.GlobalOptions)
		installCtx.SetGlobalValues(globalValues)

		for _, fn := range node.Pkg.GetInstallFuncs() {
			fn(installCtx)
		}

		buildCtx := api.NewBuildContext(name, entry.Options)
		buildCtx.SetOptions(ctx.AllOptions[name])
		buildCtx.SetGlobalOptions(ctx.GlobalOptions)
		buildCtx.SetGlobalValues(globalValues)
		for _, fn := range node.Pkg.GetBuildFuncs() {
			fn(buildCtx)
		}

		installItems := installCtx.GetInstallItems()
		installItems = append(installItems, buildCtx.GetInstallItems()...)

		prefix := installCtx.Prefix()
		if !installCtx.PrefixSet() {
			prefix = prefixFlag
		}

		var installFilter api.InstallFilterFunc
		if installCtx.GetInstallFilter() != nil {
			installFilter = installCtx.GetInstallFilter()
		} else if buildCtx.GetInstallFilter() != nil {
			installFilter = buildCtx.GetInstallFilter()
		}

		installer.SetPackageInfo(name, &build.PkgInstallInfo{
			Prefix:        prefix,
			PrefixSet:     installCtx.PrefixSet(),
			Targets:       result.AllTargets[name],
			InstallItems:  installItems,
			BuildDir:      result.PkgDirs[name],
			Mode:          result.Mode,
			TcName:        result.TcName,
			InstallFilter: installFilter,
		})
	}

	if err := installer.InstallAll(); err != nil {
		return err
	}

	vlog.Info("")
	vlog.Info("Install succeeded!")
	return nil
}
