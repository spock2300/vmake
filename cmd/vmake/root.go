package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/config"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/plugin"
	"gitee.com/spock2300/vmake/pkg/toolchain"

	"github.com/spf13/cobra"
)

var (
	verbose     bool
	veryVerbose bool
	quiet       bool
)

type BuildContext struct {
	WorkDir     string
	Packages    []plugin.Package
	LoadResults []plugin.LoadResult
	AllOptions  map[string]map[string]*api.Option
	Config      *config.ConfigFile
	ConfigPath  string
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

func init() {
	RootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	RootCmd.PersistentFlags().BoolVarP(&veryVerbose, "very-verbose", "V", false, "very verbose output")
	RootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "quiet mode")
}

func PrepareBuild() (*BuildContext, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	vlog.Info("Scanning %s...", workDir)

	packages, err := plugin.Scan(workDir)
	if err != nil {
		return nil, err
	}

	if len(packages) == 0 {
		return nil, fmt.Errorf("no build.go files found")
	}

	var pkgNames []string
	for _, pkg := range packages {
		pkgNames = append(pkgNames, pkg.Name)
	}
	vlog.Info("Found %d package(s): %s", len(packages), strings.Join(pkgNames, ", "))

	vlog.Info("")
	vlog.Info("Compiling...")
	compileResults := plugin.CompileAll(packages)

	hasError := false
	for _, cr := range compileResults {
		if cr.Success {
			relPath, _ := filepath.Rel(workDir, cr.PluginPath)
			vlog.Info("  %s -> %s", cr.Package.Name, relPath)
		} else {
			vlog.Info("  %s -> FAILED", cr.Package.Name)
			vlog.Error("    %v", cr.Error)
			hasError = true
		}
	}

	if hasError {
		return nil, fmt.Errorf("compilation failed")
	}

	vlog.Info("")
	vlog.Info("Loading plugins...")
	loadResults := plugin.LoadAll(compileResults)

	for _, lr := range loadResults {
		if !lr.Success {
			vlog.Info("  %s -> FAILED: %v", lr.Package.Name, lr.Error)
			continue
		}

		cfgCount := len(lr.Loaded.Builder.GetConfigFuncs())
		buildCount := len(lr.Loaded.Builder.GetBuildFuncs())
		vlog.Info("  %s: OnConfig(%d), OnBuild(%d)", lr.Package.Name, cfgCount, buildCount)
	}

	configPath := filepath.Join(workDir, ".vmake", "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	vlog.Info("")
	vlog.Info("Executing OnConfig...")
	allOptions := make(map[string]map[string]*api.Option)
	for _, lr := range loadResults {
		if !lr.Success {
			continue
		}

		pkgName := lr.Package.Name
		cfgCtx := api.NewConfigContext(pkgName)

		for _, fn := range lr.Loaded.Builder.GetConfigFuncs() {
			fn(cfgCtx)
		}

		allOptions[pkgName] = cfgCtx.GetOptions()
		if len(cfgCtx.GetOptions()) > 0 {
			vlog.Info("  %s: %d option(s)", pkgName, len(cfgCtx.GetOptions()))
		}
	}

	return &BuildContext{
		WorkDir:     workDir,
		Packages:    packages,
		LoadResults: loadResults,
		AllOptions:  allOptions,
		Config:      cfg,
		ConfigPath:  configPath,
	}, nil
}

func GetToolchain(cfg *config.ConfigFile) (*toolchain.Toolchain, string, error) {
	mgr := toolchain.GetManager()
	tcName := cfg.Toolchain
	if tcName == "" {
		tcName = mgr.GetDefaultToolchain()
	}
	tc, err := mgr.SelectToolchain(tcName)
	if err != nil {
		return nil, "", err
	}
	return tc, tcName, nil
}

func GetPackageBuildDirs(packages []plugin.Package) map[string]string {
	pkgBuildDirs := make(map[string]string)
	for _, pkg := range packages {
		pkgBuildDirs[pkg.Name] = filepath.Join(filepath.Dir(pkg.Path), "build")
	}
	return pkgBuildDirs
}

var _ = fmt.Sprintf
