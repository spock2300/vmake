package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/tui"
)

var (
	setFlags   []string
	unsetFlags []string
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Edit the active build configuration or manage configuration files",
	Long:  `Open a TUI to edit the active configuration, or use list, use, copy, describe and check to manage configuration files in configs/.`,
	Args:  cobra.NoArgs,
	Run:   runConfig,
}

func init() {
	addConfigFlag(configCmd)
	configCmd.Flags().StringArrayVarP(&setFlags, "set", "s", nil,
		"Set option value non-interactively (format: [pkg/]option=value)")
	configCmd.Flags().StringArrayVarP(&unsetFlags, "unset", "u", nil,
		"Remove explicit option override and revert to default (format: [pkg/]option)")
	RootCmd.AddCommand(configCmd)
	configCmd.AddCommand(newConfigListCmd(), newConfigUseCmd(), newConfigCopyCmd(), newConfigDescribeCmd(), newConfigCheckCmd())
}

func runConfig(cmd *cobra.Command, args []string) {
	ctx := mustOpenProject(projectOptions{})
	defer closeProject(ctx)

	if len(setFlags) > 0 || len(unsetFlags) > 0 {
		runModifyConfig(ctx, setFlags, unsetFlags)
		return
	}

	allOpts := ctx.AllOptions()
	hasOptions := len(allOpts) > 0
	if !hasOptions && len(ctx.GlobalOptions()) == 0 {
		vlog.Info("No configuration options found")
		return
	}

	values := make(map[string]map[string]any)
	for pkgName := range allOpts {
		entry := config.GetEntry(ctx.Config, pkgName)
		values[pkgName] = entry.Options
	}

	globalValues := config.BuildGlobalValues(ctx.Config)
	currentTC := pipeline.ResolveToolchainName(ctx.Config, "", ctx.PreferredToolchain)

	var sources []buildscript.Source
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.IsLocal() && node.Source != nil {
			sources = append(sources, buildscript.Source{
				Name:   node.Source.Name,
				Path:   node.Source.Path,
				Dir:    node.Source.Dir,
				Origin: node.Source.Origin,
				Script: node.Source.Script,
			})
		}
	}

	deps := make(map[string][]string)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		deps[name] = node.Deps
	}

	closeProject(ctx)
	result, err := tui.Run(tui.Input{
		Packages:         sources,
		Deps:             deps,
		Options:          allOpts,
		Values:           values,
		DerivedOptions:   ctx.DerivedOptions(),
		WorkDir:          ctx.WorkDir,
		CurrentToolchain: currentTC,
		GlobalOptions:    ctx.GlobalOptions(),
		GlobalValues:     globalValues,
		ChipProfiles:     ctx.ChipProfiles(),
		KConfigs:         ctx.KConfigs(),
		Description:      ctx.Config.Description,
		DisplayDefaults:  pipeline.ToolchainTargetDefaults,
	})
	fatalErr(err)

	if !result.Saved {
		vlog.Info("Configuration cancelled")
		return
	}

	fatalErr(saveConfigResult(ctx, result))
	vlog.Info("Configuration saved to %s", ctx.ConfigPath)
}

func saveConfigResult(ctx *RuntimeContext, result *tui.ConfigResult) error {
	committed, err := result.Commit(ctx.Config, ctx.KConfigs(), func(pkg string, k *api.KConfigEntry) (string, error) {
		configPath := filepath.Join(k.SrcDir(), k.ConfigPath())
		data, err := os.ReadFile(configPath)
		if err != nil {
			if k.ConfigPath() != "" && k.ConfigPath() != ".config" {
				vlog.Error("menuconfig did not write %s; a custom MenuconfigCmd must honor KCONFIG_CONFIG", configPath)
			}
			return "", err
		}
		return string(data), nil
	})
	if err != nil {
		return err
	}
	*ctx.Config = *committed
	return config.Save(ctx.ConfigPath, ctx.Config)
}

func runModifyConfig(ctx *RuntimeContext, sets []string, unsets []string) {
	var req pipeline.ConfigMutation
	for _, f := range sets {
		pkgName, optName, rawVal, err := config.ParseSetFlag(f)
		if err != nil {
			vlog.Error("Error: %v", err)
			os.Exit(1)
		}
		req.Sets = append(req.Sets, pipeline.SetItem{Pkg: pkgName, Opt: optName, Raw: rawVal})
	}
	for _, f := range unsets {
		pkgName, optName, err := config.ParseUnsetFlag(f)
		if err != nil {
			vlog.Error("Error: %v", err)
			os.Exit(1)
		}
		req.Unsets = append(req.Unsets, pipeline.UnsetItem{Pkg: pkgName, Opt: optName})
	}

	changes, err := pipeline.MutateConfig(ctx, req)
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}

	config.PruneEmptyEntries(ctx.Config)
	if err := config.Save(ctx.ConfigPath, ctx.Config); err != nil {
		fatalMsg("Failed to save config: %v", err)
	}

	for _, ch := range changes {
		if ch.Kind == "unset" {
			vlog.Info("  unset %s", ch.Key())
		}
	}
	for _, ch := range changes {
		if ch.Kind == "set" {
			vlog.Info("  %s = %s", ch.Key(), ch.Raw)
		}
	}
	vlog.Info("Configuration saved to %s", ctx.ConfigPath)
}
