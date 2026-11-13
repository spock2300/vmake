package pipeline

import (
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type preludeOptions struct {
	filterDeps        bool
	freezeDeps        bool
	logFilter         bool
	validateNeeded    bool
	resolveChip       bool
	resolveConfig     bool
	validateToolchain bool
	resolveTools      bool
}

type buildPrelude struct {
	cfg             *buildConfig
	tools           *build.ResolvedTools
	needed          map[string]bool
	rawGlobalFlags  [4][]string
	effectiveFlags  [4][]string
	globalFlagsHash string
}

func prepareBuild(ctx *RuntimeContext, opts preludeOptions) (*buildPrelude, error) {
	var needed map[string]bool
	var err error
	if opts.filterDeps {
		needed, err = resolveNeeded(ctx, opts.logFilter, opts.freezeDeps, opts.validateNeeded)
	} else {
		needed, err = computeReachable(ctx.DepGraph, configRootOverride(ctx))
	}
	if err != nil {
		return nil, err
	}

	pre := &buildPrelude{needed: needed}
	if opts.filterDeps {
		if err := mergeGlobalOptionsFiltered(ctx, needed); err != nil {
			return nil, err
		}
		extendChipChoice(ctx, needed)
		if err := ValidateConfigValues(ctx, needed); err != nil {
			return nil, err
		}
		if err := resolveDerivedOptions(ctx, needed); err != nil {
			return nil, err
		}
		applyConfigCallbacksForNeeded(ctx, needed, false)
		if err := updateGlobalMacroDefines(ctx); err != nil {
			return nil, err
		}
	}
	if opts.resolveChip {
		if opts.filterDeps {
			if err := resolveActiveChip(ctx, needed); err != nil {
				return nil, err
			}
		} else {
			ResolveActiveChipForDisplay(ctx)
		}
	}
	if opts.resolveConfig {
		cfg, err := resolveBuildConfig(ctx, opts.validateToolchain)
		if err != nil {
			return nil, err
		}
		pre.cfg = cfg
		if opts.resolveTools {
			tools, err := build.ResolveTools(cfg.Tc, cfg.Platform)
			if err != nil {
				return nil, err
			}
			pre.tools = tools
		}
	}
	pre.rawGlobalFlags, pre.effectiveFlags, pre.globalFlagsHash = aggregateGlobalFlags(ctx, needed)
	return pre, nil
}

func neededGlobalFlags(ctx *RuntimeContext, needed map[string]bool) (cflags, cxxflags, ldflags, links []string) {
	for _, name := range ctx.Resolver.GetOrder() {
		if !needed[name] {
			continue
		}
		buf := ctx.BufferedGlobalFlags[name]
		if buf == nil {
			continue
		}
		cflags = append(cflags, buf.allCFlags()...)
		cxxflags = append(cxxflags, buf.allCxxFlags()...)
		ldflags = append(ldflags, buf.allLdFlags()...)
		links = append(links, buf.allLinks()...)
	}
	return cflags, cxxflags, ldflags, links
}

func aggregateGlobalFlags(ctx *RuntimeContext, needed map[string]bool) (raw, effective [4][]string, hash string) {
	raw = [4][]string{}
	raw[0], raw[1], raw[2], raw[3] = neededGlobalFlags(ctx, needed)

	effective = [4][]string{
		append([]string(nil), raw[0]...),
		append([]string(nil), raw[1]...),
		append([]string(nil), raw[2]...),
		append([]string(nil), raw[3]...),
	}
	for _, define := range ctx.GlobalMacroDefines {
		effective[0] = append(effective[0], "-D"+define)
		effective[1] = append(effective[1], "-D"+define)
	}

	mgr := toolchain.GetManager()
	mgr.SetProjectFlags(effective[0], effective[1], effective[2], effective[3])
	effective = [4][]string{
		mgr.GetGlobalCFlags(),
		mgr.GetGlobalCxxFlags(),
		mgr.GetGlobalLdFlags(),
		mgr.GetGlobalLinks(),
	}
	return raw, effective, build.GlobalFlagsHash()
}
