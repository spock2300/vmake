package main

import (
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/buildscript"
	"gitee.com/spock2300/vmake/pkg/config"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/tui"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Open a TUI to configure build options for all packages.",
	Long:  `Open a TUI to configure build options for all packages.`,
	Run:   runConfig,
}

func init() {
	RootCmd.AddCommand(configCmd)
}

func runConfig(cmd *cobra.Command, args []string) {
	ctx := mustInitContext()
	runThroughConfigPhase(ctx, false)

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

	currentTC := resolveToolchainName(ctx.Config, "")

	var sources []buildscript.Source
	localPkgs := make(map[string]bool)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.IsLocal() && node.Source != nil {
			sources = append(sources, buildscript.Source{
				Name:   node.Source.Name,
				Path:   node.Source.Path,
				Dir:    node.Source.Dir,
				Origin: node.Source.Origin,
			})
			localPkgs[node.Source.Name] = true
		}
	}

	deps := make(map[string][]string)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		deps[name] = node.Deps
	}

	result, err := tui.Run(sources, deps, ctx.AllOptions, values, ctx.WorkDir, currentTC, ctx.GlobalOptions, globalValues, ctx.AllKConfigs)
	fatalErr(err)

	if !result.Saved {
		vlog.Info("Configuration cancelled")
		return
	}

	configured := make(map[string]bool)
	for pkgName := range result.Values {
		configured[pkgName] = true
	}
	for pkgName := range ctx.AllKConfigs {
		configured[pkgName] = true
	}

	for pkgName := range configured {
		entry := config.GetEntry(ctx.Config, pkgName)

		if opts, ok := result.Values[pkgName]; ok {
			entry.Options = opts
		}

		if result.PresetValues != nil {
			if preset, ok := result.PresetValues[pkgName]; ok {
				entry.SelectedPreset = preset
			}
		}

		if result.MenuconfigRan[pkgName] {
			if kconfigs, ok := ctx.AllKConfigs[pkgName]; ok && len(kconfigs) > 0 {
				k := kconfigs[0]
				data, err := os.ReadFile(filepath.Join(k.SrcDir(), k.ConfigPath()))
				if err == nil {
					entry.KConfig = string(data)
				}
			}
		} else if result.PresetValues != nil {
			if _, presetChanged := result.PresetValues[pkgName]; presetChanged {
				entry.KConfig = ""
			}
		}

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
		fatalMsg("Failed to save config: %v", err)
	}

	vlog.Info("Configuration saved to %s", ctx.ConfigPath)
}
