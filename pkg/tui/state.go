package tui

import "github.com/spock2300/vmake/pkg/api"

type initialState struct {
	globalValues   map[string]any
	values         map[string]map[string]any
	derivedGlobals map[string]bool
}

func resolveInitialState(in Input) initialState {
	options := in.Options
	globalOptions := in.GlobalOptions
	values := deepCopyValues(in.Values)
	globalValues := deepCopyGlobal(in.GlobalValues)
	chipProfiles := in.ChipProfiles
	if chipProfiles == nil {
		chipProfiles = make(map[string]api.ChipProfile)
	}

	explicitToolchain := false
	if value, ok := globalValues[api.ToolchainOptionName].(string); ok && value != "" {
		explicitToolchain = true
	}
	chip := ""
	if value, ok := globalValues[api.ChipOptionName].(string); ok {
		chip = value
	}
	if chip == "" {
		if opt, ok := globalOptions[api.ChipOptionName]; ok {
			if value, ok := opt.Default().(string); ok {
				chip = value
			}
		}
	}
	profile, hasProfile := chipProfiles[chip]
	derivedGlobals := make(map[string]bool)
	if !explicitToolchain {
		delete(globalValues, api.ToolchainOptionName)
		toolchainName := in.CurrentToolchain
		if toolchainName == "" && hasProfile {
			toolchainName = profile.PreferredToolchain
		}
		if toolchainName != "" {
			globalValues[api.ToolchainOptionName] = toolchainName
		}
		derivedGlobals[api.ToolchainOptionName] = true
	}
	for _, name := range []string{api.TargetOSOptionName, api.TargetTripleOptionName} {
		if value, ok := globalValues[name].(string); !ok || value == "" {
			derivedGlobals[name] = true
		}
	}
	if _, ok := globalValues["mode"]; !ok {
		if opt, ok := globalOptions["mode"]; ok {
			if d, ok := opt.Default().(string); ok {
				globalValues["mode"] = d
			}
		}
	}
	if _, ok := globalValues["mode"]; !ok {
		globalValues["mode"] = api.ModeRelease
	}

	fillOptionDefaults(globalValues, globalOptions)

	for pkgName, opts := range options {
		if values[pkgName] == nil {
			values[pkgName] = make(map[string]any)
		}
		for name, opt := range opts {
			if opt.IsGlobal() {
				delete(values[pkgName], name)
				continue
			}
			if _, ok := values[pkgName][name]; !ok {
				if def := opt.Default(); def != nil {
					values[pkgName][name] = def
				}
			}
		}
	}
	return initialState{globalValues: globalValues, values: values, derivedGlobals: derivedGlobals}
}
