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
	ctx, err := PrepareFull()
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}

	if err := executeBuild(ctx); err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}
}

func executeBuild(ctx *BuildContext) error {
	globalValues := make(map[string]any)
	if ctx.Config.Global != nil {
		globalValues["toolchain"] = ctx.Config.Global.Toolchain
		globalValues["mode"] = ctx.Config.Global.Mode
		for k, v := range ctx.Config.Global.Options {
			globalValues[k] = v
		}
	}

	mode := ""
	if m, ok := globalValues["mode"].(string); ok {
		mode = m
	}
	if mode == "" {
		mode = api.ModeDebug
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
		buildCtx.SetGlobalOptions(ctx.GlobalOptions)
		buildCtx.SetGlobalValues(globalValues)

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
		return err
	}
	vlog.Info("")
	vlog.Info("Using toolchain: %s, mode: %s", tcName, mode)

	graph, err := build.NewBuildGraph(allTargets)
	if err != nil {
		return err
	}

	vlog.Info("")
	vlog.Info("Build order:")
	for _, fullName := range graph.Order {
		vlog.Info("  - %s", fullName)
	}

	pkgDirs := GetPackageDirs(ctx.Packages)

	scheduler, err := build.NewScheduler(graph, tc, pkgDirs, mode)
	if err != nil {
		return err
	}

	vlog.Info("")
	vlog.Info("Building...")
	if err := scheduler.BuildAll(); err != nil {
		return err
	}

	vlog.Info("")
	vlog.Info("Build succeeded!")
	return nil
}
