package main

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/fs"
	vlog "github.com/spock2300/vmake/pkg/log"
)

var distCleanPurgeSources bool

var distCleanCmd = &cobra.Command{
	Use:   "distclean",
	Short: "Deep clean all build artifacts",
	Long: `Remove all build outputs, the install directory, and the build report.

This is equivalent to 'vmake clean --all' plus:
  - build/compile_commands.json
  - install/ directory at project root

Downloaded package sources under .vmake_deps/ are kept so the next build
reuses the existing working trees without re-downloading. Pass
--purge-sources to remove them too; use 'vmake pkg clean <repo/name> -a'
to purge individual packages.`,
	Run: runDistClean,
}

func init() {
	RootCmd.AddCommand(distCleanCmd)
	distCleanCmd.Flags().BoolVar(&distCleanPurgeSources, "purge-sources", false, "also remove downloaded package sources under .vmake_deps")
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

	if distCleanPurgeSources {
		removeIfExists(getDepsDir(), "", ".vmake_deps/", true)
	} else if fs.FileExists(getDepsDir()) {
		vlog.Info("Kept downloaded sources under .vmake_deps/ (use --purge-sources to remove them)")
	}

	vlog.Info("Distclean completed!")
}
