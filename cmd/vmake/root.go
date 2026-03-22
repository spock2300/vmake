package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/config"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/plugin"
	"gitee.com/spock2300/vmake/pkg/repo"
	"gitee.com/spock2300/vmake/pkg/toolchain"

	"github.com/spf13/cobra"
)

var (
	verbose     bool
	veryVerbose bool
	quiet       bool
	vmakeDir    string
)

func init() {
	homeDir, _ := os.UserHomeDir()
	vmakeDir = filepath.Join(homeDir, ".vmake")
	RootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	RootCmd.PersistentFlags().BoolVarP(&veryVerbose, "very-verbose", "V", false, "very verbose output")
	RootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "quiet mode")
}

type RuntimeContext struct {
	WorkDir       string
	Config        *config.ConfigFile
	ConfigPath    string
	DepGraph      *repo.DependencyGraph
	AllOptions    map[string]map[string]*api.Option
	GlobalOptions map[string]*api.Option
	Resolver      *repo.Resolver
}

var RootCmd = &cobra.Command{
	Use:   "vmake",
	Short: "VMake - A Go-based C/C++ build system",
	Long: `VMake is a minimal build system for C/C++ projects.
It uses Go plugins for configuration and provides a TUI for option management.`,
	Run: func(cmd *cobra.Command, args []string) {
		runBuild(cmd, args)
	},
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		switch {
		case veryVerbose:
			vlog.SetLevel(vlog.VeryVerbose)
		case verbose:
			vlog.SetLevel(vlog.Verbose)
		case quiet:
			vlog.SetLevel(vlog.Quiet)
		}
	},
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func initContext() (*RuntimeContext, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(workDir, ".vmake", "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	return &RuntimeContext{
		WorkDir:    workDir,
		Config:     cfg,
		ConfigPath: configPath,
	}, nil
}

func mustInitContext() *RuntimeContext {
	ctx, err := initContext()
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}
	return ctx
}

type pipelineOptions struct {
	force        bool
	afterPhase1  func(ctx *RuntimeContext)
	installAfter bool
}

func runPipeline(opts pipelineOptions) {
	ctx := mustInitContext()

	if err := runRequirePhase(ctx, opts.force); err != nil {
		vlog.Error("Phase 1 (OnRequire) failed: %v", err)
		os.Exit(1)
	}

	if opts.afterPhase1 != nil {
		opts.afterPhase1(ctx)
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

	if opts.installAfter {
		if err := executeInstall(ctx, result); err != nil {
			vlog.Error("Install error: %v", err)
			os.Exit(1)
		}
	}
}

func runRequirePhase(ctx *RuntimeContext, force bool) error {
	vlog.Info("Scanning %s...", ctx.WorkDir)

	packages, err := plugin.Scan(ctx.WorkDir)
	if err != nil {
		return err
	}

	if len(packages) == 0 {
		return fmt.Errorf("no build.go files found")
	}

	for i := range packages {
		packages[i].Force = force
	}

	var pkgNames []string
	for _, pkg := range packages {
		pkgNames = append(pkgNames, pkg.Name)
	}
	vlog.Info("Found %d package(s): %s", len(packages), strings.Join(pkgNames, ", "))

	reposDir := filepath.Join(vmakeDir, "repos")
	cacheDir := filepath.Join(vmakeDir, "cache")

	repoMgr := repo.NewRepoManager(reposDir)
	resolver := repo.NewResolver(repoMgr, cacheDir)
	ctx.Resolver = resolver

	vlog.Info("")
	vlog.Info("Resolving dependencies...")

	graph, err := resolver.ResolveWithLocal(packages, force)
	if err != nil {
		return err
	}

	ctx.DepGraph = graph

	for _, name := range graph.Order {
		node := graph.Packages[name]
		if node.IsLocal() {
			vlog.Info("  %s (local)", name)
		} else if node.Deferred {
			vlog.Info("  %s (deferred)", name)
		} else {
			vlog.Info("  %s", name)
		}
	}

	return nil
}

func collectOptions(name string, pkg *api.Package) map[string]*api.Option {
	cfgCtx := api.NewConfigContext(name)
	for _, fn := range pkg.GetConfigFuncs() {
		fn(cfgCtx)
	}
	return cfgCtx.GetOptions()
}

func runConfigPhase(ctx *RuntimeContext) error {
	vlog.Info("")
	vlog.Info("Executing OnConfig...")

	ctx.AllOptions = make(map[string]map[string]*api.Option)

	if ctx.Resolver != nil {
		for _, name := range ctx.DepGraph.Order {
			node := ctx.DepGraph.Packages[name]
			if !node.IsLocal() && node.Deferred {
				if err := ctx.Resolver.ResolveSingle(name, ctx.DepGraph); err != nil {
					vlog.Info("  %s: resolve deferred: %v", name, err)
				}
			}
		}
	}

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]

		var opts map[string]*api.Option
		if node.Definition != nil {
			opts = collectOptions(name, node.Definition)
		}

		if len(opts) > 0 {
			ctx.AllOptions[name] = opts
			vlog.Info("  %s: %d option(s)", name, len(opts))
		}
	}

	mgr := toolchain.GetManager()
	var tcList []string
	if tcs, err := mgr.ListToolchains(); err == nil {
		for name := range tcs {
			tcList = append(tcList, name)
		}
		sort.Strings(tcList)
	}

	var err error
	ctx.GlobalOptions, err = api.MergeGlobalOptions(ctx.AllOptions, tcList)
	if err != nil {
		return fmt.Errorf("global options error: %w", err)
	}

	return nil
}

func GetToolchain(cfg *config.ConfigFile) (*toolchain.Toolchain, string, error) {
	mgr := toolchain.GetManager()
	tcName := cfg.Global.Toolchain
	if tcName == "" {
		tcName = mgr.GetDefaultToolchain()
	}
	tc, err := mgr.SelectToolchain(tcName)
	if err != nil {
		return nil, "", err
	}
	return tc, tcName, nil
}

func GetPackageDirs(graph *repo.DependencyGraph) map[string]string {
	dirs := make(map[string]string)
	for name, node := range graph.Packages {
		if node.IsLocal() && node.Source != nil {
			dirs[name] = node.Source.Dir
		}
	}
	return dirs
}

func getVMakeSourceDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exeDir := filepath.Dir(exe)
	goMod := filepath.Join(exeDir, "go.mod")
	if _, err := os.Stat(goMod); err == nil {
		return exeDir
	}
	parent := filepath.Dir(exeDir)
	goMod = filepath.Join(parent, "go.mod")
	if _, err := os.Stat(goMod); err == nil {
		return parent
	}
	return ""
}

var _ = fmt.Sprintf
