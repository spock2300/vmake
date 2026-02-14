package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	vlog "gitee.com/spock2300/vmake/pkg/log"

	"github.com/spf13/cobra"
)

var cleanAll bool

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean build artifacts",
	Long: `Remove object files and build cache for all packages.
Use --all to clean build directories for all toolchains.`,
	Run: runClean,
}

func init() {
	cleanCmd.Flags().BoolVar(&cleanAll, "all", false, "clean build directories for all toolchains")
	RootCmd.AddCommand(cleanCmd)
}

func runClean(cmd *cobra.Command, args []string) {
	ctx, err := PrepareBuild()
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}

	origDir, _ := os.Getwd()

	_, tcName, err := GetToolchain(ctx.Config)
	if err != nil {
		vlog.Error("Toolchain error: %v", err)
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

	for _, pkg := range ctx.Packages {
		if err := os.Chdir(pkg.Dir); err != nil {
			vlog.Error("Failed to chdir to %s: %v", pkg.Name, err)
			continue
		}

		if cleanAll {
			cleanAllToolchains(pkg.Name)
		} else {
			cleanCurrentToolchain(pkg.Name, buildDir)
		}
	}

	os.Chdir(origDir)
	vlog.Info("Clean completed!")
}

func cleanCurrentToolchain(pkgName, buildDir string) {
	tcDir := fmt.Sprintf("build/%s", buildDir)
	if _, err := os.Stat(tcDir); err == nil {
		if err := os.RemoveAll(tcDir); err != nil {
			vlog.Error("Failed to clean %s/%s: %v", pkgName, tcDir, err)
			return
		}
		vlog.Info("Cleaned %s/%s/", pkgName, tcDir)
	}
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

		tcDir := filepath.Join("build", name)
		if err := os.RemoveAll(tcDir); err != nil {
			vlog.Error("Failed to clean %s/%s: %v", pkgName, tcDir, err)
			continue
		}
		vlog.Info("Cleaned %s/%s/", pkgName, tcDir)
	}
}
