package pipeline

import (
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
	LocksDir   string
	ReposDir   string
	LockPath   string
}

type RuntimeContext struct {
	WorkDir             string
	ConfigPath          string
	Config              *config.ConfigFile
	DepGraph            *resolver.Graph
	AllOptions          map[string]map[string]*api.Option
	AllKConfigs         map[string][]*api.KConfigEntry
	GlobalOptions       map[string]*api.Option
	Resolver            *resolver.Resolver
	BufferedGlobalFlags map[string]*packageGlobalFlags
	Lock                *lockfile.Lock
	LockPath            string
	IgnoreLock          bool

	Paths             *Paths
	TrustChecker      buildscript.ScriptTrustChecker
	ModeOverride      string
	ToolchainOverride string
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
	Config            *config.ConfigFile
	Lock              *lockfile.Lock
	LockPath          string
	IgnoreLock        bool
	TrustChecker      buildscript.ScriptTrustChecker
	Paths             *Paths
	ModeOverride      string
	ToolchainOverride string
}

func NewContext(p ResolveParams) *RuntimeContext {
	return &RuntimeContext{
		WorkDir:           p.WorkDir,
		ConfigPath:        p.ConfigPath,
		Config:            p.Config,
		Lock:              p.Lock,
		LockPath:          p.LockPath,
		IgnoreLock:        p.IgnoreLock,
		Paths:             p.Paths,
		TrustChecker:      p.TrustChecker,
		ModeOverride:      p.ModeOverride,
		ToolchainOverride: p.ToolchainOverride,
	}
}

func newBuildContext(ctx *RuntimeContext, name string, globalValues map[string]any) *api.BuildContext {
	entry := config.GetEntry(ctx.Config, name)
	buildCtx := api.NewBuildContext(name, entry.Options)
	if opts, ok := ctx.AllOptions[name]; ok {
		buildCtx.SetOptions(opts)
	}
	buildCtx.MergeGlobals(ctx.GlobalOptions, globalValues)
	return buildCtx
}

func mergeCfgVals(name string, node *resolver.PackageNode, ctx *RuntimeContext, globalValues map[string]any, allPkgOptions map[string]map[string]any) map[string]any {
	cfgVals := make(map[string]any)
	allOpts := ctx.AllOptions[name]
	if allOpts == nil && node.Pkg != nil {
		allOpts = node.Pkg.GetOptions()
	}
	if allOpts != nil {
		for optName, opt := range allOpts {
			if opt.Default() != nil {
				cfgVals[optName] = opt.Default()
			}
		}
	}
	if opts, ok := allPkgOptions[name]; ok {
		for k, v := range opts {
			cfgVals[k] = v
		}
	}
	for k, v := range globalValues {
		if _, exists := cfgVals[k]; !exists {
			cfgVals[k] = v
		}
	}
	return cfgVals
}

func applyBuildContextConfig(buildCtx *api.BuildContext, node *resolver.PackageNode, ctx *RuntimeContext, currentPkg string) {
	subParents := ctx.Resolver.SubParents()
	if buildCtx.GenConfigDefines() && node.Pkg != nil {
		var importPkgs []*api.Package
		for _, depName := range buildCtx.ImportConfigs() {
			resolved := api.ResolveSubPackageName(currentPkg, depName, subParents, func(candidate string) bool {
				_, ok := ctx.DepGraph.Packages[candidate]
				return ok
			})
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
	if buildCtx.ExportEnabled() && node.Pkg != nil {
		node.Pkg.SetExportConfig(true)
	}
	if imports := buildCtx.ImportConfigs(); len(imports) > 0 && node.Pkg != nil {
		node.Pkg.SetImportConfigs(imports)
	}
	if buildCtx.GenConfigHeader() && node.Pkg != nil {
		node.Pkg.SetGenConfigHeader(true)
	}
}

func DeclareTargets(ctx *RuntimeContext, name string, dirs *api.PkgDirs, tc *toolchain.Toolchain, globalValues map[string]any) *api.BuildContext {
	node := ctx.DepGraph.Packages[name]
	buildCtx := newBuildContext(ctx, name, globalValues)
	buildCtx.SetDryRun(true)
	buildCtx.SetBuildSubGraphFunc(func(string) error { return nil })
	buildCtx.SetDepOutputFunc(func(string) string { return "" })

	if node != nil && node.Pkg != nil {
		pkg := node.Pkg
		allOpts := ctx.AllOptions[name]
		if allOpts == nil {
			allOpts = pkg.GetOptions()
		}
		cfgVals := mergeCfgVals(name, node, ctx, globalValues, map[string]map[string]any{
			name: config.GetEntry(ctx.Config, name).Options,
		})
		pkg.SetDirs(*dirs)
		pkg.SetOptions(allOpts)
		pkg.SetCfgVals(cfgVals)
		if tc != nil {
			pkg.SetToolchain(tc)
		}
		buildCtx.SetPackage(pkg)

		pkg.SetDryRun(true)
		pkg.ExecBuildFuncs(dirs.SourceDir, func(fn api.BuildFunc) {
			fn(buildCtx)
		})
		pkg.SetDryRun(false)
	}

	return buildCtx
}
