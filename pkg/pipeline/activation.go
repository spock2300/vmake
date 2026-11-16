package pipeline

import (
	"maps"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

// ActivationMode selects the effect level of a package activation.
type ActivationMode int

const (
	// ActivationBuild performs real OnBuild wiring: config lookup with bound
	// package values, real subgraph/dependency callbacks and config defines.
	ActivationBuild ActivationMode = iota
	// ActivationDeclarative performs dry-run OnBuild wiring used by inspect,
	// query, check-symbols, config check and deps graph.
	ActivationDeclarative
)

// ActivationOptions carries the parts that necessarily differ between the two
// activation paths. Configure runs after the shared wiring and before
// ExecBuildFuncs, so callers can install their config lookup and dependency
// callbacks.
type ActivationOptions struct {
	Mode      ActivationMode
	Configure func(*api.BuildContext)
}

// ActivatePackage is the single package-activation path for build and
// declarative phases. Callers must not repeat the SetDirs/SetOptions/SetCfgVals/
// SetToolchain/SetPlatform/SetPackage/ExecBuildFuncs sequence themselves.
func ActivatePackage(ctx *RuntimeContext, name string, dirs api.PkgDirs, tc *toolchain.Toolchain, values map[string]any, opts ActivationOptions) (*api.BuildContext, error) {
	buildCtx, err := newBuildContext(ctx, name, values)
	if err != nil {
		return nil, err
	}
	platform := platformFromValues(api.BindCfgVals(&buildCtx.ConfigAccessor))
	if opts.Mode == ActivationDeclarative {
		buildCtx.SetDryRun(true)
		api.BindSetBuildSubGraphFunc(buildCtx, func(string) error { return nil })
		api.BindSetDepOutputFunc(buildCtx, func(string) string { return "" })
		api.BindSetDepSrcDirFunc(buildCtx, func(string) string { return "" })
		api.BindSetDepPackageDirFunc(buildCtx, func(depRef string) string {
			validateDirectDepPackageRef(ctx, name, depRef)
			return ""
		})
	}

	var node = ctx.DepGraph.Packages[name]
	if node != nil && node.Pkg != nil {
		pkg := node.Pkg
		allOpts := ctx.allOptions[name]
		if allOpts == nil {
			allOpts = pkg.GetOptions()
		}
		pkg.SetDirs(dirs)
		pkg.SetOptions(mergedPackageAndGlobalOptions(allOpts, visibleGlobalOptionsFor(node, allOpts, ctx.globalOptions)))
		pkg.SetCfgVals(maps.Clone(api.BindCfgVals(&buildCtx.ConfigAccessor)))
		if tc != nil {
			tcName := resolvePkgToolchain(ctx.Config, name, tc.Name)
			if tcName != tc.Name {
				tc, err = toolchain.GetManager().GetToolchain(tcName)
				if err != nil {
					return nil, err
				}
			}
			pkg.SetToolchain(tc)
			pkg.SetPlatform(platform)
			flags := api.DefaultBuildFlags(tc.Name, pkg.TargetOS())
			buildCtx.SetDefaultFlags(flags.CFlags, flags.CxxFlags, flags.LdFlags)
		}
		api.BindSetPackage(buildCtx, pkg)
		if opts.Mode == ActivationDeclarative {
			pkg.SetDryRun(true)
			defer pkg.SetDryRun(false)
		}
		if opts.Configure != nil {
			opts.Configure(buildCtx)
		}
		api.BindPackageExecBuildFuncs(pkg, dirs.SourceDir, func(fn api.BuildFunc) {
			fn(buildCtx)
		})
		if opts.Mode == ActivationDeclarative {
			pkg.SetDryRun(false)
		}
		return buildCtx, nil
	}
	if opts.Configure != nil {
		opts.Configure(buildCtx)
	}
	return buildCtx, nil
}
