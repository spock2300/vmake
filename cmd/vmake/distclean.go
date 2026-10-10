package main

import (
	"path/filepath"

	"github.com/spf13/cobra"

	vlog "github.com/spock2300/vmake/pkg/log"
)

var distCleanCmd = &cobra.Command{
	Use:   "distclean",
	Short: "Deep clean all build artifacts",
	Long: `Remove all build outputs, the project source trees under .vmake_deps/,
the install directory, and the build report.

This is equivalent to 'vmake clean --all' plus:
  - .vmake_deps/ (the single per-package working trees)
  - build/compile_commands.json
  - install/ directory at project root

Sources are shallow clones re-downloaded on the next build, so distclean
removes the only local copy. Use 'vmake pkg clean <repo/name>' to purge
individual packages.`,
	Run: runDistClean,
}

func init() {
	RootCmd.AddCommand(distCleanCmd)
}

func runDistClean(cmd *cobra.Command, args []string) {
	commandStorageLocks()
	fatalErr(invalidateBuildReport())
	ctx, ok := resolveToConfigBestEffort(false)
	if ok {
		vlog.Info("")
		vlog.Info("Executing OnClean...")
		if err := executeCleanHooks(ctx, false, false); err != nil {
			vlog.Error("Skipping OnClean: %v", err)
		}
	}

	entries := scanPackages(ctx.WorkDir)
	if err := cleanPackages(entries, ctx, true); err != nil {
		vlog.Error("Error: %v", err)
	}

	removeIfExists(filepath.Join(ctx.Paths.ProjectDir, "build", "compile_commands.json"), "", "compile_commands.json", false)

	removeIfExists(filepath.Join(ctx.Paths.ProjectDir, "install"), "", "install/", true)

	removeIfExists(getDepsDir(), "", ".vmake_deps/", true)

	vlog.Info("Distclean completed!")
}
