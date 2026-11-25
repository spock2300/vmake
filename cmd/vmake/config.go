package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/api"
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

func runConfig(cmd *cobra.Command, args []string) {
	ctx := mustOpenProject(projectOptions{})
	defer closeProject(ctx)

	if len(setFlags) > 0 || len(unsetFlags) > 0 {
		runModifyConfig(ctx, setFlags, unsetFlags)
		return
	}

	workbench := pipeline.NewConfigWorkbench(ctx)
	if workbench == nil || (len(workbench.Options) == 0 && len(workbench.GlobalOptions) == 0) {
		vlog.Info("No configuration options found")
		return
	}
	workDir := ctx.WorkDir
	closeProject(ctx)

	result, err := tui.Run(tui.Input{Workbench: workbench, WorkDir: workDir})
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
			fatalErr(err)
		}
		req.Sets = append(req.Sets, pipeline.SetItem{Pkg: pkgName, Opt: optName, Raw: rawVal})
	}
	for _, f := range unsets {
		pkgName, optName, err := config.ParseUnsetFlag(f)
		if err != nil {
			fatalErr(err)
		}
		req.Unsets = append(req.Unsets, pipeline.UnsetItem{Pkg: pkgName, Opt: optName})
	}

	changes, err := pipeline.MutateConfig(ctx, req)
	if err != nil {
		fatalErr(err)
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
