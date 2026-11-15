package pipeline

import (
	"maps"

	"github.com/spock2300/vmake/pkg/api"
)

// Invocation is the immutable command/project input. It is currently the same
// shape as ResolveParams; the alias keeps callers stable.
type Invocation = ResolveParams

// Configuration is the output of Configure (and the derived-option refresh in
// the build prelude). It owns every field the configuration phase produces.
type Configuration struct {
	AllOptions          map[string]map[string]*api.Option
	AllKConfigs         map[string][]*api.KConfigEntry
	ChipProfiles        map[string]api.ChipProfile
	ChipProfile         api.ChipProfile
	GlobalOptions       map[string]*api.Option
	DerivedOptions      map[string]map[string]any
	BufferedGlobalFlags map[string]*packageGlobalFlags
	GlobalMacroDefines  []string
}

func (ctx *RuntimeContext) currentConfiguration() Configuration {
	return Configuration{
		AllOptions:          ctx.AllOptions,
		AllKConfigs:         ctx.allKConfigs,
		ChipProfiles:        ctx.chipProfiles,
		ChipProfile:         ctx.chipProfile,
		GlobalOptions:       ctx.globalOptions,
		DerivedOptions:      ctx.derivedOptions,
		BufferedGlobalFlags: ctx.bufferedGlobalFlags,
		GlobalMacroDefines:  ctx.globalMacroDefines,
	}
}

func (ctx *RuntimeContext) applyConfiguration(result Configuration) {
	ctx.AllOptions = result.AllOptions
	ctx.allKConfigs = result.AllKConfigs
	ctx.chipProfiles = result.ChipProfiles
	ctx.chipProfile = result.ChipProfile
	ctx.globalOptions = result.GlobalOptions
	ctx.derivedOptions = result.DerivedOptions
	ctx.bufferedGlobalFlags = result.BufferedGlobalFlags
	ctx.globalMacroDefines = result.GlobalMacroDefines
}

// GlobalDefines returns the configuration's macro defines.
func (ctx *RuntimeContext) GlobalDefines() []string {
	if ctx == nil {
		return nil
	}
	return append([]string(nil), ctx.globalMacroDefines...)
}

// GlobalOptions returns the project's global option definitions. Option
// values are shared read-only; the map itself is a copy.
func (ctx *RuntimeContext) GlobalOptions() map[string]*api.Option {
	if ctx == nil || ctx.globalOptions == nil {
		return nil
	}
	return maps.Clone(ctx.globalOptions)
}

// DerivedOptions returns the option values derived by OnApply callbacks.
func (ctx *RuntimeContext) DerivedOptions() map[string]map[string]any {
	if ctx == nil || ctx.derivedOptions == nil {
		return nil
	}
	out := make(map[string]map[string]any, len(ctx.derivedOptions))
	for name, values := range ctx.derivedOptions {
		out[name] = maps.Clone(values)
	}
	return out
}

// ChipProfiles returns the discovered chip profiles.
func (ctx *RuntimeContext) ChipProfiles() map[string]api.ChipProfile {
	if ctx == nil || ctx.chipProfiles == nil {
		return nil
	}
	out := make(map[string]api.ChipProfile, len(ctx.chipProfiles))
	for name, profile := range ctx.chipProfiles {
		out[name] = profile
	}
	return out
}

// KConfigs returns the configuration's per-package Kconfig entries.
func (ctx *RuntimeContext) KConfigs() map[string][]*api.KConfigEntry {
	if ctx == nil || ctx.allKConfigs == nil {
		return nil
	}
	out := make(map[string][]*api.KConfigEntry, len(ctx.allKConfigs))
	for name, entries := range ctx.allKConfigs {
		out[name] = append([]*api.KConfigEntry(nil), entries...)
	}
	return out
}

// Configuration returns a defensive copy of the configuration the context
// currently holds. It is derived on read, so no second copy has to be refreshed
// after a phase mutates a field.
func (ctx *RuntimeContext) Configuration() Configuration {
	if ctx == nil {
		return Configuration{}
	}
	return cloneConfiguration(ctx.currentConfiguration())
}

func cloneConfiguration(in Configuration) Configuration {
	out := in
	out.AllOptions = cloneOptionMap(in.AllOptions)
	out.AllKConfigs = maps.Clone(in.AllKConfigs)
	out.ChipProfiles = maps.Clone(in.ChipProfiles)
	out.GlobalOptions = maps.Clone(in.GlobalOptions)
	out.DerivedOptions = cloneNestedAny(in.DerivedOptions)
	out.BufferedGlobalFlags = cloneBufferedFlags(in.BufferedGlobalFlags)
	out.GlobalMacroDefines = append([]string(nil), in.GlobalMacroDefines...)
	return out
}

func cloneOptionMap(in map[string]map[string]*api.Option) map[string]map[string]*api.Option {
	if in == nil {
		return nil
	}
	out := make(map[string]map[string]*api.Option, len(in))
	for pkg, opts := range in {
		out[pkg] = maps.Clone(opts)
	}
	return out
}

func cloneNestedAny(in map[string]map[string]any) map[string]map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]map[string]any, len(in))
	for k, v := range in {
		out[k] = maps.Clone(v)
	}
	return out
}

func cloneBufferedFlags(in map[string]*packageGlobalFlags) map[string]*packageGlobalFlags {
	if in == nil {
		return nil
	}
	out := make(map[string]*packageGlobalFlags, len(in))
	for k, v := range in {
		if v == nil {
			out[k] = nil
			continue
		}
		copy := *v
		copy.cFlags = append([]string(nil), v.cFlags...)
		copy.cxxFlags = append([]string(nil), v.cxxFlags...)
		copy.ldFlags = append([]string(nil), v.ldFlags...)
		copy.links = append([]string(nil), v.links...)
		copy.applyCFlags = append([]string(nil), v.applyCFlags...)
		copy.applyCxxFlags = append([]string(nil), v.applyCxxFlags...)
		copy.applyLdFlags = append([]string(nil), v.applyLdFlags...)
		copy.applyLinks = append([]string(nil), v.applyLinks...)
		out[k] = &copy
	}
	return out
}
