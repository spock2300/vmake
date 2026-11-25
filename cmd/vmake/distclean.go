package main

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/buildreport"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
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

func runDistClean(cmd *cobra.Command, args []string) {
	commandStorageLocks()
	fatalErr(buildreport.InvalidateReports(findProjectDir()))
	ctx, ok := openProjectBestEffort(projectOptions{softGitignore: true})
	defer closeProject(ctx)
	if ok {
		vlog.Info("")
		vlog.Info("Executing OnClean...")
		if err := pipeline.ExecuteCleanHooks(ctx, false, false, false); err != nil {
			vlog.Error("Skipping OnClean: %v", err)
		}
	}

	entries := scanPackages(ctx.WorkDir)
	if err := pipeline.CleanPackages(entries, ctx, true); err != nil {
		vlog.Error("Error: %v", err)
	}

	pipeline.RemoveIfExists(filepath.Join(ctx.Paths.ProjectDir, "build", "compile_commands.json"), "", "compile_commands.json", false)

	pipeline.RemoveIfExists(filepath.Join(ctx.Paths.ProjectDir, "install"), "", "install/", true)

	if distCleanPurgeSources {
		pipeline.RemoveIfExists(getDepsDir(), "", ".vmake_deps/", true)
	} else if fs.FileExists(getDepsDir()) {
		vlog.Info("Kept downloaded sources under .vmake_deps/ (use --purge-sources to remove them)")
	}

	vlog.Info("Distclean completed!")
}
