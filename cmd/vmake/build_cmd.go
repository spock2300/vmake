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
	ctx, err := PrepareFull()
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}

	result, err := executeBuild(ctx)
	if err != nil {
		vlog.Error("Error: %v", err)
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
	AllTargets map[string]map[string]*api.Target
	Graph      *build.BuildGraph
	PkgDirs    map[string]string
	TcName     string
	Mode       string
}

func executeBuild(ctx *BuildContext) (*BuildResult, error) {
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

	var pkgProvider *packageProvider
	if len(ctx.Requires) > 0 {
		vlog.Info("")
		vlog.Info("Installing packages...")

		reposDir := filepath.Join(vmakeDir, "repos")
		packagesDir := filepath.Join(vmakeDir, "packages")
		cacheDir := filepath.Join(vmakeDir, "cache")

		repoMgr := repo.NewRepoManager(reposDir)
		loader := repo.NewPackageLoader(cacheDir)
		resolver := repo.NewResolverWithLoader(repoMgr, loader)
		graph, err := resolver.Resolve(ctx.Requires)
		if err != nil {
			return nil, err
		}

		constraints := make(map[string]string)
		for _, req := range ctx.Requires {
			constraints[req.Name] = req.Constraint
		}

		configs := make(map[string]*repo.InstallConfig)
		for _, req := range ctx.Requires {
			rc := config.GetRequireConfig(ctx.Config, req.Name)
			configs[req.Name] = &repo.InstallConfig{
				Version: rc.Version,
				Options: rc.Options,
			}
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

		sourceMgr := repo.NewSourceManager(cacheDir)
		installer := repo.NewInstaller(sourceMgr, packagesDir, cacheDir)

		for _, name := range graph.Order {
			pkgResolved := graph.Packages[name]
			if pkgResolved == nil {
				continue
			}

			cfg := configs[name]
			if cfg == nil {
				cfg = &repo.InstallConfig{Options: make(map[string]any)}
				configs[name] = cfg
			}

			parts := splitPackagePath(name)
			if len(parts) != 2 {
				continue
			}

			pkg := pkgResolved.Definition
			if pkg == nil {
				pkgGoPath, err := repoMgr.FindPackageGo(parts[0], parts[1])
				if err != nil {
					return nil, err
				}
				pkg, err = loader.Load(pkgGoPath)
				if err != nil {
					return nil, err
				}
			}

			installer.SetPackage(name, pkg)

			pkgDef := repo.NewPackageDef(parts[0], parts[1])
			pkgDef.SetPackage(pkg)

			if cfg.Version == "" {
				constraint := constraints[name]
				selected, err := pkgDef.SelectVersion(constraint)
				if err != nil {
					return nil, err
				}
				cfg.Version = selected
			}

			if installer.IsInstalled(name, cfg.Version, apiTC, cfg.Options) {
				vlog.Info("  %s (cached)", name)
				continue
			}

			vlog.Info("  %s...", name)

			sourceDir, err := sourceMgr.EnsureSource(pkgDef, cfg.Version)
			if err != nil {
				return nil, err
			}

			if err := installer.InstallPackage(pkgDef, cfg, apiTC, graph, sourceDir, configs); err != nil {
				return nil, err
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
			graph:     graph,
		}
	}

	vlog.Info("")
	vlog.Info("Executing OnBuild...")
	allTargets := make(map[string]map[string]*api.Target)

	for _, lr := range ctx.LoadResults {
		if !lr.Success {
			continue
		}

		pkgName := lr.Package.Name
		pc := config.GetPackageConfig(ctx.Config, pkgName)
		buildCtx := api.NewBuildContext(pkgName, pc.Options)
		buildCtx.SetOptions(ctx.AllOptions[pkgName])
		buildCtx.SetGlobalOptions(ctx.GlobalOptions)
		buildCtx.SetGlobalValues(globalValues)

		for _, fn := range lr.Loaded.Builder.GetBuildFuncs() {
			fn(buildCtx)
		}

		allTargets[pkgName] = buildCtx.GetTargets()
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

	pkgDirs := GetPackageDirs(ctx.Packages)

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
		AllTargets: allTargets,
		Graph:      graph,
		PkgDirs:    pkgDirs,
		TcName:     tcName,
		Mode:       mode,
	}, nil
}

type packageProvider struct {
	installer *repo.Installer
	config    *config.ConfigFile
	tc        *api.Toolchain
	versions  map[string]string
	graph     *repo.DependencyGraph
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
	if p.graph == nil {
		return []string{name}
	}

	visited := make(map[string]bool)
	var collect func(string)
	collect = func(n string) {
		if visited[n] {
			return
		}
		visited[n] = true
		pkg := p.graph.Packages[n]
		if pkg != nil {
			for _, dep := range pkg.Deps {
				collect(dep)
			}
		}
	}
	collect(name)

	var result []string
	for i := len(p.graph.Order) - 1; i >= 0; i-- {
		n := p.graph.Order[i]
		if visited[n] {
			result = append(result, n)
		}
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

func executeInstall(ctx *BuildContext, result *BuildResult) error {
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

	for _, lr := range ctx.LoadResults {
		if !lr.Success {
			continue
		}

		pkgName := lr.Package.Name
		pc := config.GetPackageConfig(ctx.Config, pkgName)

		installCtx := api.NewInstallContext(pkgName, pc.Options)
		installCtx.SetOptions(ctx.AllOptions[pkgName])
		installCtx.SetGlobalOptions(ctx.GlobalOptions)
		installCtx.SetGlobalValues(globalValues)

		for _, fn := range lr.Loaded.Builder.GetInstallFuncs() {
			fn(installCtx)
		}

		buildCtx := api.NewBuildContext(pkgName, pc.Options)
		buildCtx.SetOptions(ctx.AllOptions[pkgName])
		buildCtx.SetGlobalOptions(ctx.GlobalOptions)
		buildCtx.SetGlobalValues(globalValues)
		for _, fn := range lr.Loaded.Builder.GetBuildFuncs() {
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

		installer.SetPackageInfo(pkgName, &build.PkgInstallInfo{
			Prefix:        prefix,
			PrefixSet:     installCtx.PrefixSet(),
			Targets:       result.AllTargets[pkgName],
			InstallItems:  installItems,
			BuildDir:      result.PkgDirs[pkgName],
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
