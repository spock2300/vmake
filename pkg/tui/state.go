package tui

import "github.com/spock2300/vmake/pkg/api"

type initialState struct {
	globalValues    map[string]any
	values          map[string]map[string]any
	derivedGlobals  map[string]bool
	explicitValues  map[string]map[string]bool
	explicitGlobals map[string]bool
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

	explicitGlobals := make(map[string]bool)
	for name, val := range in.GlobalValues {
		if val == nil {
			continue
		}
		if name == api.ToolchainOptionName || name == api.TargetOSOptionName || name == api.TargetTripleOptionName {
			if s, ok := val.(string); !ok || s == "" {
				continue
			}
		}
		explicitGlobals[name] = true
	}

	explicitValues := make(map[string]map[string]bool)
	for pkgName, pkgVals := range in.Values {
		for name, val := range pkgVals {
			if val == nil {
				continue
			}
			if opts := options[pkgName]; opts != nil {
				if opt, ok := opts[name]; ok && opt.IsGlobal() {
					continue
				}
			}
			if explicitValues[pkgName] == nil {
				explicitValues[pkgName] = make(map[string]bool)
			}
			explicitValues[pkgName][name] = true
		}
	}

	explicitToolchain := explicitGlobals[api.ToolchainOptionName]
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
				if derivedPkg := in.DerivedOptions[pkgName]; derivedPkg != nil {
					if dv, hasDerived := derivedPkg[name]; hasDerived && dv != nil {
						values[pkgName][name] = dv
						continue
					}
				}
				if def := opt.Default(); def != nil {
					values[pkgName][name] = def
				}
			}
		}
	}
	return initialState{
		globalValues:    globalValues,
		values:          values,
		derivedGlobals:  derivedGlobals,
		explicitValues:  explicitValues,
		explicitGlobals: explicitGlobals,
	}
}
