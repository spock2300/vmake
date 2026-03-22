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

	var needed map[string]bool
	if ctx.Resolver != nil {
		// Step 1: Resolve deferred packages
		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if !node.IsLocal() && node.Deferred {
				vlog.Info("Resolving %s...", name)
				if err := ctx.Resolver.ResolveSingle(name, ctx.DepGraph); err != nil {
					return nil, fmt.Errorf("resolve %s: %w", name, err)
				}
			}
		}
		// Step 2: Collect options for all non-local packages
		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if !node.IsLocal() && node.Definition != nil {
				if _, exists := ctx.AllOptions[name]; !exists {
					opts := collectOptions(name, node.Definition)
					if len(opts) > 0 {
						ctx.AllOptions[name] = opts
						vlog.Info("  %s: %d option(s) (deferred)", name, len(opts))
					}
				}
			}
		}
		// Step 3: Filter deps with actual config (AFTER resolve + options)
		vlog.Info("")
		vlog.Info("Filtering dependencies...")
		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if node.Definition == nil {
				continue
			}
			entry := config.GetEntry(ctx.Config, name)
			opts := ctx.AllOptions[name]
			if err := ctx.Resolver.FilterDeps(name, ctx.DepGraph, entry.Options, opts); err != nil {
				vlog.Error("  %s: filter deps: %v", name, err)
			} else if len(node.Deps) > 0 {
				vlog.Info("  %s: deps=%v", name, node.Deps)
			}
		}
		// Recompute needed AFTER filtering
		needed = collectNeeded(ctx.DepGraph)
		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if !node.IsLocal() && !needed[name] {
				vlog.Info("  %s: skipped (not needed)", name)
			}
		}
	} else {
		needed = collectNeeded(ctx.DepGraph)
	}

	hasDeps := false
	for _, name := range ctx.DepGraph.Order {
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

		for _, name := range ctx.DepGraph.Order {
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

		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if needed[name] && !node.IsLocal() && node.Definition != nil {
				repoInstaller.SetPackage(name, node.Definition)
			}
		}

		vlog.Info("")
		vlog.Info("Downloading package sources...")

		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if needed[name] && !node.IsLocal() {
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
	packageContexts := make(map[string]*api.PackageContext)

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() && !needed[name] {
			continue
		}

		entry := config.GetEntry(ctx.Config, name)
		buildCtx := api.NewBuildContext(name, entry.Options)
		buildCtx.SetOptions(ctx.AllOptions[name])
		buildCtx.SetGlobalOptions(ctx.GlobalOptions)
		buildCtx.SetGlobalValues(globalValues)

		for _, fn := range node.Definition.GetBuildFuncs() {
			fn(buildCtx)
		}

		allTargets[name] = buildCtx.GetTargets()

		if !node.IsLocal() && node.Definition != nil {
			pkg := node.Definition
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

			packageContexts[name] = pkgCtx
		}
	}

	// Inject active package configs into installer so Dep() builds with correct options
	if repoInstaller != nil {
		for _, name := range ctx.DepGraph.Order {
			if needed[name] && !ctx.DepGraph.Packages[name].IsLocal() {
				if _, ok := configs[name]; ok {
					repoInstaller.SetConfig(name, configs[name])
				}
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
		} else if needed[name] {
			scheduler.SetPkgDirs(name, pkgSourceDirs[name], pkgBuildDirs[name], pkgInstallDirs[name])
		}
	}

	for name, pkgCtx := range packageContexts {
		scheduler.SetPackageContext(name, pkgCtx)
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
		if !needed[name] {
			continue
		}
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

func collectNeeded(graph *repo.DependencyGraph) map[string]bool {
	needed := make(map[string]bool)
	var queue []string
	for _, node := range graph.Packages {
		if node.IsLocal() {
			needed[node.Name] = true
			queue = append(queue, node.Name)
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

		for _, fn := range node.Definition.GetInstallFuncs() {
			fn(installCtx)
		}

		buildCtx := api.NewBuildContext(name, entry.Options)
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
