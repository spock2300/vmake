package pipeline

import (
	"maps"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
)

func DisplayPackageValues(ctx *RuntimeContext, name string, globalValues map[string]any) map[string]any {
	opts := ctx.AllOptions[name]
	values := maps.Clone(config.GetEntry(ctx.Config, name).Options)
	if values == nil {
		values = make(map[string]any)
	}
	for optName, value := range values {
		opt, declared := opts[optName]
		if !declared || !opt.IsGlobal() {
			continue
		}
		if _, resolved := globalValues[optName]; !resolved {
			continue
		}
		if api.NormalizeOptionValue(opt, value) == api.NormalizeOptionValue(opt, opt.Default()) {
			delete(values, optName)
		}
	}
	if ctx.DerivedOptions != nil {
		for optName, value := range ctx.DerivedOptions[name] {
			if _, exists := values[optName]; !exists && value != nil {
				values[optName] = value
			}
		}
	}
	for optName, opt := range opts {
		if _, resolved := globalValues[optName]; resolved && opt.IsGlobal() {
			continue
		}
		if _, exists := values[optName]; !exists && opt.Default() != nil {
			values[optName] = opt.Default()
		}
	}
	for optName, value := range globalValues {
		if _, exists := values[optName]; !exists {
			values[optName] = value
		}
	}
	for optName := range values {
		if _, declared := opts[optName]; !declared {
			delete(values, optName)
		}
	}
	return values
}

func applyGlobalValues(ctx *RuntimeContext, needed map[string]bool) map[string]any {
	values := ProjectGlobalValues(ctx)
	values[api.ModeOptionName] = ResolveMode(ctx.Config, ctx.ModeOverride)

	profile := ctx.ChipProfile
	if profile.Name == "" {
		resolved := false
		if needed != nil {
			if candidate, _, err := neededChipProfile(ctx, needed); err == nil && candidate.Name != "" {
				profile, resolved = candidate, true
			}
		}
		if !resolved {
			profile, _ = selectedChipProfile(ctx)
		}
	}
	preferred := ctx.PreferredToolchain
	if preferred == "" {
		preferred = profile.PreferredToolchain
	}
	tcName := ResolveToolchainName(ctx.Config, ctx.ToolchainOverride, preferred)
	values[api.ToolchainOptionName] = tcName
	if chipTargetsApply(profile, tcName) {
		fillTargetDefaults(values, profile.TargetOS, profile.TargetTriple)
	}
	if os, triple := toolchainTargetDefaults(tcName); os != "" || triple != "" {
		fillTargetDefaults(values, os, triple)
	}
	if _, ok := values[api.TargetOSOptionName]; !ok {
		values[api.TargetOSOptionName] = ""
	}
	if _, ok := values[api.TargetTripleOptionName]; !ok {
		values[api.TargetTripleOptionName] = ""
	}
	return values
}
