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
			if opt.MacroName() == existing.MacroName() {
				continue
			}
			if opt.MacroName() == "" {
				continue
			}
			if existing.MacroName() == "" {
				clone := *existing
				clone.SetMacroName(opt.MacroName())
				result[name] = &clone
				continue
			}
			return nil, fmt.Errorf("global option '%s' macro name mismatch: already defined as %q, but %s defines as %q",
				name, existing.MacroName(), pkgName, opt.MacroName())
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

func validateGlobalOption(name string, existing, newOpt *Option, fromPkg string) error {
	if existing.Type() != newOpt.Type() {
		return fmt.Errorf("global option '%s' type mismatch: already defined as %s, but %s defines as %s",
			name, existing.Type(), fromPkg, newOpt.Type())
	}

	if existing.Default() != newOpt.Default() {
		return fmt.Errorf("global option '%s' default value mismatch: already defined as %v, but %s defines as %v",
			name, existing.Default(), fromPkg, newOpt.Default())
	}

	if !isBuiltinGlobalOption(name) && !slices.Equal(existing.Values(), newOpt.Values()) {
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
