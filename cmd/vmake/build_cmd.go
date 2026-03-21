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
	buildCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "force plugin recompilation")
}

func runBuild(cmd *cobra.Command, args []string) {
	ctx, err := initContext()
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}

	if err := runRequirePhase(ctx, forceFlag); err != nil {
		vlog.Error("Phase 1 (OnRequire) failed: %v", err)
		os.Exit(1)
	}

	if err := runConfigPhase(ctx); err != nil {
		vlog.Error("Phase 2 (OnConfig) failed: %v", err)
		os.Exit(1)
	}

	result, err := runBuildPhase(ctx)
	if err != nil {
		vlog.Error("Phase 3 (OnBuild) failed: %v", err)
		os.Exit(1)
	}

	if installFlag {
		if err := executeInstall(ctx, result); err != nil {
			vlog.Error("Install error: %v", err)
			os.Exit(1)
		}
	}
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
	globalValues := make(map[string]any)
	if ctx.Config.Global != nil {
		globalValues["toolchain"] = ctx.Config.Global.Toolchain
		globalValues["mode"] = ctx.Config.Global.Mode
		for k, v := range ctx.Config.Global.Options {
			globalValues[k] = v
		}
	}

	mode := ""
	if m, ok := globalValues["mode"].(string); ok {
		mode = m
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
		SysRoot:  "",
		CFlags:   strings.Join(tc.DefaultFlags.CFlags, " "),
		CXXFlags: strings.Join(tc.DefaultFlags.CxxFlags, " "),
		LDFlags:  strings.Join(tc.DefaultFlags.LdFlags, " "),
	}
	if toolchain.IsCrossCompiling(tc) {
		apiTC.Target = tc.Host
	}

	if ctx.Resolver != nil {
		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if !node.IsLocal() && node.Deferred {
				vlog.Info("Resolving %s...", name)
				if err := ctx.Resolver.ResolveSingle(name, ctx.DepGraph); err != nil {
					return nil, fmt.Errorf("resolve %s: %w", name, err)
				}
			}
		}
		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if !node.IsLocal() && node.Definition != nil {
				if _, exists := ctx.AllOptions[name]; !exists {
					cfgCtx := api.NewConfigContext(name)
					for _, fn := range node.Definition.GetConfigFuncs() {
						fn(cfgCtx)
					}
					opts := cfgCtx.GetOptions()
					if len(opts) > 0 {
						ctx.AllOptions[name] = opts
						vlog.Info("  %s: %d option(s) (deferred)", name, len(opts))
					}
				}
			}
		}
	}

	hasDeps := false
	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() {
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

		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if !node.IsLocal() {
				rc := config.GetRequireConfig(ctx.Config, name)
				configs[name] = &repo.InstallConfig{
					Version: rc.Version,
					Options: rc.Options,
				}
			}
		}

		sourceMgr := repo.NewSourceManager(cacheDir)
		repoInstaller = repo.NewInstaller(sourceMgr, packagesDir, cacheDir)
		repoInstaller.SetRepoManager(repoMgr)
		repoInstaller.SetConfigs(configs)
		repoInstaller.SetToolchain(apiTC)

		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if !node.IsLocal() && node.Definition != nil {
				repoInstaller.SetPackage(name, node.Definition)
			}
		}

		vlog.Info("")
		vlog.Info("Downloading package sources...")

		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if !node.IsLocal() {
				cfg := configs[name]
				pkgDef := repo.NewPackageDef(splitPackagePath(name)[0], splitPackagePath(name)[1])
				if node.Definition != nil {
					pkgDef.SetPackage(node.Definition)
				}

				if cfg.Version == "" && node.Definition != nil {
					selected, err := pkgDef.SelectVersion(node.Constraint)
					if err != nil {
						return nil, err
					}
					cfg.Version = selected
				}

				sourceDir, err := sourceMgr.EnsureSource(pkgDef, cfg.Version)
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

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]

		if node.IsLocal() {
			pc := config.GetPackageConfig(ctx.Config, name)
			buildCtx := api.NewBuildContext(name, pc.Options)
			buildCtx.SetOptions(ctx.AllOptions[name])
			buildCtx.SetGlobalOptions(ctx.GlobalOptions)
			buildCtx.SetGlobalValues(globalValues)

			for _, fn := range node.Definition.GetBuildFuncs() {
				fn(buildCtx)
			}

			allTargets[name] = buildCtx.GetTargets()
		} else {
			pkg := node.Definition
			if pkg != nil && pkg.GetPackageBuildFunc() != nil {
				if hasInstalledFiles(pkgInstallDirs[name]) {
					continue
				}
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

				pkgCtx := api.NewPackageContext(name, cfg.Version, apiTC, cfgVals)
				pkgCtx.SetOptions(pkg.GetOptions())
				pkgCtx.SetDirs(pkgSourceDirs[name], pkgBuildDirs[name], pkgInstallDirs[name])
				if repoInstaller != nil {
					pkgCtx.SetInstaller(repoInstaller)
				}

				pkg.GetPackageBuildFunc()(pkgCtx)
				allTargets[name] = pkgCtx.GetTargets()
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

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]

		if node.IsLocal() {
			scheduler.SetPkgDirs(name, pkgDirs[name], "", "")
		} else {
			scheduler.SetPkgDirs(name, pkgSourceDirs[name], pkgBuildDirs[name], pkgInstallDirs[name])
		}
	}

	versions := make(map[string]string)
	for name, cfg := range configs {
		versions[name] = cfg.Version
	}

	pkgProvider := &packageProvider{
		installer: repoInstaller,
		config:    ctx.Config,
		tc:        apiTC,
		versions:  versions,
		pkgDirs:   pkgInstallDirs,
		pkgDefs:   make(map[string]*api.Package),
		pkgDeps:   make(map[string][]string),
	}
	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() && node.Definition != nil {
			pkgProvider.pkgDefs[name] = node.Definition
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

type packageProvider struct {
	installer *repo.Installer
	config    *config.ConfigFile
	tc        *api.Toolchain
	versions  map[string]string
	pkgDirs   map[string]string
	pkgDefs   map[string]*api.Package
	pkgDeps   map[string][]string
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

func hasInstalledFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

func executeInstall(ctx *RuntimeContext, result *BuildResult) error {
	globalValues := make(map[string]any)
	if ctx.Config.Global != nil {
		globalValues["toolchain"] = ctx.Config.Global.Toolchain
		globalValues["mode"] = ctx.Config.Global.Mode
		for k, v := range ctx.Config.Global.Options {
			globalValues[k] = v
		}
	}

	vlog.Info("")
	vlog.Info("Installing...")

	installer := build.NewInstaller(result.Graph, result.PkgDirs, prefixFlag)

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() {
			continue
		}

		pc := config.GetPackageConfig(ctx.Config, name)

		installCtx := api.NewInstallContext(name, pc.Options)
		installCtx.SetOptions(ctx.AllOptions[name])
		installCtx.SetGlobalOptions(ctx.GlobalOptions)
		installCtx.SetGlobalValues(globalValues)

		for _, fn := range node.Definition.GetInstallFuncs() {
			fn(installCtx)
		}

		buildCtx := api.NewBuildContext(name, pc.Options)
		buildCtx.SetOptions(ctx.AllOptions[name])
		buildCtx.SetGlobalOptions(ctx.GlobalOptions)
		buildCtx.SetGlobalValues(globalValues)
		for _, fn := range node.Definition.GetBuildFuncs() {
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
