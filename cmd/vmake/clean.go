package main

import (
	"fmt"
	"os"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/plugin"

	"github.com/spf13/cobra"
)

var cleanAllFlag bool

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean build artifacts",
	Long: `Remove object files and build cache for all packages.
Use --all to clean build directories for all toolchains.`,
	Run: runClean,
}

func init() {
	RootCmd.AddCommand(cleanCmd)
	cleanCmd.Flags().BoolVar(&cleanAllFlag, "all", false, "clean build directories for all toolchains")
}

func runClean(cmd *cobra.Command, args []string) {
	ctx, err := initContext()
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}

	packages, err := plugin.Scan(ctx.WorkDir)
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}

	_, tcName, err := GetToolchain(ctx.Config)
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}

	mode := ""
	if ctx.Config.Global != nil {
		mode = ctx.Config.Global.Mode
	}
	if mode == "" {
		mode = api.ModeDebug
	}

	buildDir := fmt.Sprintf("%s-%s", tcName, mode)

	origDir, _ := os.Getwd()
	for _, pkg := range packages {
		if err := os.Chdir(pkg.Dir); err != nil {
			vlog.Error("Failed to chdir to %s: %v", pkg.Name, err)
			continue
		}

		if cleanAllFlag {
			cleanAllToolchains(pkg.Name)
		} else {
			cleanCurrentToolchain(pkg.Name, buildDir)
		}
	}

	os.Chdir(origDir)
	vlog.Info("Clean completed!")
}

func cleanAllToolchains(pkgName string) {
	entries, err := os.ReadDir("build")
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		tcDir := fmt.Sprintf("build/%s", name)
		if err := os.RemoveAll(tcDir); err != nil {
			vlog.Error("Failed to clean %s/%s: %v", pkgName, tcDir, err)
			continue
		}
		vlog.Info("Cleaned %s/%s/", pkgName, tcDir)
	}
}
