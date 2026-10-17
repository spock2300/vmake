package main

import (
	vlog "github.com/spock2300/vmake/pkg/log"

	"github.com/spf13/cobra"
)

var rebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild the project",
	Long:  `Clean and then build the project from scratch.`,
	RunE:  runRebuild,
}

func init() {
	RootCmd.AddCommand(rebuildCmd)
	addInstallFlags(rebuildCmd)
	addBuildFlags(rebuildCmd)
}

func runRebuild(cmd *cobra.Command, args []string) (err error) {
	commandStorageLocks()
	report, err := beginBuildReport()
	if err != nil {
		return err
	}
	defer report.finish(&err)
	execution := &RuntimeContext{}
	return withResolvedProject(execution, func(ctx *RuntimeContext) error {
		executeCleanLocal(ctx)
		vlog.Info("")
		_, err := runReportedBuild(ctx, report, BuildOptions{IncludeTests: testsFlag, Jobs: jobsFlag, KeepGoing: keepGoingFlag})
		return err
	})
}

func executeCleanLocal(ctx *RuntimeContext) {
	vlog.Info("")
	vlog.Info("Executing OnClean...")
	if err := executeCleanHooks(ctx, true, true); err != nil {
		vlog.Error("Skipping OnClean: %v", err)
	}
	if err := cleanPackages(collectCleanEntries(ctx), ctx, false); err != nil {
		vlog.Error("Error: %v", err)
	}
}
