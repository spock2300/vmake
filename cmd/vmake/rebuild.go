package main

import (
	"fmt"
	"os"

	"gitee.com/spock2300/vmake/pkg/api"
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
	rebuildCmd.Flags().BoolVarP(&installFlag, "install", "i", false, "install after build")
	rebuildCmd.Flags().StringVarP(&prefixFlag, "prefix", "p", "", "installation prefix (default: ./install)")
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
	origDir, _ := os.Getwd()

	_, tcName, err := GetToolchain(ctx.Config)
	if err != nil {
		vlog.Error("Error: %v", err)
		return
	}

	mode := ""
	if ctx.Config.Global != nil {
		mode = ctx.Config.Global.Mode
	}
	if mode == "" {
		mode = api.ModeDebug
	}

	buildDir := fmt.Sprintf("%s-%s", tcName, mode)

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if !node.IsLocal() {
			continue
		}

		src := node.Source
		if err := os.Chdir(src.Dir); err != nil {
			vlog.Error("Failed to chdir to %s: %v", src.Name, err)
			continue
		}

		cleanCurrentToolchain(src.Name, buildDir)
	}

	os.Chdir(origDir)
	vlog.Info("Clean completed!")
}

func cleanCurrentToolchain(pkgName, buildDir string) {
	tcDir := fmt.Sprintf("build/%s", buildDir)
	if _, err := os.Stat(tcDir); err == nil {
		if err := os.RemoveAll(tcDir); err != nil {
			vlog.Error("Failed to clean %s/%s: %v", pkgName, tcDir, err)
			return
		}
		vlog.Info("Cleaned %s/%s/", pkgName, tcDir)
	}
}
