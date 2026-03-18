package main

import (
	"os"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/build"
	"gitee.com/spock2300/vmake/pkg/config"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/repo"

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
}

func runBuild(cmd *cobra.Command, args []string) {
	ctx, err := initContext()
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}

	if err := runRequirePhase(ctx); err != nil {
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
		Target:   tc.Host,
		SysRoot:  "",
		CFlags:   strings.Join(tc.DefaultFlags.CFlags, " "),
		CXXFlags: strings.Join(tc.DefaultFlags.CxxFlags, " "),
		LDFlags:  strings.Join(tc.DefaultFlags.LdFlags, " "),
	}

	installedPkgs := make(map[string]*api.InstalledPackage)
	var pkgProvider *packageProvider

	hasDeps := false
	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() {
			hasDeps = true
			break
		}
	}

	if hasDeps {
		reposDir := filepath.Join(vmakeDir, "repos")
		packagesDir := filepath.Join(vmakeDir, "packages")
		cacheDir := filepath.Join(vmakeDir, "cache")

		repoMgr := repo.NewRepoManager(reposDir)

		configs := make(map[string]*repo.InstallConfig)
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
		installer := repo.NewInstaller(sourceMgr, packagesDir, cacheDir)
		installer.SetRepoManager(repoMgr)
		installer.SetConfigs(configs)
		installer.SetToolchain(apiTC)

		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if !node.IsLocal() && node.Definition != nil {
				installer.SetPackage(name, node.Definition)
			}
		}

		vlog.Info("")
		vlog.Info("Installing packages...")

		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if !node.IsLocal() {
				cfg := configs[name]
				if cfg.Version == "" && node.Definition != nil {
					pkgDef := repo.NewPackageDef(splitPackagePath(name)[0], splitPackagePath(name)[1])
					pkgDef.SetPackage(node.Definition)
					selected, err := pkgDef.SelectVersion(node.Constraint)
					if err != nil {
						return nil, err
					}
					cfg.Version = selected
				}

				installedPkg := installer.EnsureInstalled(name)
				if installedPkg != nil {
					vlog.Info("  %s@%s", name, cfg.Version)
					installedPkgs[name] = installedPkg
				}
			}
		}

		versions := make(map[string]string)
		for name, cfg := range configs {
			versions[name] = cfg.Version
		}

		pkgProvider = &packageProvider{
			installer: installer,
			config:    ctx.Config,
			tc:        apiTC,
			versions:  versions,
		}
	}

	vlog.Info("")
	vlog.Info("Executing OnBuild...")
	allTargets := make(map[string]map[string]*api.Target)

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() {
			continue
		}

		pc := config.GetPackageConfig(ctx.Config, name)
		buildCtx := api.NewBuildContext(name, pc.Options)
		buildCtx.SetOptions(ctx.AllOptions[name])
		buildCtx.SetGlobalOptions(ctx.GlobalOptions)
		buildCtx.SetGlobalValues(globalValues)

		for _, dep := range node.Deps {
			if pkg, ok := installedPkgs[dep]; ok {
				buildCtx.AddPackages(pkg.Name)
			}
		}

		for _, fn := range node.Plugin.Builder.GetBuildFuncs() {
			fn(buildCtx)
		}

		allTargets[name] = buildCtx.GetTargets()
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

	if pkgProvider != nil {
		scheduler.SetPackageProvider(pkgProvider)
	}

	vlog.Info("")
	vlog.Info("Building...")
	if err := scheduler.BuildAll(); err != nil {
		return nil, err
	}

	vlog.Info("")
	vlog.Info("Build succeeded!")

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
}

func (p *packageProvider) GetInstalledPackage(name string) *api.InstalledPackage {
	rc := config.GetRequireConfig(p.config, name)
	version := rc.Version
	if version == "" {
		version = p.versions[name]
	}
	return p.installer.GetInstalledPackage(name, version, p.tc, rc.Options)
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

		for _, fn := range node.Plugin.Builder.GetInstallFuncs() {
			fn(installCtx)
		}

		buildCtx := api.NewBuildContext(name, pc.Options)
		buildCtx.SetOptions(ctx.AllOptions[name])
		buildCtx.SetGlobalOptions(ctx.GlobalOptions)
		buildCtx.SetGlobalValues(globalValues)
		for _, fn := range node.Plugin.Builder.GetBuildFuncs() {
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
