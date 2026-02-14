package main

import (
	vlog "gitee.com/spock2300/vmake/pkg/log"

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
}

func runRebuild(cmd *cobra.Command, args []string) {
	ctx, err := PrepareFull()
	if err != nil {
		vlog.Error("Error: %v", err)
		return
	}

	if err := executeClean(ctx, false); err != nil {
		vlog.Error("Error: %v", err)
		return
	}

	vlog.Info("")

	if err := executeBuild(ctx); err != nil {
		vlog.Error("Error: %v", err)
		return
	}
}
