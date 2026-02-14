package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/build"
	"gitee.com/spock2300/vmake/pkg/config"
	"gitee.com/spock2300/vmake/pkg/plugin"
	"gitee.com/spock2300/vmake/pkg/toolchain"
	"gitee.com/spock2300/vmake/pkg/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) < 2 {
		runBuild()
		return
	}

	cmd := os.Args[1]
	switch cmd {
	case "config":
		runConfig()
	case "build":
		runBuild()
	case "clean":
		runClean()
	case "rebuild":
		runRebuild()
	case "toolchain":
		runToolchain(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		fmt.Println("Usage: vmake [build|config|clean|rebuild|toolchain]")
		os.Exit(1)
	}
}

func runToolchain(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: vmake toolchain <init|list|show>")
		os.Exit(1)
	}

	subCmd := args[0]
	switch subCmd {
	case "init":
		runToolchainInit()
	case "list":
		runToolchainList()
	case "show":
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		runToolchainShow(name)
	default:
		fmt.Fprintf(os.Stderr, "Unknown toolchain command: %s\n", subCmd)
		os.Exit(1)
	}
}

func runToolchainInit() {
	path := toolchain.GetGlobalConfigPath()

	if toolchain.ExistsGlobalConfig() {
		fmt.Printf("Config already exists: %s\n", path)
		fmt.Println("Delete it first if you want to regenerate")
		os.Exit(1)
	}

	tmpl := toolchain.GetDefaultTemplate()
	if err := toolchain.SaveGlobal(tmpl); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Created %s\n", path)
	fmt.Println("Please edit the file to configure your toolchains")
}

func runToolchainList() {
	mgr := toolchain.GetManager()
	toolchains, err := mgr.ListToolchains()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load toolchains: %v\n", err)
		os.Exit(1)
	}

	defaultTC := mgr.GetDefaultToolchain()
	fmt.Println("Available toolchains:")
	for name, tc := range toolchains {
		mark := ""
		if name == defaultTC {
			mark = " (default)"
		}
		fmt.Printf("  %s%s\n", name, mark)
		fmt.Printf("    Display: %s\n", tc.DisplayName)
		fmt.Printf("    Host:    %s\n", tc.Host)
		fmt.Printf("    CC:      %s\n", tc.Tools.CC)
		fmt.Printf("    CXX:     %s\n", tc.Tools.CXX)
	}
}

func runToolchainShow(name string) {
	mgr := toolchain.GetManager()

	if name == "" {
		name = mgr.GetDefaultToolchain()
	}

	tc, err := mgr.GetToolchain(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Toolchain: %s\n", tc.Name)
	fmt.Printf("Display Name: %s\n", tc.DisplayName)
	fmt.Printf("Host: %s\n", tc.Host)
	fmt.Println()
	fmt.Println("Tools:")
	fmt.Printf("  CC:     %s\n", tc.Tools.CC)
	fmt.Printf("  CXX:    %s\n", tc.Tools.CXX)
	fmt.Printf("  AR:     %s\n", tc.Tools.AR)
	fmt.Printf("  LD:     %s\n", tc.Tools.LD)
	fmt.Printf("  STRIP:  %s\n", tc.Tools.STRIP)
	fmt.Printf("  RANLIB: %s\n", tc.Tools.RANLIB)
	fmt.Println()
	fmt.Println("Default Flags:")
	fmt.Printf("  CFlags:   [%s]\n", strings.Join(tc.DefaultFlags.CFlags, ", "))
	fmt.Printf("  CxxFlags: [%s]\n", strings.Join(tc.DefaultFlags.CxxFlags, ", "))
	fmt.Printf("  LdFlags:  [%s]\n", strings.Join(tc.DefaultFlags.LdFlags, ", "))
	fmt.Println()
	fmt.Println("Download URL:", tc.DownloadURL)
	fmt.Println("Install Path:", tc.InstallPath)

	fmt.Println()
	fmt.Println("Validation:")
	errs := toolchain.ValidateToolchain(tc)
	if len(errs) == 0 {
		fmt.Println("  All tools found")
	} else {
		for _, err := range errs {
			fmt.Printf("  ERROR: %s\n", err)
		}
	}
}

func runConfig() {
	workDir, packages, _, allOptions, cfg, configPath, err := prepareBuild()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(allOptions) == 0 && len(packages) > 0 {
		mgr := toolchain.GetManager()
		if tcs, err := mgr.ListToolchains(); err != nil || len(tcs) == 0 {
			fmt.Println("No configuration options found")
			return
		}
	}

	values := make(map[string]map[string]any)
	for pkgName := range allOptions {
		pc := config.GetPackageConfig(cfg, pkgName)
		values[pkgName] = pc.Options
	}

	currentTC := cfg.Toolchain
	if currentTC == "" {
		currentTC = toolchain.GetManager().GetDefaultToolchain()
	}

	result, err := tui.Run(packages, allOptions, values, workDir, currentTC)
	if err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}

	if !result.Saved {
		fmt.Println("Configuration cancelled")
		return
	}

	for pkgName, opts := range result.Values {
		if cfg.Packages[pkgName] == nil {
			cfg.Packages[pkgName] = &config.PackageConfig{Options: make(map[string]any)}
		}
		cfg.Packages[pkgName].Options = opts
	}

	cfg.Toolchain = result.Toolchain

	if err := config.Save(configPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Configuration saved to %s\n", configPath)
	_ = workDir
}

func runBuild() {
	workDir, packages, loadResults, allOptions, cfg, _, err := prepareBuild()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nExecuting OnBuild...")
	allTargets := make(map[string]map[string]*api.Target)

	for _, lr := range loadResults {
		if !lr.Success {
			continue
		}

		pkgName := lr.Package.Name
		pc := config.GetPackageConfig(cfg, pkgName)
		buildCtx := api.NewBuildContext(pkgName, pc.Options)
		buildCtx.SetOptions(allOptions[pkgName])

		for _, fn := range lr.Loaded.Builder.GetBuildFuncs() {
			fn(buildCtx)
		}

		allTargets[pkgName] = buildCtx.GetTargets()
	}

	fmt.Println("\nTargets found:")
	for pkgName, targets := range allTargets {
		for _, t := range targets {
			defaultMark := ""
			if !t.IsDefault() {
				defaultMark = " [disabled]"
			}
			fmt.Printf("  - %s:%s (%s)%s\n", pkgName, t.Name(), t.Kind(), defaultMark)
		}
	}

	mgr := toolchain.GetManager()
	tcName := cfg.Toolchain
	if tcName == "" {
		tcName = mgr.GetDefaultToolchain()
	}
	tc, err := mgr.SelectToolchain(tcName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Toolchain error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nUsing toolchain: %s\n", tcName)

	graph, err := build.NewBuildGraph(allTargets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Dependency error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nBuild order:")
	for _, fullName := range graph.Order {
		fmt.Printf("  - %s\n", fullName)
	}

	pkgBuildDirs := make(map[string]string)
	for _, pkg := range packages {
		pkgBuildDirs[pkg.Name] = filepath.Join(filepath.Dir(pkg.Path), "build")
	}

	scheduler, err := build.NewScheduler(graph, tc, pkgBuildDirs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Scheduler error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nBuilding...")
	if err := scheduler.BuildAll(); err != nil {
		fmt.Fprintf(os.Stderr, "Build failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nBuild succeeded!")
	_ = workDir
}

func runClean() {
	workDir, packages, _, _, _, _, err := prepareBuild()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for _, pkg := range packages {
		buildDir := filepath.Join(filepath.Dir(pkg.Path), "build")
		objectsDir := filepath.Join(buildDir, "objects")

		if _, err := os.Stat(objectsDir); err == nil {
			if err := os.RemoveAll(objectsDir); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to clean %s: %v\n", pkg.Name, err)
				continue
			}
			fmt.Printf("Cleaned %s/objects/\n", pkg.Name)
		}

		cachePath := filepath.Join(buildDir, "cache.json")
		os.Remove(cachePath)
	}

	fmt.Println("Clean completed!")
	_ = workDir
}

func runRebuild() {
	runClean()
	runBuild()
}

func prepareBuild() (string, []plugin.Package, []plugin.LoadResult, map[string]map[string]*api.Option, *config.ConfigFile, string, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return "", nil, nil, nil, nil, "", err
	}

	fmt.Printf("Scanning %s...\n", workDir)

	packages, err := plugin.Scan(workDir)
	if err != nil {
		return "", nil, nil, nil, nil, "", err
	}

	if len(packages) == 0 {
		return "", nil, nil, nil, nil, "", fmt.Errorf("no build.go files found")
	}

	fmt.Printf("Found %d package(s): ", len(packages))
	for i, pkg := range packages {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Print(pkg.Name)
	}
	fmt.Println()

	fmt.Println("\nCompiling...")
	compileResults := plugin.CompileAll(packages)

	hasError := false
	for _, cr := range compileResults {
		if cr.Success {
			relPath, _ := filepath.Rel(workDir, cr.PluginPath)
			fmt.Printf("  %s -> %s\n", cr.Package.Name, relPath)
		} else {
			fmt.Printf("  %s -> FAILED\n", cr.Package.Name)
			fmt.Fprintf(os.Stderr, "    %v\n", cr.Error)
			hasError = true
		}
	}

	if hasError {
		return "", nil, nil, nil, nil, "", fmt.Errorf("compilation failed")
	}

	fmt.Println("\nLoading plugins...")
	loadResults := plugin.LoadAll(compileResults)

	for _, lr := range loadResults {
		if !lr.Success {
			fmt.Printf("  %s -> FAILED: %v\n", lr.Package.Name, lr.Error)
			continue
		}

		cfgCount := len(lr.Loaded.Builder.GetConfigFuncs())
		buildCount := len(lr.Loaded.Builder.GetBuildFuncs())
		fmt.Printf("  %s: OnConfig(%d), OnBuild(%d)\n", lr.Package.Name, cfgCount, buildCount)
	}

	configPath := filepath.Join(workDir, ".vmake", "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		return "", nil, nil, nil, nil, "", err
	}

	fmt.Println("\nExecuting OnConfig...")
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
			fmt.Printf("  %s: %d option(s)\n", pkgName, len(cfgCtx.GetOptions()))
		}
	}

	return workDir, packages, loadResults, allOptions, cfg, configPath, nil
}

var _ = tea.Quit
