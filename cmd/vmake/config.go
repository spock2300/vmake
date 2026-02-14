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
	ctx, err := PrepareFull()
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}

	if len(ctx.AllOptions) == 0 && len(ctx.Packages) > 0 {
		if len(ctx.GlobalOptions) == 0 {
			vlog.Info("No configuration options found")
			return
		}
	}

	values := make(map[string]map[string]any)
	for pkgName := range ctx.AllOptions {
		pc := config.GetPackageConfig(ctx.Config, pkgName)
		values[pkgName] = pc.Options
	}

	globalValues := make(map[string]any)
	if ctx.Config.Global != nil {
		globalValues["toolchain"] = ctx.Config.Global.Toolchain
		globalValues["mode"] = ctx.Config.Global.Mode
		for k, v := range ctx.Config.Global.Options {
			globalValues[k] = v
		}
	}

	currentTC := ""
	if ctx.Config.Global != nil {
		currentTC = ctx.Config.Global.Toolchain
	}
	if currentTC == "" {
		currentTC = toolchain.GetManager().GetDefaultToolchain()
	}

	result, err := tui.Run(ctx.Packages, ctx.AllOptions, values, ctx.WorkDir, currentTC, ctx.GlobalOptions, globalValues)
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

	if ctx.Config.Global == nil {
		ctx.Config.Global = &config.GlobalConfig{Options: make(map[string]any)}
	}
	ctx.Config.Global.Toolchain = result.Toolchain

	for k, v := range result.GlobalValues {
		if k == "toolchain" {
			continue
		}
		if k == "mode" {
			if s, ok := v.(string); ok {
				ctx.Config.Global.Mode = s
			}
		} else {
			if ctx.Config.Global.Options == nil {
				ctx.Config.Global.Options = make(map[string]any)
			}
			ctx.Config.Global.Options[k] = v
		}
	}

	if err := config.Save(ctx.ConfigPath, ctx.Config); err != nil {
		vlog.Error("Failed to save config: %v", err)
		os.Exit(1)
	}

	vlog.Info("Configuration saved to %s", ctx.ConfigPath)
}
