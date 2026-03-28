package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/build"
	"gitee.com/spock2300/vmake/pkg/config"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/repo"
	"gitee.com/spock2300/vmake/pkg/resolver"

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
	addInstallFlags(buildCmd)
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

	packagesDir := getPackagesDir()
	cacheDir := getCacheDir()

	pkgSourceDirs := make(map[string]string)
	pkgBuildDirs := make(map[string]string)
	pkgInstallDirs := make(map[string]string)
	configs := make(map[string]*config.EntryConfig)
	var repoInstaller *repo.PackageInstaller

	if hasDeps {
		repoMgr := getRepoManager()

		for _, name := range ctx.Resolver.GetOrder() {
			node := ctx.DepGraph.Packages[name]
			if needed[name] && !node.IsLocal() {
				entry := config.GetEntry(ctx.Config, name)
				configs[name] = entry
			}
		}

		sourceMgr := repo.NewSourceManager(cacheDir)
		repoInstaller = repo.NewPackageInstaller(sourceMgr, packagesDir, cacheDir)
		repoInstaller.SetRepoManager(repoMgr)
		repoInstaller.SetConfigs(configs)
		repoInstaller.SetToolchain(tc)

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
				repoName, pkgName, ok := api.SplitPackageRef(name)
				if !ok {
					continue
				}
				pkg := api.NewPackage()
				pkg.SetRepo(repoName).SetName(pkgName)
				if node.Pkg != nil {
					pkg.SetGit(node.Pkg.GitURLs()...)
					pkg.SetVersions(node.Pkg.Versions())
				} else if node.Source != nil && node.Source.Path != "" {
					pkg.SetVersions(extractVersionsFromBuildGo(node.Source.Path))
					pkg.SetGit(extractGitURLs(node.Source.Path)...)
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

				cacheHash := repo.CacheHash(tc.Tools.CC, "release", cfg.Options)
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
	pkgDirs := GetPackageDirs(ctx.DepGraph)

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
		buildCtx.MergeGlobals(ctx.GlobalOptions, globalValues)
		buildCtx.SetSubBuildFunc(func(tcName, dir string, args ...string) error {
			claimedPkg := filepath.Base(filepath.Clean(dir))
			subBuildClaimed[claimedPkg] = true
			switch {
			case veryVerbose:
				args = append(args, "-V")
			case verbose:
				args = append(args, "-v")
			}
			return build.SubBuild(tcName, dir, args...)
		})

		node.Pkg.ExecBuildFuncs(pkgDirs[name], func(fn api.BuildFunc) {
			fn(buildCtx)
		})

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
			pkg.SetToolchain(tc)
		}
	}

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() && needed[name] && node.Pkg != nil {
			if err := applyPatches(node.Pkg, pkgSourceDirs[name]); err != nil {
				return nil, fmt.Errorf("apply patches for %s: %w", name, err)
			}
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

	graph, err := build.NewBuildGraph(allTargets, pkgMetaMap)
	if err != nil {
		return nil, err
	}

	vlog.Info("")
	vlog.Info("Build order:")
	for _, fullName := range graph.Order {
		vlog.Info("  - %s", fullName)
	}

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

func executeInstall(ctx *RuntimeContext, result *BuildResult) error {
	globalValues := config.BuildGlobalValues(ctx.Config)

	vlog.Info("")
	vlog.Info("Installing...")

	installer := build.NewArtifactInstaller(result.Graph, result.PkgDirs, prefixFlag)

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

		buildCtx := api.NewBuildContext(name, entry.Options)
		buildCtx.SetOptions(ctx.AllOptions[name])
		buildCtx.MergeGlobals(ctx.GlobalOptions, globalValues)
		node.Pkg.ExecBuildFuncs(result.PkgDirs[name], func(fn api.BuildFunc) {
			fn(buildCtx)
		})

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
