package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/buildscript"
	"gitee.com/spock2300/vmake/pkg/config"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/resolver"
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
	RootCmd.AddCommand(newQueryCmd())
}

type RuntimeContext struct {
	WorkDir       string
	Config        *config.ConfigFile
	ConfigPath    string
	DepGraph      *resolver.Graph
	AllOptions    map[string]map[string]*api.Option
	GlobalOptions map[string]*api.Option
	Resolver      *resolver.Resolver
}

var RootCmd = &cobra.Command{
	Use:   "vmake",
	Short: "VMake - A Go-based C/C++ build system",
	Long: `VMake is a minimal build system for C/C++ projects.
It uses Go buildscripts for configuration and provides a TUI for option management.`,
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

func runPostPhase1(ctx *RuntimeContext) {
	fatalErr(ctx.Resolver.ResolveDeferred())
	ctx.Resolver.UpdateOrder()
	fatalErr(runConfigPhase(ctx))
}

func runThroughConfigPhase(ctx *RuntimeContext, force bool) {
	fatalErr(runRequirePhase(ctx, force))
	runPostPhase1(ctx)
}

func runPipeline(opts pipelineOptions) {
	ctx := mustInitContext()

	fatalErr(runRequirePhase(ctx, opts.force))

	if opts.afterPhase1 != nil {
		opts.afterPhase1(ctx)
	}

	runPostPhase1(ctx)

	result, err := runBuildPhase(ctx)
	fatalErr(err)

	if opts.installAfter {
		fatalErr(executeInstall(ctx, result))
	}
}

func resolveMode(cfg *config.ConfigFile, flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if cfg.Global != nil && cfg.Global.Mode != "" {
		return cfg.Global.Mode
	}
	return api.ModeDebug
}

func resolvePkgToolchain(cfg *config.ConfigFile, pkgName, defaultTc string) string {
	entry := config.GetEntry(cfg, pkgName)
	if v, ok := entry.Options["toolchain"].(string); ok && v != "" {
		return v
	}
	return defaultTc
}

func newBuildContext(ctx *RuntimeContext, name string, globalValues map[string]any) *api.BuildContext {
	entry := config.GetEntry(ctx.Config, name)
	buildCtx := api.NewBuildContext(name, entry.Options)
	if opts, ok := ctx.AllOptions[name]; ok {
		buildCtx.SetOptions(opts)
	}
	buildCtx.MergeGlobals(ctx.GlobalOptions, globalValues)
	return buildCtx
}

func runRequirePhase(ctx *RuntimeContext, force bool) error {
	vlog.Info("Scanning %s...", ctx.WorkDir)

	packages, err := buildscript.Scan(ctx.WorkDir)
	if err != nil {
		return err
	}

	if len(packages) == 0 {
		return fmt.Errorf("no build.go files found")
	}

	var pkgNames []string
	for _, pkg := range packages {
		pkgNames = append(pkgNames, pkg.Name)
	}
	vlog.Info("Found %d package(s): %s", len(packages), strings.Join(pkgNames, ", "))

	r := resolver.NewResolver(getRepoManager(), getCacheDir())
	r.SetForce(force)
	ctx.Resolver = r

	vlog.Info("")
	vlog.Info("Resolving dependencies...")

	if err := r.ResolveAll(packages); err != nil {
		return err
	}

	ctx.DepGraph = r.Graph()

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
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

func collectOptions(name, dir string, pkg *api.Package) map[string]*api.Option {
	cfgCtx := api.NewConfigContext(name)
	pkg.ExecConfigFuncs(dir, func(fn api.ConfigFunc) { fn(cfgCtx) })
	return cfgCtx.GetOptions()
}

func runConfigPhase(ctx *RuntimeContext) error {
	vlog.Info("")
	vlog.Info("Executing OnConfig...")

	ctx.AllOptions = make(map[string]map[string]*api.Option)
	pkgDirs := GetPackageDirs(ctx.DepGraph)

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]

		var opts map[string]*api.Option
		if node.Pkg != nil {
			opts = collectOptions(name, pkgDirs[name], node.Pkg)
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

func resolveToolchainName(cfg *config.ConfigFile, flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if cfg.Global != nil && cfg.Global.Toolchain != "" {
		return cfg.Global.Toolchain
	}
	return toolchain.GetManager().GetDefaultToolchain()
}

func GetToolchain(cfg *config.ConfigFile) (*toolchain.Toolchain, string, error) {
	mgr := toolchain.GetManager()
	tcName := resolveToolchainName(cfg, toolchainFlag)
	tc, err := mgr.SelectToolchain(tcName)
	if err != nil {
		return nil, "", err
	}
	return tc, tcName, nil
}

func GetPackageDirs(graph *resolver.Graph) map[string]string {
	dirs := make(map[string]string)
	for name, node := range graph.Packages {
		if node.IsLocal() && node.Source != nil {
			dirs[name] = node.Source.Dir
		}
	}
	return dirs
}
