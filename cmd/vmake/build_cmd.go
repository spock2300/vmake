package main

import (
	"os"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/build"
	"gitee.com/spock2300/vmake/pkg/config"
	vlog "gitee.com/spock2300/vmake/pkg/log"

	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the project",
	Long:  `Compile and link all targets defined in build.go files.`,
	Run:   runBuild,
}

func init() {
	RootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) {
	ctx, err := PrepareBuild()
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}

	vlog.Info("")
	vlog.Info("Executing OnBuild...")
	allTargets := make(map[string]map[string]*api.Target)

	for _, lr := range ctx.LoadResults {
		if !lr.Success {
			continue
		}

		pkgName := lr.Package.Name
		pc := config.GetPackageConfig(ctx.Config, pkgName)
		buildCtx := api.NewBuildContext(pkgName, pc.Options)
		buildCtx.SetOptions(ctx.AllOptions[pkgName])

		for _, fn := range lr.Loaded.Builder.GetBuildFuncs() {
			fn(buildCtx)
		}

		allTargets[pkgName] = buildCtx.GetTargets()
	}

	vlog.Info("")
	vlog.Info("Targets found:")
	for pkgName, targets := range allTargets {
		for _, t := range targets {
			defaultMark := ""
			if !t.IsDefault() {
				defaultMark = " [disabled]"
			}
			vlog.Info("  - %s:%s (%s)%s", pkgName, t.Name(), t.Kind(), defaultMark)
		}
	}

	tc, tcName, err := GetToolchain(ctx.Config)
	if err != nil {
		vlog.Error("Toolchain error: %v", err)
		os.Exit(1)
	}
	vlog.Info("")
	vlog.Info("Using toolchain: %s", tcName)

	graph, err := build.NewBuildGraph(allTargets)
	if err != nil {
		vlog.Error("Dependency error: %v", err)
		os.Exit(1)
	}

	vlog.Info("")
	vlog.Info("Build order:")
	for _, fullName := range graph.Order {
		vlog.Info("  - %s", fullName)
	}

	pkgBuildDirs := GetPackageBuildDirs(ctx.Packages)

	scheduler, err := build.NewScheduler(graph, tc, pkgBuildDirs)
	if err != nil {
		vlog.Error("Scheduler error: %v", err)
		os.Exit(1)
	}

	vlog.Info("")
	vlog.Info("Building...")
	if err := scheduler.BuildAll(); err != nil {
		vlog.Error("Build failed: %v", err)
		os.Exit(1)
	}

	vlog.Info("")
	vlog.Info("Build succeeded!")
}
