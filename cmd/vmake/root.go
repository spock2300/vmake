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

type BuildContext struct {
	WorkDir       string
	Packages      []plugin.Package
	LoadResults   []plugin.LoadResult
	AllOptions    map[string]map[string]*api.Option
	GlobalOptions map[string]*api.Option
	Config        *config.ConfigFile
	ConfigPath    string
	Requires      []api.RequireInfo
	ResolvedPkgs  map[string]*repo.PackageDef
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

func PrepareBase() (*BuildContext, error) {
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

	configPath := filepath.Join(workDir, ".vmake", "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	return &BuildContext{
		WorkDir:    workDir,
		Packages:   packages,
		Config:     cfg,
		ConfigPath: configPath,
	}, nil
}

func PrepareFull() (*BuildContext, error) {
	ctx, err := PrepareBase()
	if err != nil {
		return nil, err
	}

	vlog.Info("")
	vlog.Info("Compiling...")
	compileResults := plugin.CompileAll(ctx.Packages)

	hasError := false
	for _, cr := range compileResults {
		if cr.Success {
			relPath, _ := filepath.Rel(ctx.WorkDir, cr.PluginPath)
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
		reqCount := len(lr.Loaded.Builder.GetRequireFuncs())
		vlog.Info("  %s: OnConfig(%d), OnBuild(%d), OnRequire(%d)", lr.Package.Name, cfgCount, buildCount, reqCount)
	}

	var allRequires []api.RequireInfo
	for _, lr := range loadResults {
		if !lr.Success {
			continue
		}
		requires := lr.Loaded.GetRequires()
		allRequires = append(allRequires, requires...)
	}

	if len(allRequires) > 0 {
		vlog.Info("")
		vlog.Info("Resolving dependencies...")
		reposDir := filepath.Join(vmakeDir, "repos")
		repoMgr := repo.NewRepoManager(reposDir)
		resolver := repo.NewResolver(repoMgr)

		graph, err := resolver.Resolve(allRequires)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve dependencies: %w", err)
		}

		for _, name := range graph.Order {
			vlog.Info("  %s", name)
		}

		resolvedPkgs := make(map[string]*repo.PackageDef)
		for name := range graph.Packages {
			resolvedPkgs[name] = &repo.PackageDef{
				Name: name,
			}
		}
		ctx.ResolvedPkgs = resolvedPkgs
	}
	ctx.Requires = allRequires

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

	if len(ctx.ResolvedPkgs) > 0 {
		vlog.Info("")
		vlog.Info("Loading package definitions...")
		reposDir := filepath.Join(vmakeDir, "repos")
		loader := repo.NewPackageLoader(filepath.Join(vmakeDir, "cache"))
		loader.SetVMakeDir(getVMakeSourceDir())
		repoMgr := repo.NewRepoManager(reposDir)

		for fullName := range ctx.ResolvedPkgs {
			parts := strings.Split(fullName, "/")
			if len(parts) != 2 {
				continue
			}
			repoName, pkgName := parts[0], parts[1]

			pkgGoPath, err := repoMgr.FindPackageGo(repoName, pkgName)
			if err != nil {
				vlog.Error("  %s: %v", fullName, err)
				continue
			}

			pkg, err := loader.Load(pkgGoPath)
			if err != nil {
				vlog.Error("  %s: failed to load: %v", fullName, err)
				continue
			}

			opts := pkg.GetOptions()
			if len(opts) > 0 {
				allOptions[fullName] = opts
				vlog.Info("  %s: %d option(s)", fullName, len(opts))
			}
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

	globalOptions, err := api.MergeGlobalOptions(allOptions, tcList)
	if err != nil {
		return nil, fmt.Errorf("global options error: %w", err)
	}

	ctx.LoadResults = loadResults
	ctx.AllOptions = allOptions
	ctx.GlobalOptions = globalOptions
	return ctx, nil
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

func GetPackageDirs(packages []plugin.Package) map[string]string {
	dirs := make(map[string]string)
	for _, pkg := range packages {
		dirs[pkg.Name] = pkg.Dir
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
