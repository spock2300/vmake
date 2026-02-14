package api

import (
	"fmt"
)

const (
	ModeOptionName      = "mode"
	ToolchainOptionName = "toolchain"
	ModeDebug           = "debug"
	ModeRelease         = "release"
)

var BuiltInGlobalOptions = map[string]*Option{
	ModeOptionName: (&Option{}).
		SetType(OptionChoice).
		SetDefault(ModeDebug).
		SetDescription("Build mode").
		SetValues(ModeDebug, ModeRelease).
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

	for pkgName, opts := range allDefs {
		for name, opt := range opts {
			if !opt.IsGlobal() {
				continue
			}
			if existing, ok := result[name]; ok {
				if err := validateGlobalOption(name, existing, opt, pkgName); err != nil {
					return nil, err
				}
			} else {
				result[name] = opt
			}
		}
	}

	return result, nil
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

	return nil
}

func GetModeFlags(mode string) (cflags []string, defines []string) {
	switch mode {
	case ModeRelease:
		return []string{"-O2"}, []string{"NDEBUG"}
	case ModeDebug:
		return []string{"-O0", "-g"}, nil
	default:
		return []string{"-O0", "-g"}, nil
	}
}
