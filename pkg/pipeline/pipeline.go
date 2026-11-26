package pipeline

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/pkgvis"
	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type Paths struct {
	ProjectDir string
	DepsDir    string
	CacheDir   string
	ReposDir   string
	LockPath   string
}

type RuntimeContext struct {
	Context             context.Context
	Locks               *storage.Session
	WorkDir             string
	ConfigPath          string
	ConfigDigest        string
	Config              *config.ConfigFile
	DepGraph            *resolver.Graph
	allOptions          map[string]map[string]*api.Option
	allKConfigs         map[string][]*api.KConfigEntry
	chipProfiles        map[string]api.ChipProfile
	chipProfile         api.ChipProfile
	PreferredToolchain  string
	globalOptions       map[string]*api.Option
	derivedOptions      map[string]map[string]any
	Resolver            *resolver.Resolver
	bufferedGlobalFlags map[string]*packageGlobalFlags
	globalMacroDefines  []string
	Lock                *lockfile.Lock
	LockPath            string
	LockPolicy          resolver.LockPolicy
	Pins                map[string]string
	SourceLinks         map[string]string

	Paths              *Paths
	TrustChecker       buildscript.ScriptTrustChecker
	ModeOverride       string
	ToolchainOverride  string
	OnOptionsCollected func(*RuntimeContext) error
}

type packageGlobalFlags struct {
	cFlags        []string
	cxxFlags      []string
	ldFlags       []string
	links         []string
	applyCFlags   []string
	applyCxxFlags []string
	applyLdFlags  []string
	applyLinks    []string
}

func (f *packageGlobalFlags) resetApply() {
	if f == nil {
		return
	}
	f.applyCFlags = nil
	f.applyCxxFlags = nil
	f.applyLdFlags = nil
	f.applyLinks = nil
}

func (f *packageGlobalFlags) allCFlags() []string {
	if f == nil {
		return nil
	}
	return append(append([]string(nil), f.cFlags...), f.applyCFlags...)
}

func (f *packageGlobalFlags) allCxxFlags() []string {
	if f == nil {
		return nil
	}
	return append(append([]string(nil), f.cxxFlags...), f.applyCxxFlags...)
}

func (f *packageGlobalFlags) allLdFlags() []string {
	if f == nil {
		return nil
	}
	return append(append([]string(nil), f.ldFlags...), f.applyLdFlags...)
}

func (f *packageGlobalFlags) allLinks() []string {
	if f == nil {
		return nil
	}
	return append(append([]string(nil), f.links...), f.applyLinks...)
}

type ResolveParams struct {
	WorkDir           string
	ConfigPath        string
	ConfigDigest      string
	Config            *config.ConfigFile
	Lock              *lockfile.Lock
	LockPath          string
	IgnoreLock        bool
	LockPolicy        resolver.LockPolicy
	Pins              map[string]string
	SourceLinks       map[string]string
	TrustChecker      buildscript.ScriptTrustChecker
	Paths             *Paths
	ModeOverride      string
	ToolchainOverride string
}

func NewContext(p ResolveParams) *RuntimeContext {
	ctx := &RuntimeContext{
		WorkDir:           p.WorkDir,
		ConfigPath:        p.ConfigPath,
		ConfigDigest:      p.ConfigDigest,
		Config:            p.Config,
		Lock:              p.Lock,
		LockPath:          p.LockPath,
		LockPolicy:        p.LockPolicy,
		Pins:              p.Pins,
		SourceLinks:       p.SourceLinks,
		Paths:             p.Paths,
		TrustChecker:      p.TrustChecker,
		ModeOverride:      p.ModeOverride,
		ToolchainOverride: p.ToolchainOverride,
	}
	if p.IgnoreLock {
		ctx.LockPolicy.IgnoreAll = true
	}
	return ctx
}

func (ctx *RuntimeContext) effectiveLockPolicy() resolver.LockPolicy {
	if ctx == nil {
		return resolver.LockPolicy{}
	}
	return ctx.LockPolicy
}

func (ctx *RuntimeContext) lockQuery() resolver.LockQuery {
	if ctx == nil {
		return resolver.LockQuery{}
	}
	return resolver.LockQuery{Policy: ctx.effectiveLockPolicy(), Pins: ctx.Pins, Lock: ctx.Lock}
}

func (ctx *RuntimeContext) isLockIgnoredFor(name string) bool {
	return ctx.lockQuery().IgnoredFor(name)
}

func canonicalPkgRef(ctx *RuntimeContext, fromPkg, rawRef string) string {
	if rawRef == "" {
		return ""
	}
	container := api.LocalContainerID
	if ctx != nil && ctx.DepGraph != nil {
		if node := ctx.DepGraph.Packages[fromPkg]; node != nil && node.Container != "" {
			container = node.Container
		}
		return ctx.DepGraph.CanonicalPackageRef(container, rawRef)
	}
	return api.CanonicalizePackageRef(container, rawRef)
}

func checkPackageVisibility(ctx *RuntimeContext, fromPkg, toPkg string) error {
	if ctx == nil || fromPkg == "" || toPkg == "" || fromPkg == toPkg {
		return nil
	}
	if ctx.Resolver != nil {
		fromVis := ctx.Resolver.VisibilityPath(fromPkg)
		if fromPkg == "@local" {
			fromVis = "@local"
		}
		if err := pkgvis.CheckInternal(fromPkg, fromVis, toPkg, ctx.Resolver.VisibilityPath(toPkg)); err != nil {
			return err
		}
	}
	if ctx.DepGraph != nil {
		if err := ctx.DepGraph.CheckExportVisibility(fromPkg, toPkg); err != nil {
			return err
		}
	}
	return nil
}

func pkgBuildMetaFor(ctx *RuntimeContext, node *resolver.PackageNode, visPath string, unmaterialized bool) build.PkgBuildMeta {
	meta := build.PkgBuildMeta{
		Origin:         node.Source.Origin,
		Container:      node.Container,
		Linked:         node.Source.Link != nil,
		Deps:           append([]string(nil), node.Deps...),
		VisPath:        visPath,
		Unmaterialized: unmaterialized,
		Exported:       node.Exported,
	}
	if ctx != nil && ctx.DepGraph != nil && node.Container != "" {
		if c := ctx.DepGraph.Containers[node.Container]; c != nil && !c.IsLocal() && c.Manifest != nil && c.Manifest.HasExports() {
			meta.ContainerHasExports = true
			meta.InternalRefMap = c.Manifest.InternalRefMap(c.ID)
		}
	}
	return meta
}

func visibleGlobalOptionsFor(node *resolver.PackageNode, pkgOpts, globalOptions map[string]*api.Option) map[string]*api.Option {
	if len(globalOptions) == 0 {
		return nil
	}
	isLocal := node != nil && node.IsLocal()
	visible := make(map[string]*api.Option, len(globalOptions))
	for name, opt := range globalOptions {
		if opt == nil {
			continue
		}
		if opt.ExportDefine() || isLocal || (pkgOpts[name] != nil && pkgOpts[name].IsGlobal()) {
			visible[name] = opt
		}
	}
	return visible
}

func visibleGlobalValuesFor(node *resolver.PackageNode, pkgOpts, globalOptions map[string]*api.Option, globalValues map[string]any) map[string]any {
	if len(globalValues) == 0 {
		return nil
	}
	isLocal := node != nil && node.IsLocal()
	visible := make(map[string]any, len(globalValues))
	for name, val := range globalValues {
		opt := globalOptions[name]
		if opt == nil || opt.ExportDefine() || isLocal || (pkgOpts[name] != nil && pkgOpts[name].IsGlobal()) {
			visible[name] = val
		}
	}
	return visible
}

func newBuildContext(ctx *RuntimeContext, name string, globalValues map[string]any) (*api.BuildContext, error) {
	values, err := PackageConfigValues(ctx, name, globalValues)
	if err != nil {
		return nil, err
	}
	var node *resolver.PackageNode
	if ctx.DepGraph != nil {
		node = ctx.DepGraph.Packages[name]
	}
	buildCtx := api.NewBuildContext(name, values)
	pkgOpts := ctx.allOptions[name]
	if pkgOpts != nil {
		buildCtx.SetOptions(maps.Clone(pkgOpts))
	}
	buildCtx.MergeGlobals(
		visibleGlobalOptionsFor(node, pkgOpts, ctx.globalOptions),
		visibleGlobalValuesFor(node, pkgOpts, ctx.globalOptions, globalValues),
	)
	return buildCtx, nil
}

func newConfigLookup(ctx *RuntimeContext, currentPkg string, base map[string]any, bound func(string) (map[string]any, bool)) func(string, string) (any, *api.Option, error) {
	if base == nil && ctx.Config != nil {
		base = ProjectGlobalValues(ctx)
	}
	values := make(map[string]map[string]any)
	return func(pkg, name string) (any, *api.Option, error) {
		if ctx.DepGraph == nil {
			return nil, nil, fmt.Errorf("package %q is not in the resolved dependency graph", pkg)
		}
		if currentPkg != "" {
			pkg = canonicalPkgRef(ctx, currentPkg, pkg)
		}
		if ctx.DepGraph.Packages[pkg] == nil {
			return nil, nil, fmt.Errorf("package %q is not in the resolved dependency graph", pkg)
		}
		if currentPkg != "" {
			if err := checkPackageVisibility(ctx, currentPkg, pkg); err != nil {
				return nil, nil, err
			}
		}
		node := ctx.DepGraph.Packages[pkg]
		opts := ctx.allOptions[pkg]
		if opts == nil && node.Pkg != nil {
			opts = node.Pkg.GetOptions()
		}
		opt := opts[name]
		if opt != nil && opt.IsInternal() && currentPkg != "" && packageContainerID(ctx, currentPkg) != packageContainerID(ctx, pkg) {
			return nil, nil, fmt.Errorf("option %q in package %q is internal to container %q and cannot be read from %q", name, pkg, packageContainerID(ctx, pkg), currentPkg)
		}
		if opt == nil {
			if gOpt := ctx.globalOptions[name]; gOpt != nil && gOpt.ExportDefine() {
				opt = gOpt
			}
		}
		if opt == nil {
			opt = implicitPlatformOption(name)
		}
		if opt == nil {
			crossContainer := currentPkg != "" && packageContainerID(ctx, currentPkg) != packageContainerID(ctx, pkg)
			names := make([]string, 0, len(opts)+len(ctx.globalOptions))
			for optionName, candidate := range opts {
				if crossContainer && candidate != nil && candidate.IsInternal() {
					continue
				}
				names = append(names, optionName)
			}
			for optionName, gOpt := range ctx.globalOptions {
				if gOpt != nil && !gOpt.ExportDefine() {
					continue
				}
				if _, exists := opts[optionName]; !exists {
					names = append(names, optionName)
				}
			}
			sort.Strings(names)
			if len(names) == 0 {
				return nil, nil, fmt.Errorf("option %q not found in package %q", name, pkg)
			}
			return nil, nil, fmt.Errorf("option %q not found in package %q (available: %s)", name, pkg, strings.Join(names, ", "))
		}
		if bound != nil {
			if resolved, ok := bound(pkg); ok {
				return resolved[name], opt, nil
			}
		}
		if ctx.Config == nil {
			return base[name], opt, nil
		}
		if resolved, ok := values[pkg]; ok {
			return resolved[name], opt, nil
		}
		resolved, err := PackageConfigValues(ctx, pkg, base)
		if err != nil {
			return nil, nil, err
		}
		values[pkg] = resolved
		return resolved[name], opt, nil
	}
}

func implicitPlatformOption(name string) *api.Option {
	switch name {
	case api.TargetOSOptionName, api.TargetTripleOptionName:
		return (&api.Option{}).SetType(api.OptionString)
	}
	return nil
}

func PackageConfigValues(ctx *RuntimeContext, name string, globalValues map[string]any) (map[string]any, error) {
	if _, err := checkedPlatform(globalValues, "project"); err != nil {
		return nil, err
	}
	if _, err := checkedPlatform(packagePlatformValues(ctx, name, globalValues), "package "+name); err != nil {
		return nil, err
	}
	var node *resolver.PackageNode
	if ctx.DepGraph != nil {
		node = ctx.DepGraph.Packages[name]
	}
	return mergeCfgVals(name, node, ctx, globalValues, config.GetEntry(ctx.Config, name).Options), nil
}

func mergeCfgVals(name string, node *resolver.PackageNode, ctx *RuntimeContext, globalValues map[string]any, pkgOptions map[string]any) map[string]any {
	cfgVals := make(map[string]any)
	allOpts := ctx.allOptions[name]
	if allOpts == nil && node != nil && node.Pkg != nil {
		allOpts = node.Pkg.GetOptions()
	}
	visGlobals := visibleGlobalValuesFor(node, allOpts, ctx.globalOptions, globalValues)
	if allOpts != nil {
		for optName, opt := range allOpts {
			if _, resolved := visGlobals[optName]; resolved && opt.IsGlobal() {
				continue
			}
			if opt.Default() != nil {
				cfgVals[optName] = opt.Default()
			}
		}
	}
	if ctx != nil && ctx.derivedOptions != nil {
		for k, v := range ctx.derivedOptions[name] {
			if opt, declared := allOpts[k]; declared && opt.IsGlobal() {
				continue
			}
			cfgVals[k] = v
		}
	}
	mergePkgOptions(cfgVals, pkgOptions, allOpts, visGlobals)
	for k, v := range visGlobals {
		if _, exists := cfgVals[k]; !exists {
			cfgVals[k] = v
		}
	}
	maps.Copy(cfgVals, packagePlatformValues(ctx, name, globalValues))
	return cfgVals
}

func mergePkgOptions(dst, pkgOpts map[string]any, allOpts map[string]*api.Option, globalValues map[string]any) {
	for k, v := range pkgOpts {
		if opt, declared := allOpts[k]; declared && opt.IsGlobal() {
			if _, hasGlobal := globalValues[k]; hasGlobal && api.NormalizeOptionValue(opt, v) == api.NormalizeOptionValue(opt, opt.Default()) {
				continue
			}
		}
		dst[k] = v
	}
}

func applyBuildContextConfig(buildCtx *api.BuildContext, node *resolver.PackageNode, ctx *RuntimeContext, currentPkg string, needed NeededSet) {
	var resolvedImports []string
	for _, depName := range buildCtx.ImportConfigs() {
		resolved := canonicalPkgRef(ctx, currentPkg, depName)
		if currentPkg != "" {
			if err := checkPackageVisibility(ctx, currentPkg, resolved); err != nil {
				panic(&api.BuildScriptError{Package: currentPkg, Op: "ImportConfig", Err: err})
			}
		}
		resolvedImports = append(resolvedImports, resolved)
	}
	if buildCtx.GenConfigDefines() && node.Pkg != nil {
		var importPkgs []*api.Package
		pkgContainer := make(map[*api.Package]string, len(resolvedImports))
		for _, resolved := range resolvedImports {
			depNode := ctx.DepGraph.Packages[resolved]
			if depNode != nil && depNode.Pkg != nil {
				importPkgs = append(importPkgs, depNode.Pkg)
				pkgContainer[depNode.Pkg] = depNode.Container
			}
		}
		mergedOpts, mergedVals := api.BindMergeImportedOptions(
			api.BindOptions(&node.Pkg.ConfigAccessor),
			api.BindCfgVals(&node.Pkg.ConfigAccessor),
			importPkgs,
			func(dep *api.Package) bool { return pkgContainer[dep] != node.Container },
		)
		defines := api.ConfigToDefines(mergedOpts, mergedVals)
		args := make([]any, len(defines))
		for i, d := range defines {
			args[i] = d
		}
		for _, t := range buildCtx.GetTargets() {
			t.AddDefines(args...)
		}
	}
	if len(resolvedImports) > 0 && node.Pkg != nil {
		node.Pkg.SetImportConfigs(resolvedImports)
	}
	if buildCtx.GenConfigHeader() && node.Pkg != nil {
		node.Pkg.SetGenConfigHeader(true)
	}
}

func DeclareTargets(ctx *RuntimeContext, name string, dirs *api.PkgDirs, tc *toolchain.Toolchain, globalValues map[string]any) (_ *api.BuildContext, err error) {
	defer scriptcall.Recover(&err)
	if dirs == nil {
		return nil, fmt.Errorf("package %s has no build directories", name)
	}
	return ActivatePackage(ctx, name, *dirs, tc, globalValues, ActivationOptions{
		Mode: ActivationDeclarative,
		Configure: func(buildCtx *api.BuildContext) {
			api.BindSetConfigLookup(&buildCtx.ConfigAccessor, newConfigLookup(ctx, name, globalValues, nil))
		},
	})
}
