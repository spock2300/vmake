package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/tui"
)

var setFlags []string

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Open a TUI to configure build options for all packages.",
	Long:  `Open a TUI to configure build options for all packages.`,
	Run:   runConfig,
}

func init() {
	configCmd.Flags().StringArrayVarP(&setFlags, "set", "s", nil,
		"Set option value non-interactively (format: [pkg/]option=value)")
	RootCmd.AddCommand(configCmd)
}

func runConfig(cmd *cobra.Command, args []string) {
	ctx := resolveToConfig(false)

	if len(setFlags) > 0 {
		runSetConfig(ctx, setFlags)
		return
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

type setEntry struct {
	pkgName string
	optName string
	rawVal  string
}

func parseSetFlag(flag string) (pkgName, optName, rawVal string, err error) {
	eqIdx := strings.Index(flag, "=")
	if eqIdx < 0 {
		return "", "", "", fmt.Errorf("missing '=' in --set flag: %s", flag)
	}

	keyPart := flag[:eqIdx]
	rawVal = flag[eqIdx+1:]

	if keyPart == "" {
		return "", "", "", fmt.Errorf("empty option name in --set flag: %s", flag)
	}

	lastSlash := strings.LastIndex(keyPart, "/")
	if lastSlash < 0 {
		return "", keyPart, rawVal, nil
	}

	pkgName = keyPart[:lastSlash]
	optName = keyPart[lastSlash+1:]
	if pkgName == "" || optName == "" {
		return "", "", "", fmt.Errorf("invalid format in --set flag: %s", flag)
	}
	return pkgName, optName, rawVal, nil
}

func coerceValue(raw string, optType api.OptionType) (any, error) {
	switch optType {
	case api.OptionBool:
		switch strings.ToLower(raw) {
		case "true", "on", "1":
			return true, nil
		case "false", "off", "0":
			return false, nil
		default:
			return nil, fmt.Errorf("invalid bool value: %q (use true/false, on/off, or 1/0)", raw)
		}
	case api.OptionInt:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid int value: %q", raw)
		}
		if f != float64(int(f)) {
			return nil, fmt.Errorf("invalid int value: %q (must be an integer)", raw)
		}
		return f, nil
	case api.OptionString, api.OptionChoice:
		return raw, nil
	default:
		return nil, fmt.Errorf("unknown option type: %d", optType)
	}
}

func resolveSetOption(pkgName, optName string, ctx *RuntimeContext) (*api.Option, error) {
	if pkgName == "" {
		opt, ok := ctx.GlobalOptions[optName]
		if !ok {
			available := sortedOptionNames(ctx.GlobalOptions)
			return nil, fmt.Errorf("global option %q not found (available: %s)", optName, strings.Join(available, ", "))
		}
		return opt, nil
	}

	pkgOpts, ok := ctx.AllOptions[pkgName]
	if !ok {
		available := sortedPackageNames(ctx.AllOptions)
		return nil, fmt.Errorf("package %q not found (available: %s)", pkgName, strings.Join(available, ", "))
	}

	opt, ok := pkgOpts[optName]
	if !ok {
		available := sortedOptionNames(pkgOpts)
		return nil, fmt.Errorf("option %q not found in package %q (available: %s)", optName, pkgName, strings.Join(available, ", "))
	}
	return opt, nil
}

func applySetValue(ctx *RuntimeContext, pkgName, optName string, value any) {
	if pkgName == "" {
		if ctx.Config.Global == nil {
			ctx.Config.Global = &config.GlobalConfig{Options: make(map[string]any)}
		}
		switch optName {
		case "toolchain":
			if s, ok := value.(string); ok {
				ctx.Config.Global.Toolchain = s
			}
		case "mode":
			if s, ok := value.(string); ok {
				ctx.Config.Global.Mode = s
			}
		default:
			if ctx.Config.Global.Options == nil {
				ctx.Config.Global.Options = make(map[string]any)
			}
			ctx.Config.Global.Options[optName] = value
		}
		return
	}

	entry := config.GetEntry(ctx.Config, pkgName)
	entry.Options[optName] = value
	config.SetEntry(ctx.Config, pkgName, entry)
}

func runSetConfig(ctx *RuntimeContext, flags []string) {
	entries := make([]setEntry, 0, len(flags))
	for _, f := range flags {
		pkgName, optName, rawVal, err := parseSetFlag(f)
		if err != nil {
			vlog.Error("Error: %v", err)
			os.Exit(1)
		}
		entries = append(entries, setEntry{pkgName: pkgName, optName: optName, rawVal: rawVal})
	}

	for _, e := range entries {
		opt, err := resolveSetOption(e.pkgName, e.optName, ctx)
		if err != nil {
			vlog.Error("Error: %v", err)
			os.Exit(1)
		}

		value, err := coerceValue(e.rawVal, opt.Type())
		if err != nil {
			prefix := e.optName
			if e.pkgName != "" {
				prefix = e.pkgName + "/" + e.optName
			}
			vlog.Error("Error: %s: %v", prefix, err)
			os.Exit(1)
		}

		if opt.Type() == api.OptionChoice {
			if !isValidChoice(value.(string), opt.Values()) {
				prefix := e.optName
				if e.pkgName != "" {
					prefix = e.pkgName + "/" + e.optName
				}
				vlog.Error("Error: %s: invalid choice %q (available: %s)", prefix, value, strings.Join(opt.Values(), ", "))
				os.Exit(1)
			}
		}

		applySetValue(ctx, e.pkgName, e.optName, value)
	}

	if err := config.Save(ctx.ConfigPath, ctx.Config); err != nil {
		fatalMsg("Failed to save config: %v", err)
	}

	for _, e := range entries {
		if e.pkgName != "" {
			vlog.Info("  %s/%s = %s", e.pkgName, e.optName, e.rawVal)
		} else {
			vlog.Info("  %s = %s", e.optName, e.rawVal)
		}
	}
	vlog.Info("Configuration saved to %s", ctx.ConfigPath)
}

func isValidChoice(val string, choices []string) bool {
	if len(choices) == 0 {
		return true
	}
	for _, c := range choices {
		if c == val {
			return true
		}
	}
	return false
}

func sortedOptionNames(opts map[string]*api.Option) []string {
	names := make([]string, 0, len(opts))
	for name := range opts {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

func sortedPackageNames(pkgs map[string]map[string]*api.Option) []string {
	names := make([]string, 0, len(pkgs))
	for name := range pkgs {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
