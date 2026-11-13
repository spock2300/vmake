package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/pkgvis"
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

	currentTC := pipeline.ResolveToolchainName(ctx.Config, "", ctx.PreferredToolchain)

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
				Script: node.Source.Script,
			})
			localPkgs[node.Source.Name] = true
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
		Options:          ctx.AllOptions,
		Values:           values,
		DerivedOptions:   ctx.DerivedOptions,
		WorkDir:          ctx.WorkDir,
		CurrentToolchain: currentTC,
		GlobalOptions:    ctx.GlobalOptions,
		GlobalValues:     globalValues,
		ChipProfiles:     ctx.ChipProfiles,
		KConfigs:         ctx.AllKConfigs,
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
	ctx.Config.Description = result.Description

	configured := make(map[string]bool)
	for pkgName := range ctx.Config.Entries {
		configured[pkgName] = true
	}
	for pkgName := range result.Values {
		configured[pkgName] = true
	}
	for pkgName := range ctx.AllKConfigs {
		configured[pkgName] = true
	}

	for pkgName := range configured {
		entry := config.GetEntry(ctx.Config, pkgName)
		entry.Options = result.Values[pkgName]

		if result.PresetValues != nil {
			if preset, ok := result.PresetValues[pkgName]; ok {
				entry.SelectedPreset = preset
			}
		}

		if result.MenuconfigRan[pkgName] {
			if kconfigs, ok := ctx.AllKConfigs[pkgName]; ok && len(kconfigs) > 0 {
				k := kconfigs[0]
				configPath := filepath.Join(k.SrcDir(), k.ConfigPath())
				data, err := os.ReadFile(configPath)
				if err == nil {
					entry.KConfig = string(data)
				} else if k.ConfigPath() != "" && k.ConfigPath() != ".config" {
					vlog.Error("menuconfig did not write %s; a custom MenuconfigCmd must honor KCONFIG_CONFIG", configPath)
				}
			}
		} else if result.PresetValues != nil {
			if _, presetChanged := result.PresetValues[pkgName]; presetChanged {
				entry.KConfig = ""
			}
		}

		config.SetEntry(ctx.Config, pkgName, entry)
	}

	ctx.Config.Global = &config.GlobalConfig{Options: make(map[string]any)}
	if !result.DerivedGlobals[api.ToolchainOptionName] {
		if result.GlobalValues != nil {
			if tc, ok := result.GlobalValues[api.ToolchainOptionName].(string); ok {
				ctx.Config.Global.Toolchain = tc
			} else if result.Toolchain != "" {
				ctx.Config.Global.Toolchain = result.Toolchain
			}
		} else if result.Toolchain != "" {
			ctx.Config.Global.Toolchain = result.Toolchain
		}
	}

	for k, v := range result.GlobalValues {
		if k == api.ToolchainOptionName {
			continue
		}
		if result.DerivedGlobals[k] {
			continue
		}
		if k == api.ModeOptionName {
			if s, ok := v.(string); ok {
				ctx.Config.Global.Mode = s
			}
		} else {
			ctx.Config.Global.Options[k] = v
		}
	}

	config.PruneEmptyEntries(ctx.Config)
	return config.Save(ctx.ConfigPath, ctx.Config)
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

func isProjectVisiblePkg(ctx *RuntimeContext, candidate string) bool {
	if ctx == nil || ctx.Resolver == nil {
		return true
	}
	return pkgvis.CheckInternal("@local", "@local", candidate, ctx.Resolver.VisibilityPath(candidate)) == nil
}

func resolveSetPackageName(ctx *RuntimeContext, pkgName string) string {
	if pkgName == "" || strings.Contains(pkgName, "/") || ctx == nil {
		return pkgName
	}
	if _, ok := ctx.AllOptions[pkgName]; ok {
		return pkgName
	}
	if ctx.DepGraph != nil && ctx.DepGraph.Packages[pkgName] != nil {
		return pkgName
	}
	suffix := "/" + pkgName
	match := ""
	for candidate := range ctx.AllOptions {
		if strings.HasSuffix(candidate, suffix) && isProjectVisiblePkg(ctx, candidate) {
			if match != "" && match != candidate {
				return pkgName
			}
			match = candidate
		}
	}
	if match != "" {
		return match
	}
	if ctx.DepGraph != nil {
		for candidate := range ctx.DepGraph.Packages {
			if strings.HasSuffix(candidate, suffix) && isProjectVisiblePkg(ctx, candidate) {
				if match != "" && match != candidate {
					return pkgName
				}
				match = candidate
			}
		}
	}
	if match != "" {
		return match
	}
	return pkgName
}

func resolveSetPackageOption(pkgName, optName string, ctx *RuntimeContext) (string, *api.Option, error) {
	if pkgName == "" {
		opt, ok := ctx.GlobalOptions[optName]
		if !ok {
			available := sortedOptionNames(ctx.GlobalOptions)
			return "", nil, fmt.Errorf("global option %q not found (available: %s)", optName, strings.Join(available, ", "))
		}
		return "", opt, nil
	}

	resolvedPkg := resolveSetPackageName(ctx, pkgName)
	if ctx != nil && ctx.Resolver != nil {
		if err := pkgvis.CheckInternal("@local", "@local", resolvedPkg, ctx.Resolver.VisibilityPath(resolvedPkg)); err != nil {
			return "", nil, err
		}
	}
	pkgOpts, ok := ctx.AllOptions[resolvedPkg]
	if !ok {
		if ctx != nil && ctx.Resolver != nil {
			suffix := "/" + pkgName
			for candidate := range ctx.AllOptions {
				if strings.HasSuffix(candidate, suffix) {
					if err := pkgvis.CheckInternal("@local", "@local", candidate, ctx.Resolver.VisibilityPath(candidate)); err != nil {
						return "", nil, err
					}
				}
			}
		}
		available := sortedPackageNames(ctx.AllOptions)
		return "", nil, fmt.Errorf("package %q not found (available: %s)", pkgName, strings.Join(available, ", "))
	}

	opt, ok := pkgOpts[optName]
	if !ok {
		available := sortedOptionNames(pkgOpts)
		return "", nil, fmt.Errorf("option %q not found in package %q (available: %s)", optName, resolvedPkg, strings.Join(available, ", "))
	}
	if opt.IsGlobal() {
		return "", nil, fmt.Errorf("option %q in package %q is global; set it without a package prefix: %s=...", optName, resolvedPkg, optName)
	}
	return resolvedPkg, opt, nil
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

type unsetEntry struct {
	pkgName string
	optName string
}

func parseUnsetFlag(flag string) (pkgName, optName string, err error) {
	trimmed := strings.TrimSpace(flag)
	if trimmed == "" {
		return "", "", fmt.Errorf("empty option name in --unset flag")
	}
	if strings.Contains(trimmed, "=") {
		return "", "", fmt.Errorf("unexpected '=' in --unset flag: %s (expected [pkg/]option)", flag)
	}
	lastSlash := strings.LastIndex(trimmed, "/")
	if lastSlash < 0 {
		return "", trimmed, nil
	}
	pkgName = trimmed[:lastSlash]
	optName = trimmed[lastSlash+1:]
	if pkgName == "" || optName == "" {
		return "", "", fmt.Errorf("invalid format in --unset flag: %s", flag)
	}
	return pkgName, optName, nil
}

func applyUnsetValue(ctx *RuntimeContext, pkgName, optName string) {
	if pkgName == "" {
		if ctx.Config.Global == nil {
			return
		}
		switch optName {
		case "toolchain":
			ctx.Config.Global.Toolchain = ""
		case "mode":
			ctx.Config.Global.Mode = ""
		default:
			if ctx.Config.Global.Options != nil {
				delete(ctx.Config.Global.Options, optName)
			}
		}
		return
	}
	if ctx.Config.Entries == nil {
		return
	}
	entry, ok := ctx.Config.Entries[pkgName]
	if !ok || entry == nil || entry.Options == nil {
		return
	}
	delete(entry.Options, optName)
}

func hasExplicitConfigKey(ctx *RuntimeContext, pkgName, optName string) (string, bool) {
	if ctx == nil || ctx.Config == nil {
		return pkgName, false
	}
	if pkgName == "" {
		if ctx.Config.Global == nil {
			return "", false
		}
		switch optName {
		case "toolchain":
			return "", ctx.Config.Global.Toolchain != ""
		case "mode":
			return "", ctx.Config.Global.Mode != ""
		default:
			_, ok := ctx.Config.Global.Options[optName]
			return "", ok
		}
	}
	resolvedPkg := resolveSetPackageName(ctx, pkgName)
	for _, cand := range []string{resolvedPkg, pkgName} {
		if entry := ctx.Config.Entries[cand]; entry != nil && entry.Options != nil {
			if _, ok := entry.Options[optName]; ok {
				return cand, true
			}
		}
	}
	return pkgName, false
}

func runModifyConfig(ctx *RuntimeContext, sets []string, unsets []string) {
	setEntries := make([]setEntry, 0, len(sets))
	for _, f := range sets {
		pkgName, optName, rawVal, err := parseSetFlag(f)
		if err != nil {
			vlog.Error("Error: %v", err)
			os.Exit(1)
		}
		setEntries = append(setEntries, setEntry{pkgName: pkgName, optName: optName, rawVal: rawVal})
	}

	unsetEntries := make([]unsetEntry, 0, len(unsets))
	for _, f := range unsets {
		pkgName, optName, err := parseUnsetFlag(f)
		if err != nil {
			vlog.Error("Error: %v", err)
			os.Exit(1)
		}
		unsetEntries = append(unsetEntries, unsetEntry{pkgName: pkgName, optName: optName})
	}

	for i := range unsetEntries {
		u := &unsetEntries[i]
		resolvedPkg, _, err := resolveSetPackageOption(u.pkgName, u.optName, ctx)
		if err != nil {
			if existingPkg, ok := hasExplicitConfigKey(ctx, u.pkgName, u.optName); ok {
				u.pkgName = existingPkg
			} else {
				vlog.Error("Error: %v", err)
				os.Exit(1)
			}
		} else {
			u.pkgName = resolvedPkg
		}
		applyUnsetValue(ctx, u.pkgName, u.optName)
	}

	for i := range setEntries {
		e := &setEntries[i]
		resolvedPkg, opt, err := resolveSetPackageOption(e.pkgName, e.optName, ctx)
		if err != nil {
			vlog.Error("Error: %v", err)
			os.Exit(1)
		}
		e.pkgName = resolvedPkg

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

	config.PruneEmptyEntries(ctx.Config)
	if err := config.Save(ctx.ConfigPath, ctx.Config); err != nil {
		fatalMsg("Failed to save config: %v", err)
	}

	for _, u := range unsetEntries {
		if u.pkgName != "" {
			vlog.Info("  unset %s/%s", u.pkgName, u.optName)
		} else {
			vlog.Info("  unset %s", u.optName)
		}
	}
	for _, e := range setEntries {
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
	return slices.Sorted(maps.Keys(opts))
}

func sortedPackageNames(pkgs map[string]map[string]*api.Option) []string {
	return slices.Sorted(maps.Keys(pkgs))
}
