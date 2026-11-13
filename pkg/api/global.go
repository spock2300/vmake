package api

import (
	"fmt"
	"slices"
	"sort"
)

const (
	ModeOptionName      = "mode"
	ToolchainOptionName = "toolchain"
	ModeDebug           = "debug"
	ModeRelease         = "release"
	ModeSize            = "size"
)

var BuiltInGlobalOptions = map[string]*Option{
	ModeOptionName: (&Option{}).
		SetType(OptionChoice).
		SetDefault(ModeRelease).
		SetDescription("Build mode").
		SetValues(ModeDebug, ModeRelease, ModeSize).
		SetGroup("Global"),
}

func MergeGlobalOptions(allDefs map[string]map[string]*Option, toolchainList []string) (map[string]*Option, error) {
	result := make(map[string]*Option)

	for name, opt := range BuiltInGlobalOptions {
		result[name] = opt
	}

	if len(toolchainList) > 0 {
		defaultTC := toolchainList[0]
		result[ToolchainOptionName] = (&Option{}).
			SetType(OptionChoice).
			SetDefault(defaultTC).
			SetDescription("Build toolchain").
			SetValues(toolchainList...).
			SetGroup("Global")
	}

	pkgNames := make([]string, 0, len(allDefs))
	for pkgName := range allDefs {
		pkgNames = append(pkgNames, pkgName)
	}
	sort.Strings(pkgNames)
	for _, pkgName := range pkgNames {
		opts := allDefs[pkgName]
		optNames := make([]string, 0, len(opts))
		for name := range opts {
			optNames = append(optNames, name)
		}
		sort.Strings(optNames)
		for _, name := range optNames {
			opt := opts[name]
			if !opt.IsGlobal() {
				continue
			}
			existing, ok := result[name]
			if !ok {
				result[name] = opt
				continue
			}
			if err := validateGlobalOption(name, existing, opt, pkgName); err != nil {
				return nil, err
			}
			if existing.ExportDefine() != opt.ExportDefine() {
				return nil, fmt.Errorf("global option '%s' export define mismatch: already defined as %v, but %s defines as %v",
					name, existing.ExportDefine(), pkgName, opt.ExportDefine())
			}
			merged := existing
			cloned := false
			ensureClone := func() *Option {
				if !cloned {
					clone := *merged
					clone.values = append([]string(nil), merged.values...)
					merged = &clone
					result[name] = merged
					cloned = true
				}
				return merged
			}
			if name == ChipOptionName {
				if isUnsetDefault(merged.Default()) && !isUnsetDefault(opt.Default()) {
					ensureClone().SetDefault(opt.Default())
				}
				for _, v := range opt.Values() {
					if !slices.Contains(merged.Values(), v) {
						ensureClone().values = append(merged.values, v)
					}
				}
			}
			if opt.MacroName() == merged.MacroName() || opt.MacroName() == "" {
				continue
			}
			if merged.MacroName() == "" {
				ensureClone().SetMacroName(opt.MacroName())
				continue
			}
			return nil, fmt.Errorf("global option '%s' macro name mismatch: already defined as %q, but %s defines as %q",
				name, merged.MacroName(), pkgName, opt.MacroName())
		}
	}

	return result, nil
}

func isBuiltinGlobalOption(name string) bool {
	if _, ok := BuiltInGlobalOptions[name]; ok {
		return true
	}
	return name == ToolchainOptionName
}

func isUnsetDefault(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}

func validateGlobalOption(name string, existing, newOpt *Option, fromPkg string) error {
	if existing.Type() != newOpt.Type() {
		return fmt.Errorf("global option '%s' type mismatch: already defined as %s, but %s defines as %s",
			name, existing.Type(), fromPkg, newOpt.Type())
	}

	if name == ChipOptionName {
		if !isUnsetDefault(existing.Default()) && !isUnsetDefault(newOpt.Default()) && existing.Default() != newOpt.Default() {
			return fmt.Errorf("global option '%s' default value mismatch: already defined as %v, but %s defines as %v",
				name, existing.Default(), fromPkg, newOpt.Default())
		}
	} else if existing.Default() != newOpt.Default() {
		return fmt.Errorf("global option '%s' default value mismatch: already defined as %v, but %s defines as %v",
			name, existing.Default(), fromPkg, newOpt.Default())
	}

	if !isBuiltinGlobalOption(name) && name != ChipOptionName && !slices.Equal(existing.Values(), newOpt.Values()) {
		return fmt.Errorf("global option '%s' values mismatch: already defined as %v, but %s defines as %v",
			name, existing.Values(), fromPkg, newOpt.Values())
	}

	return nil
}

func GetModeFlags(mode string) (cflags []string, defines []string) {
	switch mode {
	case ModeRelease:
		return []string{"-O2"}, []string{"NDEBUG"}
	case ModeDebug:
		return []string{"-O0", "-g"}, nil
	case ModeSize:
		return []string{"-Os"}, []string{"NDEBUG"}
	default:
		return []string{"-O0", "-g"}, nil
	}
}
