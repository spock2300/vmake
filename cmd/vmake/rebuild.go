package main

import (
	"fmt"

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
	addInstallFlags(rebuildCmd)
}

func runRebuild(cmd *cobra.Command, args []string) {
	runPipeline(pipelineOptions{
		force: false,
		afterPhase1: func(ctx *RuntimeContext) {
			executeCleanLocal(ctx)
			vlog.Info("")
		},
		installAfter: installFlag,
	})
}

func executeCleanLocal(ctx *RuntimeContext) {
	_, tcName, err := GetToolchain(ctx.Config)
	if err != nil {
		vlog.Error("Error: %v", err)
		return
	}

	mode := resolveMode(ctx.Config)

	buildDir := fmt.Sprintf("%s-%s", tcName, mode)

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() {
			continue
		}

		cleanCurrentToolchain(node.Source.Dir, name, buildDir)
		cleanPkgToolchain(node.Source.Dir, name, ctx.Config, tcName, mode)
	}

	vlog.Info("Clean completed!")
}
