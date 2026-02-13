package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/config"
	"gitee.com/spock2300/vmake/pkg/plugin"
	"gitee.com/spock2300/vmake/pkg/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "config" {
		runConfig()
		return
	}
	runBuild()
}

func runConfig() {
	workDir, packages, _, allOptions, cfg, configPath, err := prepareBuild()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(allOptions) == 0 {
		fmt.Println("No configuration options found")
		return
	}

	values := make(map[string]map[string]any)
	for pkgName := range allOptions {
		pc := config.GetPackageConfig(cfg, pkgName)
		values[pkgName] = pc.Options
	}

	result, err := tui.Run(packages, allOptions, values, workDir)
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

	if err := config.Save(configPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Configuration saved to %s\n", configPath)
	_ = workDir
}

func runBuild() {
	workDir, _, loadResults, allOptions, cfg, _, err := prepareBuild()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	_ = workDir

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
