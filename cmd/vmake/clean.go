package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/pkg/buildscript"
	vlog "gitee.com/spock2300/vmake/pkg/log"

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
	ctx := mustInitContext()

	packages, err := buildscript.Scan(ctx.WorkDir)
	fatalErr(err)

	_, tcName, err := GetToolchain(ctx.Config)
	fatalErr(err)

	mode := resolveMode(ctx.Config)

	buildDir := fmt.Sprintf("%s-%s", tcName, mode)

	for _, pkg := range packages {
		if cleanAllFlag {
			cleanAllToolchains(pkg.Dir, pkg.Name)
		} else {
			cleanCurrentToolchain(pkg.Dir, pkg.Name, buildDir)
		}
	}

	vlog.Info("Clean completed!")
}

func cleanAllToolchains(dir, pkgName string) {
	buildBase := filepath.Join(dir, "build")
	entries, err := os.ReadDir(buildBase)
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

		tcDir := filepath.Join(buildBase, name)
		if err := os.RemoveAll(tcDir); err != nil {
			vlog.Error("Failed to clean %s/%s: %v", pkgName, name, err)
			continue
		}
		vlog.Info("Cleaned %s/%s/", pkgName, name)
	}
}
