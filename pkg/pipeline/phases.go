package pipeline

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func Require(ctx *RuntimeContext) (err error) {
	defer scriptcall.Recover(&err)
	vlog.Info("Scanning %s...", ctx.WorkDir)

	links, err := ValidateSourceLinks(ctx)
	if err != nil {
		return err
	}
	ctx.SourceLinks = links
	if ctx.Pins == nil && ctx.Paths != nil && ctx.Paths.ProjectDir != "" {
		pins, err := config.LoadProjectPins(ctx.Paths.ProjectDir)
		if err != nil {
			return err
		}
		ctx.Pins = pins
	}

	packages, err := buildscript.Scan(ctx.WorkDir)
	if err != nil {
		return err
	}

	if len(packages) == 0 {
		return fmt.Errorf("no build.go files found")
	}

	var pkgNames []string
	for _, pkg := range packages {
		pkgNames = append(pkgNames, pkg.Name)
	}
	vlog.Info("Found %d package(s)", len(packages))
	vlog.Verbose("  %s", strings.Join(pkgNames, ", "))

	r := resolver.NewResolver(repo.NewRepoManager(ctx.Paths.ReposDir), ctx.Paths.DepsDir)
	projectRoot := ctx.WorkDir
	if ctx.Paths != nil && ctx.Paths.ProjectDir != "" {
		projectRoot = ctx.Paths.ProjectDir
	}
	r.SetProjectRoot(projectRoot)
	r.SetContext(ctx.Context)
	r.SetSourceManager(repo.NewSourceManager(ctx.Paths.DepsDir, ctx.Paths.CacheDir).WithSession(ctx.Locks).WithContext(ctx.Context))
	r.SetLockfile(ctx.Lock, ctx.IgnoreLock)
	r.SetLockPolicy(ctx.effectiveLockPolicy())
	r.SetTrustChecker(ctx.TrustChecker)
	r.SetConfigPins(ctx.Pins)
	r.SetSourceLinks(ctx.SourceLinks)
	ctx.Resolver = r

	vlog.Info("")
	vlog.Info("Resolving dependencies...")

	if err := r.ResolveAll(packages); err != nil {
		return err
	}

	ctx.DepGraph = r.Graph()

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if node.IsLocal() {
			vlog.Verbose("  %s (local)", name)
		} else {
			vlog.Verbose("  %s", name)
		}
	}

	return nil
}

func Configure(ctx *RuntimeContext) (err error) {
	defer scriptcall.Recover(&err)
	if err := ctx.Resolver.UpdateOrder(); err != nil {
		return err
	}
	return runConfigPhase(ctx)
}

func bindBaseGlobalFlagCallbacks(cfgCtx *api.ConfigContext, buf *packageGlobalFlags) {
	cfgCtx.SetGlobalCFlagsFunc(func(flags ...string) {
		buf.cFlags = append(buf.cFlags, flags...)
	})
	cfgCtx.SetGlobalCxxFlagsFunc(func(flags ...string) {
		buf.cxxFlags = append(buf.cxxFlags, flags...)
	})
	cfgCtx.SetGlobalLdFlagsFunc(func(flags ...string) {
		buf.ldFlags = append(buf.ldFlags, flags...)
	})
	cfgCtx.SetGlobalLinksFunc(func(links ...string) {
		buf.links = append(buf.links, links...)
	})
}

func bindApplyGlobalFlagCallbacks(cfgCtx *api.ConfigContext, buf *packageGlobalFlags) {
	cfgCtx.SetGlobalCFlagsFunc(func(flags ...string) {
		buf.applyCFlags = append(buf.applyCFlags, flags...)
	})
	cfgCtx.SetGlobalCxxFlagsFunc(func(flags ...string) {
		buf.applyCxxFlags = append(buf.applyCxxFlags, flags...)
	})
	cfgCtx.SetGlobalLdFlagsFunc(func(flags ...string) {
		buf.applyLdFlags = append(buf.applyLdFlags, flags...)
	})
	cfgCtx.SetGlobalLinksFunc(func(links ...string) {
		buf.applyLinks = append(buf.applyLinks, links...)
	})
}

func collectOptions(name, dir string, pkg *api.Package, buf *packageGlobalFlags) map[string]*api.Option {
	cfgCtx := api.NewConfigContextWithPackage(name, pkg)
	bindBaseGlobalFlagCallbacks(cfgCtx, buf)
	pkg.ExecConfigFuncs(dir, func(fn api.ConfigFunc) { fn(cfgCtx) })
	return cfgCtx.GetOptions()
}

func collectAllOptionsAndKConfigs(ctx *RuntimeContext) error {
	ctx.AllOptions = make(map[string]map[string]*api.Option)
	ctx.AllKConfigs = make(map[string][]*api.KConfigEntry)
	ctx.BufferedGlobalFlags = make(map[string]*packageGlobalFlags)
	pkgDirs := ResolveAllPackageDirs(ctx.DepGraph)

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]

		var opts map[string]*api.Option
		if node.Pkg != nil {
			buf := &packageGlobalFlags{}
			ctx.BufferedGlobalFlags[name] = buf
			opts = collectOptions(name, pkgDirs[name].SourceDir, node.Pkg, buf)
		}

		if len(opts) > 0 {
			for _, opt := range opts {
				if err := api.ValidateOption(opt); err != nil {
					return fmt.Errorf("package %s: %w", name, err)
				}
			}
			ctx.AllOptions[name] = opts
			vlog.Verbose("  %s: %d option(s)", name, len(opts))
		}

		if node.Pkg != nil {
			entries := node.Pkg.KConfigEntries()
			if len(entries) > 0 && node.Source != nil && node.Source.Link != nil {
				return fmt.Errorf("linked package %s declares KConfig; linked sources do not support KConfig", name)
			}
			if len(entries) > 0 {
				for _, e := range entries {
					if e.ConfigPath() == "" {
						e.SetConfigPath(".config")
					}
					if e.SrcDir() == "" {
						e.SetSrcDir(pkgDirs[name].SourceDir)
					} else if !filepath.IsAbs(e.SrcDir()) {
						e.SetSrcDir(filepath.Join(pkgDirs[name].SourceDir, e.SrcDir()))
					}
					cfgEntry := config.GetEntry(ctx.Config, name)
					if cfgEntry.SelectedPreset != "" {
						e.SelectPreset(cfgEntry.SelectedPreset)
					} else if e.DefaultPreset() != "" {
						e.SelectPreset(e.DefaultPreset())
					}
				}
				ctx.AllKConfigs[name] = entries
				vlog.Verbose("  %s: %d kconfig(s)", name, len(entries))
			}
		}
	}
	return nil
}

func applyAllConfigCallbacks(ctx *RuntimeContext) {
	applyConfigCallbacksForNeeded(ctx, nil, true)
}

func applyConfigCallbacksForNeeded(ctx *RuntimeContext, needed map[string]bool, logHeader bool) {
	if logHeader {
		vlog.Info("")
		vlog.Info("Applying configuration...")
	}
	if ctx.BufferedGlobalFlags == nil {
		ctx.BufferedGlobalFlags = make(map[string]*packageGlobalFlags)
	}
	globalVals := config.BuildGlobalValues(ctx.Config)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if needed != nil {
			if buf := ctx.BufferedGlobalFlags[name]; buf != nil && !needed[name] {
				buf.resetApply()
			}
			if !needed[name] {
				if node != nil && node.Pkg != nil {
					node.Pkg.ResetApplyState()
				}
				continue
			}
		}
		if node == nil || node.Pkg == nil {
			continue
		}
		node.Pkg.ResetApplyState()
		opts := ctx.AllOptions[name]
		if len(opts) == 0 {
			continue
		}

		buf := ctx.BufferedGlobalFlags[name]
		if buf == nil {
			buf = &packageGlobalFlags{}
			ctx.BufferedGlobalFlags[name] = buf
		}
		buf.resetApply()

		applyCtx := api.NewConfigContextWithPackage(name, node.Pkg)
		applyCtx.SetOptions(mergedPackageAndGlobalOptions(opts, visibleGlobalOptionsFor(node, opts, ctx.GlobalOptions)))
		applyVals := packageConfigValuesWithGlobals(ctx, name, globalVals)
		applyCtx.SetCfgVals(applyVals)
		bindApplyGlobalFlagCallbacks(applyCtx, buf)

		optNames := make([]string, 0, len(opts))
		for optName := range opts {
			optNames = append(optNames, optName)
		}
		sort.Strings(optNames)
		for _, optName := range optNames {
			opt := opts[optName]
			if opt.OnApply() == nil {
				continue
			}
			val, ok := applyVals[optName]
			if !ok || val == nil {
				val = opt.Default()
			}
			val = api.NormalizeOptionValue(opt, val)
			vlog.Debug("  %s/%s = %v", name, optName, val)
			api.RunScriptSafe(name, func() { opt.OnApply()(applyCtx, val) })
		}
	}
}

func packageConfigValuesWithGlobals(ctx *RuntimeContext, name string, globalVals map[string]any) map[string]any {
	if ctx == nil || ctx.Config == nil {
		return nil
	}
	var node *resolver.PackageNode
	if ctx.DepGraph != nil {
		node = ctx.DepGraph.Packages[name]
	}
	opts := ctx.AllOptions[name]
	visGlobals := visibleGlobalValuesFor(node, opts, ctx.GlobalOptions, globalVals)
	entry := config.GetEntry(ctx.Config, name)
	values := make(map[string]any, len(entry.Options)+len(visGlobals))
	for optName, val := range visGlobals {
		if opt, declared := opts[optName]; declared && !opt.IsGlobal() {
			continue
		}
		values[optName] = val
	}
	if ctx.DerivedOptions != nil {
		for k, v := range ctx.DerivedOptions[name] {
			if opt, declared := opts[k]; declared && opt.IsGlobal() {
				continue
			}
			values[k] = v
		}
	}
	mergePkgOptions(values, entry.Options, opts, visGlobals)
	return values
}

var globalMacroExcluded = map[string]bool{
	api.ModeOptionName:         true,
	api.ToolchainOptionName:    true,
	api.TargetOSOptionName:     true,
	api.TargetTripleOptionName: true,
}

func collectGlobalMacroDefines(opts map[string]*api.Option, vals map[string]any) ([]string, error) {
	names := make([]string, 0, len(opts))
	for name := range opts {
		names = append(names, name)
	}
	sort.Strings(names)
	var defines []string
	definesByName := make(map[string]string)
	owners := make(map[string]string)
	for _, name := range names {
		opt := opts[name]
		if !opt.ExportDefine() {
			continue
		}
		if globalMacroExcluded[name] && opt.MacroName() == "" {
			continue
		}
		val, ok := vals[name]
		if !ok || val == nil {
			val = opt.Default()
		}
		if val == nil {
			continue
		}
		val = api.NormalizeOptionValue(opt, val)
		optionDefines, err := opt.MacroDefines(val)
		if err != nil {
			return nil, err
		}
		for _, define := range optionDefines {
			macro, _, _ := strings.Cut(define, "=")
			if prev, exists := definesByName[macro]; exists {
				if prev != define {
					return nil, fmt.Errorf("global macro %s conflicts between options %s and %s (%q vs %q)", macro, owners[macro], name, prev, define)
				}
				continue
			}
			definesByName[macro] = define
			owners[macro] = name
			defines = append(defines, define)
		}
	}
	sort.Strings(defines)
	return defines, nil
}

func mergeGlobalOptions(ctx *RuntimeContext) error {
	return mergeGlobalOptionsFiltered(ctx, nil)
}

func mergeGlobalOptionsFiltered(ctx *RuntimeContext, needed map[string]bool) error {
	mgr := toolchain.GetManager()
	var tcList []string
	if tcs, err := mgr.ListToolchains(); err == nil {
		for name := range tcs {
			tcList = append(tcList, name)
		}
		sort.Strings(tcList)
	}

	allDefs := ctx.AllOptions
	if needed != nil {
		allDefs = make(map[string]map[string]*api.Option, len(needed))
		for pkgName, opts := range ctx.AllOptions {
			if needed[pkgName] {
				allDefs[pkgName] = opts
			}
		}
	}

	var err error
	ctx.GlobalOptions, err = api.MergeGlobalOptions(allDefs, tcList)
	if err != nil {
		return fmt.Errorf("global options error: %w", err)
	}
	return nil
}

func updateGlobalMacroDefines(ctx *RuntimeContext) error {
	values := config.BuildGlobalValues(ctx.Config)
	values[api.ModeOptionName] = ResolveMode(ctx.Config, ctx.ModeOverride)
	values[api.ToolchainOptionName] = ResolveToolchainName(ctx.Config, ctx.ToolchainOverride, ctx.PreferredToolchain)
	defines, err := collectGlobalMacroDefines(ctx.GlobalOptions, values)
	if err != nil {
		return fmt.Errorf("global option macro export: %w", err)
	}
	ctx.GlobalMacroDefines = defines
	return nil
}

func runConfigPhase(ctx *RuntimeContext) error {
	vlog.Info("")
	vlog.Info("Executing OnConfig...")

	if err := collectAllOptionsAndKConfigs(ctx); err != nil {
		return err
	}
	if err := collectChipProfiles(ctx); err != nil {
		return err
	}
	if err := mergeGlobalOptions(ctx); err != nil {
		return err
	}
	extendChipChoice(ctx, nil)
	if ctx.OnOptionsCollected != nil {
		if err := ctx.OnOptionsCollected(ctx); err != nil {
			return err
		}
	}
	resolvePreviewDerivedOptions(ctx)
	applyAllConfigCallbacks(ctx)
	return updateGlobalMacroDefines(ctx)
}

func resolveWithDefault(flagVal, configVal, defaultVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if configVal != "" {
		return configVal
	}
	return defaultVal
}

func ResolveMode(cfg *config.ConfigFile, flagValue string) string {
	var configMode string
	if cfg.Global != nil {
		configMode = cfg.Global.Mode
	}
	return resolveWithDefault(flagValue, configMode, api.ModeDebug)
}

func ResolveToolchainName(cfg *config.ConfigFile, flagValue, fallback string) string {
	var configTC string
	if cfg != nil && cfg.Global != nil {
		configTC = cfg.Global.Toolchain
	}
	if fallback == "" {
		fallback = toolchain.GetManager().GetDefaultToolchain()
	}
	return resolveWithDefault(flagValue, configTC, fallback)
}

func resolvePkgToolchain(cfg *config.ConfigFile, pkgName, defaultTc string) string {
	entry := config.GetEntry(cfg, pkgName)
	if v, ok := entry.Options["toolchain"].(string); ok && v != "" {
		return v
	}
	return defaultTc
}

func GetToolchain(cfg *config.ConfigFile, toolchainFlag, fallback string) (*toolchain.Toolchain, string, error) {
	mgr := toolchain.GetManager()
	tcName := ResolveToolchainName(cfg, toolchainFlag, fallback)
	tc, err := mgr.SelectToolchain(tcName)
	if err != nil {
		return nil, tcName, err
	}
	return tc, tcName, nil
}

func ResolveAllPackageDirs(graph *resolver.Graph) map[string]*api.PkgDirs {
	dirs := make(map[string]*api.PkgDirs)
	for name, node := range graph.Packages {
		if node.Source == nil {
			continue
		}
		dirs[name] = &api.PkgDirs{SourceDir: node.Source.Dir}
	}
	return dirs
}
