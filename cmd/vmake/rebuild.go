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
	runClean(cmd, args)

	vlog.Info("")

	runBuild(cmd, args)
}
