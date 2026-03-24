package main

import (
	"os"

	"gitee.com/spock2300/vmake/pkg/buildscript"
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
	ctx := mustInitContext()

	if err := runRequirePhase(ctx, false); err != nil {
		vlog.Error("Phase 1 (OnRequire) failed: %v", err)
		os.Exit(1)
	}

	if err := ctx.Resolver.ResolveDeferred(); err != nil {
		vlog.Error("Resolve deferred packages failed: %v", err)
		os.Exit(1)
	}

	if err := runConfigPhase(ctx); err != nil {
		vlog.Error("Phase 2 (OnConfig) failed: %v", err)
		os.Exit(1)
	}

	hasOptions := len(ctx.AllOptions) > 0
	if !hasOptions && len(ctx.GlobalOptions) == 0 {
		vlog.Info("No configuration options found")
		return
	}

	values := make(map[string]map[string]any)
	for pkgName := range ctx.AllOptions {
		entry := config.GetEntry(ctx.Config, pkgName)
		values[pkgName] = entry.Options
	}

	globalValues := config.BuildGlobalValues(ctx.Config)

	currentTC := ""
	if ctx.Config.Global != nil {
		currentTC = ctx.Config.Global.Toolchain
	}
	if currentTC == "" {
		currentTC = toolchain.GetManager().GetDefaultToolchain()
	}

	var sources []buildscript.Source
	localPkgs := make(map[string]bool)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.IsLocal() && node.Source != nil {
			sources = append(sources, buildscript.Source{
				Name:   node.Source.ID,
				Path:   node.Source.BuildGo,
				Dir:    node.Source.Dir,
				Origin: node.Source.Origin,
			})
			localPkgs[node.Source.ID] = true
		}
	}

	deps := make(map[string][]string)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		deps[name] = node.Deps
	}

	result, err := tui.Run(sources, deps, ctx.AllOptions, values, ctx.WorkDir, currentTC, ctx.GlobalOptions, globalValues)
	if err != nil {
		vlog.Error("TUI error: %v", err)
		os.Exit(1)
	}

	if !result.Saved {
		vlog.Info("Configuration cancelled")
		return
	}

	for pkgName, opts := range result.Values {
		entry := config.GetEntry(ctx.Config, pkgName)
		entry.Options = opts
		config.SetEntry(ctx.Config, pkgName, entry)
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
