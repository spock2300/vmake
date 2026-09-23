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

func runRebuild(cmd *cobra.Command, args []string) error {
	commandStorageLocks()
	execution := &RuntimeContext{}
	return withBuildContext(execution, func() error {
		ctx, err := resolveToConfigContext(execution.Context, false)
		if err != nil {
			return err
		}
		executeCleanLocal(ctx)
		vlog.Info("")
		result, err := runBuildPhase(ctx, BuildOptions{Jobs: jobsFlag, KeepGoing: keepGoingFlag})
		if err != nil {
			return err
		}
		if installFlag {
			return executeInstall(ctx, result)
		}
		return nil
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
