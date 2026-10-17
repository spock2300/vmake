package pipeline

import (
	"fmt"
	"maps"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func resolveBuildConfig(ctx *RuntimeContext) (*buildConfig, error) {
	if _, err := ProjectPlatform(ctx); err != nil {
		return nil, err
	}
	tc, tcName, err := GetToolchain(ctx.Config, ctx.ToolchainOverride)
	if err != nil {
		return nil, err
	}
	return makeBuildConfig(ctx, tc, tcName), nil
}

func makeBuildConfig(ctx *RuntimeContext, tc *toolchain.Toolchain, tcName string) *buildConfig {
	mode := ResolveMode(ctx.Config, ctx.ModeOverride)
	globalValues := projectGlobalValues(ctx)
	globalValues[api.ModeOptionName] = mode
	globalValues[api.ToolchainOptionName] = tcName
	platformValues := maps.Clone(globalValues)
	fillTargetDefaults(platformValues, tc.TargetOS, tc.TargetTriple)

	return &buildConfig{
		Mode:         mode,
		TcName:       tcName,
		Tc:           tc,
		Platform:     platformFromValues(platformValues),
		GlobalValues: globalValues,
	}
}

func filterAndCollectNeeded(ctx *RuntimeContext) (map[string]bool, error) {
	vlog.Info("")
	vlog.Info("Filtering dependencies...")
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.Pkg == nil {
			continue
		}
		entry := config.GetEntry(ctx.Config, name)
		opts := ctx.AllOptions[name]
		if err := ctx.Resolver.FilterDeps(name, entry.Options, opts); err != nil {
			return nil, fmt.Errorf("%s: filter deps: %w", name, err)
		} else if len(node.Deps) > 0 {
			vlog.Info("  %s: deps=%v", name, node.Deps)
		}
	}
	if err := ctx.Resolver.UpdateOrder(); err != nil {
		return nil, fmt.Errorf("dependency cycle: %w", err)
	}

	ctx.DepGraph.Freeze()

	needed, err := computeReachable(ctx.DepGraph)
	if err != nil {
		return nil, err
	}

	return needed, nil
}

func (r *runContext) toolsForPackage(name string) (*build.ResolvedTools, error) {
	tcName := resolvePkgToolchain(r.ctx.Config, name, r.cfg.TcName)
	tc := r.cfg.Tc
	if tcName != r.cfg.TcName {
		var err error
		tc, err = toolchain.GetManager().SelectToolchain(tcName)
		if err != nil {
			return nil, err
		}
	}
	platform, err := PackagePlatform(r.ctx, name)
	if err != nil {
		return nil, err
	}
	return r.session.ResolveTools(tc, platform)
}
