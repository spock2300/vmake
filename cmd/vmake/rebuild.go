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
}

func runRebuild(cmd *cobra.Command, args []string) {
	ctx := resolveToConfig(false)
	executeCleanLocal(ctx)
	vlog.Info("")
	result, err := runBuildPhase(ctx, false)
	fatalErr(err)
	if installFlag {
		fatalErr(executeInstall(ctx, result))
	}
}

func executeCleanLocal(ctx *RuntimeContext) {
	vlog.Info("")
	vlog.Info("Executing OnClean...")
	executeCleanHooks(ctx, true)
	cleanPackages(collectCleanEntries(ctx), ctx.Config, false)
}
