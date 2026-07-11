package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
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
	RootCmd.AddCommand(newCheckSymbolsCmd())
}

type packageGlobalFlags struct {
	cFlags   []string
	cxxFlags []string
	ldFlags  []string
	links    []string
}

type RuntimeContext struct {
	WorkDir             string
	Config              *config.ConfigFile
	ConfigPath          string
	DepGraph            *resolver.Graph
	AllOptions          map[string]map[string]*api.Option
	AllKConfigs         map[string][]*api.KConfigEntry
	GlobalOptions       map[string]*api.Option
	Resolver            *resolver.Resolver
	BufferedGlobalFlags map[string]*packageGlobalFlags
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

func runConfigurePhase(ctx *RuntimeContext) {
	fatalErr(ctx.Resolver.ResolveDeferred())
	fatalErr(ctx.Resolver.UpdateOrder())
	fatalErr(runConfigPhase(ctx))
}

func resolveToConfig() *RuntimeContext {
	ctx := mustInitContext()
	ensureGitignore(findProjectDir())
	fatalErr(runRequirePhase(ctx))
	runConfigurePhase(ctx)
	return ctx
}

func resolveToConfigBestEffort() (*RuntimeContext, bool) {
	ctx := mustInitContext()
	ensureGitignore(findProjectDir())
	if err := runRequirePhase(ctx); err != nil {
		return ctx, false
	}
	runConfigurePhase(ctx)
	return ctx, true
}

func resolveWithDefault(flagVal, configVal, defaultVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if configVal != "" {
		return configVal
	}
	return defaultVal
}

func resolveMode(cfg *config.ConfigFile, flagValue string) string {
	var configMode string
	if cfg.Global != nil {
		configMode = cfg.Global.Mode
	}
	return resolveWithDefault(flagValue, configMode, api.ModeDebug)
}

func resolveToolchainName(cfg *config.ConfigFile, flagValue string) string {
	var configTC string
	if cfg.Global != nil {
		configTC = cfg.Global.Toolchain
	}
	return resolveWithDefault(flagValue, configTC, toolchain.GetManager().GetDefaultToolchain())
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

func runRequirePhase(ctx *RuntimeContext) error {
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

	r := resolver.NewResolver(getRepoManager(), getDepsDir())
	r.SetGlobalSourcesDir(getSourcesDir())
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

func collectOptions(name, dir string, pkg *api.Package, buf *packageGlobalFlags) map[string]*api.Option {
	cfgCtx := api.NewConfigContextWithPackage(name, pkg)
	cfgCtx.SetGlobalCFlagsFunc(func(flags ...string) {
		buf.cFlags = append(buf.cFlags, flags...)
	})
	cfgCtx.SetGlobalCxxFlagsFunc(func(flags ...string) {
		buf.cxxFlags = append(buf.cxxFlags, flags...)
	})
	cfgCtx.SetGlobalLdFlagsFunc(func(flags ...string) {
		buf.ldFlags = append(buf.ldFlags, flags...)
	})
	cfgCtx.SetGlobalLinksFunc(func(links ...string) {
		buf.links = append(buf.links, links...)
	})
	pkg.ExecConfigFuncs(dir, func(fn api.ConfigFunc) { fn(cfgCtx) })
	return cfgCtx.GetOptions()
}

func collectAllOptionsAndKConfigs(ctx *RuntimeContext) {
	ctx.AllOptions = make(map[string]map[string]*api.Option)
	ctx.AllKConfigs = make(map[string][]*api.KConfigEntry)
	ctx.BufferedGlobalFlags = make(map[string]*packageGlobalFlags)
	pkgDirs := ResolveAllPackageDirs(ctx.DepGraph)

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]

		var opts map[string]*api.Option
		if node.Pkg != nil {
			buf := &packageGlobalFlags{}
			ctx.BufferedGlobalFlags[name] = buf
			opts = collectOptions(name, pkgDirs[name].SourceDir, node.Pkg, buf)
		}

		if len(opts) > 0 {
			ctx.AllOptions[name] = opts
			vlog.Info("  %s: %d option(s)", name, len(opts))
		}

		if node.Pkg != nil {
			entries := node.Pkg.KConfigEntries()
			if len(entries) > 0 {
				for _, e := range entries {
					if e.ConfigPath() == "" {
						e.SetConfigPath(".config")
					}
					if e.SrcDir() == "" {
						e.SetSrcDir(pkgDirs[name].SourceDir)
					} else if !filepath.IsAbs(e.SrcDir()) {
						e.SetSrcDir(filepath.Join(pkgDirs[name].SourceDir, e.SrcDir()))
					}
					cfgEntry := config.GetEntry(ctx.Config, name)
					if cfgEntry.SelectedPreset != "" {
						e.SetSelectedPreset(cfgEntry.SelectedPreset)
					} else if e.DefaultPreset() != "" {
						e.SetSelectedPreset(e.DefaultPreset())
					}
				}
				ctx.AllKConfigs[name] = entries
				vlog.Info("  %s: %d kconfig(s)", name, len(entries))
			}
		}
	}
}

func applyAllConfigCallbacks(ctx *RuntimeContext) {
	vlog.Info("")
	vlog.Info("Applying configuration...")
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.Pkg == nil {
			continue
		}
		opts := ctx.AllOptions[name]
		if len(opts) == 0 {
			continue
		}

		buf := ctx.BufferedGlobalFlags[name]
		if buf == nil {
			buf = &packageGlobalFlags{}
			ctx.BufferedGlobalFlags[name] = buf
		}

		entry := config.GetEntry(ctx.Config, name)
		applyCtx := api.NewConfigContextWithPackage(name, node.Pkg)
		applyCtx.SetGlobalCFlagsFunc(func(flags ...string) {
			buf.cFlags = append(buf.cFlags, flags...)
		})
		applyCtx.SetGlobalCxxFlagsFunc(func(flags ...string) {
			buf.cxxFlags = append(buf.cxxFlags, flags...)
		})
		applyCtx.SetGlobalLdFlagsFunc(func(flags ...string) {
			buf.ldFlags = append(buf.ldFlags, flags...)
		})
		applyCtx.SetGlobalLinksFunc(func(links ...string) {
			buf.links = append(buf.links, links...)
		})

		for optName, opt := range opts {
			if opt.OnApply() == nil {
				continue
			}
			val, ok := entry.Options[optName]
			if !ok || val == nil {
				val = opt.Default()
			}
			vlog.Debug("  %s/%s = %v", name, optName, val)
			opt.OnApply()(applyCtx, val)
		}
	}
}

func applyGlobalFlagsFromNeeded(ctx *RuntimeContext, needed map[string]bool) {
	mgr := toolchain.GetManager()
	for _, name := range ctx.Resolver.GetOrder() {
		if !needed[name] {
			continue
		}
		buf := ctx.BufferedGlobalFlags[name]
		if buf == nil {
			continue
		}
		if len(buf.cFlags) > 0 {
			mgr.AddGlobalCFlags(buf.cFlags...)
		}
		if len(buf.cxxFlags) > 0 {
			mgr.AddGlobalCxxFlags(buf.cxxFlags...)
		}
		if len(buf.ldFlags) > 0 {
			mgr.AddGlobalLdFlags(buf.ldFlags...)
		}
		if len(buf.links) > 0 {
			mgr.AddGlobalLinks(buf.links...)
		}
	}
}

func buildToolchainAndGlobalOptions(ctx *RuntimeContext) error {
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

func runConfigPhase(ctx *RuntimeContext) error {
	vlog.Info("")
	vlog.Info("Executing OnConfig...")

	collectAllOptionsAndKConfigs(ctx)
	applyAllConfigCallbacks(ctx)
	return buildToolchainAndGlobalOptions(ctx)
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

func ResolveAllPackageDirs(graph *resolver.Graph) map[string]*api.PkgDirs {
	dirs := make(map[string]*api.PkgDirs)
	for name, node := range graph.Packages {
		if node.Source == nil {
			continue
		}
		dirs[name] = &api.PkgDirs{SourceDir: node.Source.Dir}
	}
	return dirs
}

func ensureGitignore(workDir string) {
	gitignorePath := filepath.Join(workDir, ".gitignore")
	content := ""
	if data, err := os.ReadFile(gitignorePath); err == nil {
		content = string(data)
	}
	if strings.Contains(content, "vmake_deps") {
		return
	}
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	var buf []byte
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		buf = append(buf, '\n')
	}
	buf = append(buf, []byte("vmake_deps/\n")...)
	f.Write(buf)
}
