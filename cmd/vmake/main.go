package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/plugin"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "config" {
		fmt.Println("config command not implemented yet")
		os.Exit(1)
	}

	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get working directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Scanning %s...\n", workDir)

	packages, err := plugin.Scan(workDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scan failed: %v\n", err)
		os.Exit(1)
	}

	if len(packages) == 0 {
		fmt.Println("No build.go files found")
		os.Exit(0)
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
		os.Exit(1)
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

	fmt.Println("\nExecuting OnBuild...")
	allTargets := make(map[string]map[string]*api.Target)

	for _, lr := range loadResults {
		if !lr.Success {
			continue
		}

		pkgName := lr.Package.Name
		ctx := api.NewBuildContext(pkgName, nil)

		for _, fn := range lr.Loaded.Builder.GetBuildFuncs() {
			fn(ctx)
		}

		allTargets[pkgName] = ctx.GetTargets()
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
