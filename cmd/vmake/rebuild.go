package main

import (
	vlog "github.com/spock2300/vmake/pkg/log"

	"github.com/spf13/cobra"
)

var rebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild the project",
	Long:  `Clean and then build the project from scratch.`,
	Run:   runRebuild,
}

func init() {
	RootCmd.AddCommand(rebuildCmd)
	addInstallFlags(rebuildCmd)
	addBuildFlags(rebuildCmd)
}

func runRebuild(cmd *cobra.Command, args []string) {
	ctx := resolveToConfig(false)
	executeCleanLocal(ctx)
	vlog.Info("")
	result, err := runBuildPhase(ctx, BuildOptions{Jobs: jobsFlag, KeepGoing: keepGoingFlag})
	fatalErr(err)
	if installFlag {
		fatalErr(executeInstall(ctx, result))
	}
}

func executeCleanLocal(ctx *RuntimeContext) {
	vlog.Info("")
	vlog.Info("Executing OnClean...")
	if err := executeCleanHooks(ctx, true); err != nil {
		vlog.Error("Skipping OnClean: %v", err)
	}
	if err := cleanPackages(collectCleanEntries(ctx), ctx, false); err != nil {
		vlog.Error("Error: %v", err)
	}
}
