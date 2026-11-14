package pipeline

import (
	"maps"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/resolver"
)

// Invocation is the immutable command/project input. It is currently the same
// shape as ResolveParams; the alias keeps callers stable while the phase result
// types below make ownership explicit.
type Invocation = ResolveParams

// Resolution is the output of Require: the resolver and its final graph.
type Resolution struct {
	Resolver *resolver.Resolver
	DepGraph *resolver.Graph
}

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

// Preparation is the output of prepareBuild: the needed set and the effective
// global flags that the build/inspect paths share.
type Preparation struct {
	Needed          map[string]bool
	RawFlags        [4][]string
	EffectiveFlags  [4][]string
	GlobalFlagsHash string
}

func (ctx *RuntimeContext) applyResolution(result Resolution) {
	ctx.resolution = result
	ctx.Resolver = result.Resolver
	ctx.DepGraph = result.DepGraph
}

func (ctx *RuntimeContext) currentConfiguration() Configuration {
	return Configuration{
		AllOptions:          ctx.AllOptions,
		AllKConfigs:         ctx.AllKConfigs,
		ChipProfiles:        ctx.ChipProfiles,
		ChipProfile:         ctx.ChipProfile,
		GlobalOptions:       ctx.GlobalOptions,
		DerivedOptions:      ctx.DerivedOptions,
		BufferedGlobalFlags: ctx.BufferedGlobalFlags,
		GlobalMacroDefines:  ctx.GlobalMacroDefines,
	}
}

func (ctx *RuntimeContext) applyConfiguration(result Configuration) {
	ctx.configuration = cloneConfiguration(result)
	ctx.AllOptions = result.AllOptions
	ctx.AllKConfigs = result.AllKConfigs
	ctx.ChipProfiles = result.ChipProfiles
	ctx.ChipProfile = result.ChipProfile
	ctx.GlobalOptions = result.GlobalOptions
	ctx.DerivedOptions = result.DerivedOptions
	ctx.BufferedGlobalFlags = result.BufferedGlobalFlags
	ctx.GlobalMacroDefines = result.GlobalMacroDefines
}

func (ctx *RuntimeContext) applyPreparation(result Preparation) {
	ctx.preparation = clonePreparation(result)
}

func (ctx *RuntimeContext) snapshotConfiguration() {
	ctx.configuration = cloneConfiguration(ctx.currentConfiguration())
}

// Resolution returns a shallow copy of the last Require result.
func (ctx *RuntimeContext) Resolution() Resolution {
	if ctx == nil {
		return Resolution{}
	}
	return ctx.resolution
}

// Configuration returns a copy of the last Configure result so callers cannot
// mutate the facade's owned state through the accessor.
func (ctx *RuntimeContext) Configuration() Configuration {
	if ctx == nil {
		return Configuration{}
	}
	return cloneConfiguration(ctx.configuration)
}

// Preparation returns a copy of the last prepareBuild result.
func (ctx *RuntimeContext) Preparation() Preparation {
	if ctx == nil {
		return Preparation{}
	}
	return clonePreparation(ctx.preparation)
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

func clonePreparation(in Preparation) Preparation {
	out := in
	out.Needed = maps.Clone(in.Needed)
	for i := range out.RawFlags {
		out.RawFlags[i] = append([]string(nil), in.RawFlags[i]...)
		out.EffectiveFlags[i] = append([]string(nil), in.EffectiveFlags[i]...)
	}
	return out
}
