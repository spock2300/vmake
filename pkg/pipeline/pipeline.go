package pipeline

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/api"
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
	AllOptions          map[string]map[string]*api.Option
	AllKConfigs         map[string][]*api.KConfigEntry
	ChipProfiles        map[string]api.ChipProfile
	ChipProfile         api.ChipProfile
	PreferredToolchain  string
	GlobalOptions       map[string]*api.Option
	Resolver            *resolver.Resolver
	BufferedGlobalFlags map[string]*packageGlobalFlags
	GlobalMacroDefines  []string
	Lock                *lockfile.Lock
	LockPath            string
	IgnoreLock          bool
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
	cFlags   []string
	cxxFlags []string
	ldFlags  []string
	links    []string
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
	return &RuntimeContext{
		WorkDir:           p.WorkDir,
		ConfigPath:        p.ConfigPath,
		ConfigDigest:      p.ConfigDigest,
		Config:            p.Config,
		Lock:              p.Lock,
		LockPath:          p.LockPath,
		IgnoreLock:        p.IgnoreLock,
		LockPolicy:        p.LockPolicy,
		Pins:              p.Pins,
		SourceLinks:       p.SourceLinks,
		Paths:             p.Paths,
		TrustChecker:      p.TrustChecker,
		ModeOverride:      p.ModeOverride,
		ToolchainOverride: p.ToolchainOverride,
	}
}

func (ctx *RuntimeContext) effectiveLockPolicy() resolver.LockPolicy {
	if ctx == nil {
		return resolver.LockPolicy{}
	}
	p := ctx.LockPolicy
	if ctx.IgnoreLock {
		p.IgnoreAll = true
	}
	return p
}

func (ctx *RuntimeContext) packagePin(name string) (target string, fromTarget bool, ok bool) {
	if ctx == nil {
		return "", false, false
	}
	if ctx.LockPolicy.Targets != nil {
		if t := strings.TrimSpace(ctx.LockPolicy.Targets[name]); t != "" {
			return t, true, true
		}
	}
	if ctx.LockPolicy.Float != nil && ctx.LockPolicy.Float[name] {
		return "", false, false
	}
	if ctx.Pins != nil {
		if p := strings.TrimSpace(ctx.Pins[name]); p != "" {
			return p, false, true
		}
	}
	return "", false, false
}

func (ctx *RuntimeContext) isLockIgnoredFor(name string) bool {
	if ctx == nil {
		return false
	}
	if ctx.IgnoreLock || ctx.LockPolicy.IgnoreAll {
		return true
	}
	if ctx.LockPolicy.Float != nil && ctx.LockPolicy.Float[name] {
		return true
	}
	if ctx.LockPolicy.Targets != nil && strings.TrimSpace(ctx.LockPolicy.Targets[name]) != "" {
		return true
	}
	return false
}

func (ctx *RuntimeContext) lockedEntry(name string) (*lockfile.LockedPkg, bool) {
	if ctx == nil || ctx.Lock == nil || ctx.isLockIgnoredFor(name) {
		return nil, false
	}
	locked, ok := ctx.Lock.Get(name)
	if !ok || (locked.Version == "" && locked.Commit == "") {
		return nil, false
	}
	return locked, true
}

func newBuildContext(ctx *RuntimeContext, name string, globalValues map[string]any) (*api.BuildContext, error) {
	values, err := PackageConfigValues(ctx, name, globalValues)
	if err != nil {
		return nil, err
	}
	buildCtx := api.NewBuildContext(name, values)
	if opts, ok := ctx.AllOptions[name]; ok {
		buildCtx.SetOptions(opts)
	}
	buildCtx.MergeGlobals(ctx.GlobalOptions, globalValues)
	return buildCtx, nil
}

func newConfigLookup(ctx *RuntimeContext, base map[string]any, bound func(string) (map[string]any, bool)) func(string, string) (any, *api.Option, error) {
	if base == nil && ctx.Config != nil {
		base = projectGlobalValues(ctx)
	}
	values := make(map[string]map[string]any)
	return func(pkg, name string) (any, *api.Option, error) {
		if ctx.DepGraph == nil || ctx.DepGraph.Packages[pkg] == nil {
			return nil, nil, fmt.Errorf("package %q is not in the resolved dependency graph", pkg)
		}
		node := ctx.DepGraph.Packages[pkg]
		opts := ctx.AllOptions[pkg]
		if opts == nil && node.Pkg != nil {
			opts = node.Pkg.GetOptions()
		}
		opt := opts[name]
		if opt == nil {
			opt = ctx.GlobalOptions[name]
		}
		if opt == nil {
			opt = implicitPlatformOption(name)
		}
		if opt == nil {
			names := make([]string, 0, len(opts)+len(ctx.GlobalOptions))
			for optionName := range opts {
				names = append(names, optionName)
			}
			for optionName := range ctx.GlobalOptions {
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
	return mergeCfgVals(name, node, ctx, globalValues, map[string]map[string]any{
		name: config.GetEntry(ctx.Config, name).Options,
	}), nil
}

func mergeCfgVals(name string, node *resolver.PackageNode, ctx *RuntimeContext, globalValues map[string]any, allPkgOptions map[string]map[string]any) map[string]any {
	cfgVals := make(map[string]any)
	allOpts := ctx.AllOptions[name]
	if allOpts == nil && node != nil && node.Pkg != nil {
		allOpts = node.Pkg.GetOptions()
	}
	if allOpts != nil {
		for optName, opt := range allOpts {
			if _, resolved := globalValues[optName]; resolved && opt.IsGlobal() {
				continue
			}
			if opt.Default() != nil {
				cfgVals[optName] = opt.Default()
			}
		}
	}
	mergePkgOptions(cfgVals, allPkgOptions[name], allOpts, globalValues)
	for k, v := range globalValues {
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

func applyBuildContextConfig(buildCtx *api.BuildContext, node *resolver.PackageNode, ctx *RuntimeContext, currentPkg string, needed map[string]bool) {
	var subParents map[string]string
	if ctx.Resolver != nil {
		subParents = ctx.Resolver.SubParents()
	}
	var resolvedImports []string
	for _, depName := range buildCtx.ImportConfigs() {
		resolvedImports = append(resolvedImports, api.ResolveSubPackageName(currentPkg, depName, subParents, func(candidate string) bool {
			depNode := ctx.DepGraph.Packages[candidate]
			return needed[candidate] && depNode != nil && depNode.Pkg != nil
		}))
	}
	if buildCtx.GenConfigDefines() && node.Pkg != nil {
		var importPkgs []*api.Package
		for _, resolved := range resolvedImports {
			depNode := ctx.DepGraph.Packages[resolved]
			if depNode != nil && depNode.Pkg != nil {
				importPkgs = append(importPkgs, depNode.Pkg)
			}
		}
		mergedOpts, mergedVals := api.MergeImportedOptions(node.Pkg.Options, node.Pkg.CfgVals, importPkgs)
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
	node := ctx.DepGraph.Packages[name]
	buildCtx, err := newBuildContext(ctx, name, globalValues)
	if err != nil {
		return nil, err
	}
	buildCtx.SetConfigLookup(newConfigLookup(ctx, globalValues, nil))
	platform := platformFromValues(buildCtx.CfgVals)
	buildCtx.SetDryRun(true)
	buildCtx.SetBuildSubGraphFunc(func(string) error { return nil })
	buildCtx.SetDepOutputFunc(func(string) string { return "" })
	buildCtx.SetDepSrcDirFunc(func(string) string { return "" })

	if node != nil && node.Pkg != nil {
		pkg := node.Pkg
		allOpts := ctx.AllOptions[name]
		if allOpts == nil {
			allOpts = pkg.GetOptions()
		}
		pkg.SetDirs(*dirs)
		pkg.SetOptions(allOpts)
		pkg.SetCfgVals(maps.Clone(buildCtx.CfgVals))
		if tc != nil {
			name := resolvePkgToolchain(ctx.Config, name, tc.Name)
			if name != tc.Name {
				tc, err = toolchain.GetManager().GetToolchain(name)
				if err != nil {
					return nil, err
				}
			}
			pkg.SetToolchain(tc)
			pkg.SetPlatform(platform)
			flags := api.DefaultBuildFlags(tc.Name, pkg.TargetOS())
			buildCtx.SetDefaultFlags(flags.CFlags, flags.CxxFlags, flags.LdFlags)
		}
		buildCtx.SetPackage(pkg)

		pkg.SetDryRun(true)
		defer pkg.SetDryRun(false)
		pkg.ExecBuildFuncs(dirs.SourceDir, func(fn api.BuildFunc) {
			fn(buildCtx)
		})
		pkg.SetDryRun(false)
	}

	return buildCtx, nil
}
