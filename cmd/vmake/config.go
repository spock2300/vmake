package main

import (
	"os"

	"gitee.com/spock2300/vmake/pkg/config"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/toolchain"
	"gitee.com/spock2300/vmake/pkg/tui"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configure project options",
	Long:  `Open a TUI to configure build options for all packages.`,
	Run:   runConfig,
}

func init() {
	RootCmd.AddCommand(configCmd)
}

func runConfig(cmd *cobra.Command, args []string) {
	ctx, err := PrepareBuild()
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}

	if len(ctx.AllOptions) == 0 && len(ctx.Packages) > 0 {
		mgr := toolchain.GetManager()
		if tcs, err := mgr.ListToolchains(); err != nil || len(tcs) == 0 {
			vlog.Info("No configuration options found")
			return
		}
	}

	values := make(map[string]map[string]any)
	for pkgName := range ctx.AllOptions {
		pc := config.GetPackageConfig(ctx.Config, pkgName)
		values[pkgName] = pc.Options
	}

	currentTC := ctx.Config.Toolchain
	if currentTC == "" {
		currentTC = toolchain.GetManager().GetDefaultToolchain()
	}

	result, err := tui.Run(ctx.Packages, ctx.AllOptions, values, ctx.WorkDir, currentTC)
	if err != nil {
		vlog.Error("TUI error: %v", err)
		os.Exit(1)
	}

	if !result.Saved {
		vlog.Info("Configuration cancelled")
		return
	}

	for pkgName, opts := range result.Values {
		if ctx.Config.Packages[pkgName] == nil {
			ctx.Config.Packages[pkgName] = &config.PackageConfig{Options: make(map[string]any)}
		}
		ctx.Config.Packages[pkgName].Options = opts
	}

	ctx.Config.Toolchain = result.Toolchain

	if err := config.Save(ctx.ConfigPath, ctx.Config); err != nil {
		vlog.Error("Failed to save config: %v", err)
		os.Exit(1)
	}

	vlog.Info("Configuration saved to %s", ctx.ConfigPath)
}
