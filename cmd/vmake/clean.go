package main

import (
	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/buildreport"
	"github.com/spock2300/vmake/pkg/pipeline"
)

var cleanAllFlag bool

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean build artifacts",
	Long: `Remove build outputs for the current configuration.
Use --all to remove outputs for every configuration. Downloaded source trees
under .vmake_deps/ are kept; use 'vmake distclean --purge-sources' to remove
them too.`,
	Args: cobra.NoArgs,
	Run:  runClean,
}

type pkgCleanEntry = pipeline.PkgCleanEntry

func runClean(cmd *cobra.Command, args []string) {
	commandStorageLocks()
	ctx, ok := openProjectBestEffort(projectOptions{softGitignore: true})
	if ok {
		defer closeProject(ctx)
	}
	if ok || cleanAllFlag {
		fatalErr(buildreport.InvalidateReports(findProjectDir()))
	}
	fatalErr(pipeline.RunClean(ctx, ok, cleanAllFlag))
}
