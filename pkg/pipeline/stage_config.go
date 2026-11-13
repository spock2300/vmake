package pipeline

import (
	"errors"
	"fmt"
	"maps"
	"sort"

	"github.com/spock2300/vmake/internal/pkgvis"
	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func resolveBuildConfig(ctx *RuntimeContext, validate bool) (*buildConfig, error) {
	if _, err := ProjectPlatform(ctx); err != nil {
		return nil, err
	}
	if !validate {
		tc, tcName, err := GetToolchain(ctx.Config, ctx.ToolchainOverride, ctx.PreferredToolchain)
		if err != nil {
			return nil, err
		}
		return makeBuildConfig(ctx, tc, tcName), nil
	}
	tcName := ResolveToolchainName(ctx.Config, ctx.ToolchainOverride, ctx.PreferredToolchain)
	tc, err := toolchain.GetManager().GetToolchain(tcName)
	if err != nil {
		return nil, fmt.Errorf("%w; run 'vmake build --toolchain %s' first", err, tcName)
	}
	if errs := toolchain.ValidateToolchain(tc); len(errs) > 0 {
		return nil, fmt.Errorf("invalid toolchain %q: %w; run 'vmake build --toolchain %s' first", tcName, errors.Join(errs...), tcName)
	}
	return makeBuildConfig(ctx, tc, tcName), nil
}

func makeBuildConfig(ctx *RuntimeContext, tc *toolchain.Toolchain, tcName string) *buildConfig {
	mode := ResolveMode(ctx.Config, ctx.ModeOverride)
	globalValues := projectGlobalValues(ctx)
	globalValues[api.ModeOptionName] = mode
	globalValues[api.ToolchainOptionName] = tcName
	platformValues := maps.Clone(globalValues)
	if ctx.ChipProfile.Name != "" && chipTargetsApply(ctx.ChipProfile, tcName) {
		fillTargetDefaults(platformValues, ctx.ChipProfile.TargetOS, ctx.ChipProfile.TargetTriple)
	}
	fillTargetDefaults(platformValues, tc.TargetOS, tc.TargetTriple)

	return &buildConfig{
		Mode:         mode,
		TcName:       tcName,
		Tc:           tc,
		Platform:     platformFromValues(platformValues),
		GlobalValues: globalValues,
	}
}

func resolveNeeded(ctx *RuntimeContext, logFilter, freeze, validate bool) (map[string]bool, error) {
	if logFilter {
		vlog.Info("")
		vlog.Info("Filtering dependencies...")
	}
	rootOverride := configRootOverride(ctx)
	if ctx != nil && ctx.DepGraph != nil && ctx.Resolver != nil && !ctx.DepGraph.IsFrozen() {
		if err := convergeFilterAndDerivedOptions(ctx, rootOverride, logFilter); err != nil {
			return nil, err
		}
	}

	if freeze && ctx != nil && ctx.DepGraph != nil {
		ctx.DepGraph.Freeze()
	}

	needed, err := computeReachable(ctx.DepGraph, rootOverride)
	if err != nil {
		return nil, err
	}
	if validate {
		if err := validateNeededSubPackageOwners(needed, ctx.Resolver.SubParents()); err != nil {
			return nil, err
		}
	}

	return needed, nil
}

func filterDependencies(ctx *RuntimeContext, log bool) error {
	if ctx == nil || ctx.DepGraph == nil || ctx.Resolver == nil || ctx.DepGraph.IsFrozen() {
		return nil
	}
	globalValues := projectGlobalValues(ctx)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Pkg == nil {
			continue
		}
		var entryOpts map[string]any
		if ctx.Config != nil {
			entryOpts = config.GetEntry(ctx.Config, name).Options
		}
		options := mergeCfgVals(name, node, ctx, globalValues, map[string]map[string]any{
			name: entryOpts,
		})
		pkgOpts := ctx.AllOptions[name]
		opts := mergedPackageAndGlobalOptions(pkgOpts, visibleGlobalOptionsFor(node, pkgOpts, ctx.GlobalOptions))
		if err := ctx.Resolver.FilterDeps(name, options, opts); err != nil {
			return fmt.Errorf("%s: filter deps: %w", name, err)
		}
		if log && len(node.Deps) > 0 {
			vlog.Verbose("  %s: deps=%v", name, node.Deps)
		}
	}
	if err := ctx.Resolver.UpdateOrder(); err != nil {
		return fmt.Errorf("dependency cycle: %w", err)
	}
	return nil
}

func convergeFilterAndDerivedOptions(ctx *RuntimeContext, rootOverride string, log bool) (err error) {
	defer scriptcall.Recover(&err)
	if ctx == nil || ctx.DepGraph == nil || ctx.Resolver == nil {
		return nil
	}
	ctx.DerivedOptions = make(map[string]map[string]any)
	derivedOwners := make(map[string]map[string]string)
	globalValues := projectGlobalValues(ctx)
	pkgDirs := ResolveAllPackageDirs(ctx.DepGraph)
	subParents := ctx.Resolver.SubParents()

	roots, err := computeRootSeeds(ctx.DepGraph, rootOverride)
	if err != nil {
		return err
	}
	if len(roots) == 0 {
		return ctx.Resolver.UpdateOrder()
	}

	reachable := make(map[string]bool, len(ctx.DepGraph.Packages))
	queue := append([]string(nil), roots...)
	for _, r := range roots {
		reachable[r] = true
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if node := ctx.DepGraph.Packages[cur]; node != nil {
			for _, dep := range node.Deps {
				if !reachable[dep] {
					reachable[dep] = true
					queue = append(queue, dep)
				}
			}
		}
	}

	inDegree := make(map[string]int, len(reachable))
	for name := range reachable {
		inDegree[name] = 0
	}
	for name := range reachable {
		if node := ctx.DepGraph.Packages[name]; node != nil {
			for _, dep := range node.Deps {
				if reachable[dep] {
					inDegree[dep]++
				}
			}
		}
	}

	processed := make(map[string]bool, len(reachable))
	for len(processed) < len(reachable) {
		var ready []string
		for name := range reachable {
			if !processed[name] && inDegree[name] == 0 {
				ready = append(ready, name)
			}
		}
		if len(ready) == 0 {
			break
		}
		sort.Strings(ready)

		for _, name := range ready {
			processed[name] = true
			node := ctx.DepGraph.Packages[name]
			oldDeps := append([]string(nil), node.Deps...)

			if node != nil && node.Pkg != nil {
				var entryOpts map[string]any
				if ctx.Config != nil {
					entryOpts = config.GetEntry(ctx.Config, name).Options
				}
				options := mergeCfgVals(name, node, ctx, globalValues, map[string]map[string]any{
					name: entryOpts,
				})
				pkgOpts := ctx.AllOptions[name]
				opts := mergedPackageAndGlobalOptions(pkgOpts, visibleGlobalOptionsFor(node, pkgOpts, ctx.GlobalOptions))
				if err := ctx.Resolver.FilterDeps(name, options, opts); err != nil {
					return fmt.Errorf("%s: filter deps: %w", name, err)
				}
				if log && len(node.Deps) > 0 {
					vlog.Verbose("  %s: deps=%v", name, node.Deps)
				}
			}

			for _, oldDep := range oldDeps {
				if reachable[oldDep] {
					inDegree[oldDep]--
				}
			}

			stillReachable, err := computeReachable(ctx.DepGraph, rootOverride)
			if err != nil {
				return err
			}
			for cand := range reachable {
				if !stillReachable[cand] && !processed[cand] {
					delete(reachable, cand)
					delete(inDegree, cand)
					if cNode := ctx.DepGraph.Packages[cand]; cNode != nil {
						for _, cDep := range cNode.Deps {
							if reachable[cDep] {
								inDegree[cDep]--
							}
						}
					}
				}
			}

			if node != nil && node.Pkg != nil && node.Pkg.HasConfigureDepsFuncs() {
				if err := applyNodeConfigureDeps(ctx, name, node, globalValues, pkgDirs, subParents, stillReachable, derivedOwners); err != nil {
					return err
				}
			}
		}
	}

	if err := ctx.Resolver.UpdateOrder(); err != nil {
		return fmt.Errorf("dependency cycle: %w", err)
	}
	return nil
}

func applyNodeConfigureDeps(
	ctx *RuntimeContext,
	name string,
	node *resolver.PackageNode,
	globalValues map[string]any,
	pkgDirs map[string]*api.PkgDirs,
	subParents map[string]string,
	needed map[string]bool,
	derivedOwners map[string]map[string]string,
) error {
	vals, valErr := PackageConfigValues(ctx, name, globalValues)
	if valErr != nil {
		return valErr
	}
	pkgOpts := ctx.AllOptions[name]
	opts := mergedPackageAndGlobalOptions(pkgOpts, visibleGlobalOptionsFor(node, pkgOpts, ctx.GlobalOptions))
	srcDir := pkgDirs[name].SourceDir
	resolveTarget := func(dep string) (string, map[string]*api.Option, error) {
		targetPkg := api.ResolveSubPackageName(name, dep, subParents, func(candidate string) bool {
			return (needed == nil || needed[candidate]) && ctx.DepGraph.Packages[candidate] != nil
		})
		if ctx.DepGraph.Packages[targetPkg] == nil || (needed != nil && !needed[targetPkg]) {
			return "", nil, fmt.Errorf("%s: OnConfigureDeps target package %q is not an active dependency", name, dep)
		}
		if targetPkg == name || !canReachDependency(ctx.DepGraph, name, targetPkg) {
			return "", nil, fmt.Errorf("%s: OnConfigureDeps cannot configure %q because %s does not depend on it", name, targetPkg, name)
		}
		if err := pkgvis.CheckInternal(name, ctx.Resolver.VisibilityPath(name), targetPkg, ctx.Resolver.VisibilityPath(targetPkg)); err != nil {
			return "", nil, err
		}
		targetOpts := ctx.AllOptions[targetPkg]
		if targetOpts == nil {
			if tNode := ctx.DepGraph.Packages[targetPkg]; tNode != nil && tNode.Pkg != nil {
				targetOpts = tNode.Pkg.GetOptions()
			}
		}
		return targetPkg, targetOpts, nil
	}
	depCtx := api.NewDepConfigContext(name, srcDir, vals, opts)
	depCtx.SetConfigLookup(newConfigLookup(ctx, name, globalValues, nil))
	depCtx.SetOptionExists(func(depPkg, option string) (bool, error) {
		_, targetOpts, err := resolveTarget(depPkg)
		if err != nil {
			return false, err
		}
		opt := targetOpts[option]
		return opt != nil && !opt.IsGlobal(), nil
	})
	node.Pkg.ExecConfigureDepsFuncs(srcDir, func(fn api.ConfigureDepsFunc) {
		fn(depCtx)
	})
	for _, def := range depCtx.Defaults() {
		targetPkg, targetOpts, err := resolveTarget(def.Dep)
		if err != nil {
			return err
		}
		opt := targetOpts[def.Option]
		if opt == nil {
			return fmt.Errorf("%s: OnConfigureDeps option %q not found in package %q", name, def.Option, targetPkg)
		}
		if opt.IsGlobal() {
			return fmt.Errorf("%s: OnConfigureDeps cannot set global option %q on %q", name, def.Option, targetPkg)
		}
		normVal := api.NormalizeOptionValue(opt, def.Value)
		if err := api.ValidateOptionValue(opt, normVal); err != nil {
			return fmt.Errorf("%s: OnConfigureDeps %s/%s: %w", name, targetPkg, def.Option, err)
		}
		if prevPkg, hasPrev := derivedOwners[targetPkg][def.Option]; hasPrev && prevPkg != name {
			prevVal := ctx.DerivedOptions[targetPkg][def.Option]
			if !api.ValuesEqual(prevVal, normVal) {
				return fmt.Errorf("conflicting OnConfigureDeps default for %s/%s: %s sets %v, %s sets %v", targetPkg, def.Option, prevPkg, prevVal, name, normVal)
			}
		}
		if ctx.DerivedOptions[targetPkg] == nil {
			ctx.DerivedOptions[targetPkg] = make(map[string]any)
			derivedOwners[targetPkg] = make(map[string]string)
		}
		ctx.DerivedOptions[targetPkg][def.Option] = normVal
		derivedOwners[targetPkg][def.Option] = name
	}
	return nil
}

func mergedPackageAndGlobalOptions(pkgOpts, globalOpts map[string]*api.Option) map[string]*api.Option {
	if len(globalOpts) == 0 {
		return pkgOpts
	}
	merged := make(map[string]*api.Option, len(globalOpts)+len(pkgOpts))
	maps.Copy(merged, globalOpts)
	for k, opt := range pkgOpts {
		if opt != nil && opt.IsGlobal() {
			if _, ok := globalOpts[k]; ok {
				continue
			}
		}
		merged[k] = opt
	}
	return merged
}

func validateNeededSubPackageOwners(needed map[string]bool, subParents map[string]string) error {
	names := make([]string, 0, len(needed))
	for name := range needed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		parent := subParents[name]
		if parent != "" && !needed[parent] {
			return fmt.Errorf("sub-package %s is required but its native parent %s is not; list %s before %s in AddRequires", name, parent, parent, name)
		}
	}
	return nil
}

func (r *runContext) toolsForPackage(name string) (*build.ResolvedTools, error) {
	if r.cfg == nil {
		return nil, nil
	}
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

func canReachDependency(graph *resolver.Graph, from, target string) bool {
	if graph == nil || from == "" || target == "" || from == target {
		return false
	}
	visited := map[string]bool{from: true}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		node := graph.Packages[cur]
		if node == nil {
			continue
		}
		for _, dep := range node.Deps {
			if dep == target {
				return true
			}
			if !visited[dep] {
				visited[dep] = true
				queue = append(queue, dep)
			}
		}
	}
	return false
}

func resolveDerivedOptions(ctx *RuntimeContext, needed map[string]bool) (err error) {
	defer scriptcall.Recover(&err)
	if ctx == nil || ctx.DepGraph == nil || ctx.Resolver == nil {
		return nil
	}
	ctx.DerivedOptions = make(map[string]map[string]any)
	derivedOwners := make(map[string]map[string]string)
	globalValues := projectGlobalValues(ctx)
	pkgDirs := ResolveAllPackageDirs(ctx.DepGraph)
	subParents := ctx.Resolver.SubParents()

	order := ctx.Resolver.GetOrder()
	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		if needed != nil && !needed[name] {
			continue
		}
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Pkg == nil || !node.Pkg.HasConfigureDepsFuncs() {
			continue
		}
		if err := applyNodeConfigureDeps(ctx, name, node, globalValues, pkgDirs, subParents, needed, derivedOwners); err != nil {
			return err
		}
	}
	return nil
}

func resolvePreviewDerivedOptions(ctx *RuntimeContext) {
	if ctx == nil || ctx.DepGraph == nil || ctx.Resolver == nil {
		return
	}
	needed, err := computeReachable(ctx.DepGraph, configRootOverride(ctx))
	if err != nil {
		return
	}
	_ = resolveDerivedOptions(ctx, needed)
}
