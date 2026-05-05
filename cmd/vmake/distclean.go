package main

import (
	"path/filepath"

	"github.com/spf13/cobra"

	vlog "gitee.com/spock2300/vmake/pkg/log"
)

var distCleanCmd = &cobra.Command{
	Use:   "distclean",
	Short: "Deep clean all build artifacts",
	Long: `Remove all build artifacts including compiled buildscripts,
remote package cache, and the install directory.

This is equivalent to 'vmake clean --all' plus:
  - build/build.so for each local package
  - build/compile_commands.json for each local package
  - go.mod/go.sum for each local package (stale buildscript cache)
  - install/ directory at project root
  - vmake_deps/ directory (all third-party sources and build cache)`,
	Run: runDistClean,
}

func init() {
	RootCmd.AddCommand(distCleanCmd)
}

func runDistClean(cmd *cobra.Command, args []string) {
	ctx, ok := resolveToConfigBestEffort(false)
	if ok {
		vlog.Info("")
		vlog.Info("Executing OnClean...")
		executeCleanHooks(ctx, false)
	}

	entries := scanPackages(ctx.WorkDir)
	cleanPackages(entries, ctx.Config, true)

	for _, pkg := range entries {
		removeIfExists(filepath.Join(pkg.Dir, "build", "build.so"), pkg.Name, "build.so", false)
		removeIfExists(filepath.Join(pkg.Dir, "build", "compile_commands.json"), pkg.Name, "compile_commands.json", false)
		removeIfExists(filepath.Join(pkg.Dir, "go.mod"), pkg.Name, "go.mod", false)
		removeIfExists(filepath.Join(pkg.Dir, "go.sum"), pkg.Name, "go.sum", false)
	}

	removeIfExists(filepath.Join(ctx.WorkDir, "install"), "", "install/", true)
	removeIfExists(getDepsDir(), "", "vmake_deps/", true)

	vlog.Info("Distclean completed!")
}
