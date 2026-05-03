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
	addInstallFlags(rebuildCmd)
}

func runRebuild(cmd *cobra.Command, args []string) {
	runPipeline(pipelineOptions{
		force: false,
		beforeBuild: func(ctx *RuntimeContext) {
			executeCleanLocal(ctx)
			vlog.Info("")
		},
		installAfter: installFlag,
	})
}

func executeCleanLocal(ctx *RuntimeContext) {
	vlog.Info("")
	vlog.Info("Executing OnClean...")
	executeCleanHooks(ctx, true)

	var entries []pkgCleanEntry
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.IsLocal() {
			entries = append(entries, pkgCleanEntry{Dir: node.Source.Dir, Name: name})
		}
	}
	cleanPackages(entries, ctx.Config, false)
}
