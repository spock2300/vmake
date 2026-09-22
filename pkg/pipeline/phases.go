package pipeline

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func Require(ctx *RuntimeContext) error {
	vlog.Info("Scanning %s...", ctx.WorkDir)

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
	vlog.Info("Found %d package(s): %s", len(packages), strings.Join(pkgNames, ", "))

	r := resolver.NewResolver(repo.NewRepoManager(ctx.Paths.ReposDir), ctx.Paths.DepsDir)
	r.SetSourceManager(repo.NewSourceManager(ctx.Paths.DepsDir, ctx.Paths.CacheDir))
	r.SetLockfile(ctx.Lock, ctx.IgnoreLock)
	r.SetTrustChecker(ctx.TrustChecker)
	r.SetConfigPins(collectConfigPins(ctx.Config))
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
			vlog.Info("  %s (local)", name)
		} else {
			vlog.Info("  %s", name)
		}
	}

	return nil
}

func Configure(ctx *RuntimeContext) error {
	if err := ctx.Resolver.UpdateOrder(); err != nil {
		return err
	}
	return runConfigPhase(ctx)
}

func collectConfigPins(cfg *config.ConfigFile) map[string]string {
	pins := make(map[string]string)
	for name, entry := range cfg.Entries {
		if entry != nil && entry.Version != "" {
			pins[name] = entry.Version
		}
	}
	return pins
}

func collectOptions(name, dir string, pkg *api.Package, buf *packageGlobalFlags) map[string]*api.Option {
	cfgCtx := api.NewConfigContextWithPackage(name, pkg)
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
			vlog.Info("  %s: %d option(s)", name, len(opts))
		}

		if node.Pkg != nil {
			entries := node.Pkg.KConfigEntries()
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
				vlog.Info("  %s: %d kconfig(s)", name, len(entries))
			}
		}
	}
	return nil
}

func applyAllConfigCallbacks(ctx *RuntimeContext) {
	vlog.Info("")
	vlog.Info("Applying configuration...")
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.Pkg == nil {
			continue
		}
		opts := ctx.AllOptions[name]
		if len(opts) == 0 {
			continue
		}

		buf := ctx.BufferedGlobalFlags[name]
		if buf == nil {
			buf = &packageGlobalFlags{}
			ctx.BufferedGlobalFlags[name] = buf
		}

		entry := config.GetEntry(ctx.Config, name)
		applyCtx := api.NewConfigContextWithPackage(name, node.Pkg)
		applyCtx.SetOptions(opts)
		applyCtx.SetCfgVals(entry.Options)
		applyCtx.SetGlobalCFlagsFunc(func(flags ...string) {
			buf.cFlags = append(buf.cFlags, flags...)
		})
		applyCtx.SetGlobalCxxFlagsFunc(func(flags ...string) {
			buf.cxxFlags = append(buf.cxxFlags, flags...)
		})
		applyCtx.SetGlobalLdFlagsFunc(func(flags ...string) {
			buf.ldFlags = append(buf.ldFlags, flags...)
		})
		applyCtx.SetGlobalLinksFunc(func(links ...string) {
			buf.links = append(buf.links, links...)
		})

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
			val, ok := entry.Options[optName]
			if !ok || val == nil {
				val = opt.Default()
			}
			val = api.NormalizeOptionValue(opt, val)
			vlog.Debug("  %s/%s = %v", name, optName, val)
			api.RunScriptSafe(name, func() { opt.OnApply()(applyCtx, val) })
		}
	}
}

func applyGlobalFlagsFromNeeded(ctx *RuntimeContext, needed map[string]bool) {
	mgr := toolchain.GetManager()
	var cflags, cxxflags, ldflags, links []string
	for _, name := range ctx.Resolver.GetOrder() {
		if !needed[name] {
			continue
		}
		buf := ctx.BufferedGlobalFlags[name]
		if buf == nil {
			continue
		}
		cflags = append(cflags, buf.cFlags...)
		cxxflags = append(cxxflags, buf.cxxFlags...)
		ldflags = append(ldflags, buf.ldFlags...)
		links = append(links, buf.links...)
	}
	mgr.SetProjectFlags(cflags, cxxflags, ldflags, links)
}

func buildToolchainAndGlobalOptions(ctx *RuntimeContext) error {
	mgr := toolchain.GetManager()
	var tcList []string
	if tcs, err := mgr.ListToolchains(); err == nil {
		for name := range tcs {
			tcList = append(tcList, name)
		}
		sort.Strings(tcList)
	}

	var err error
	ctx.GlobalOptions, err = api.MergeGlobalOptions(ctx.AllOptions, tcList)
	if err != nil {
		return fmt.Errorf("global options error: %w", err)
	}
	return nil
}

func runConfigPhase(ctx *RuntimeContext) error {
	vlog.Info("")
	vlog.Info("Executing OnConfig...")

	if err := collectAllOptionsAndKConfigs(ctx); err != nil {
		return err
	}
	applyAllConfigCallbacks(ctx)
	return buildToolchainAndGlobalOptions(ctx)
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

func ResolveToolchainName(cfg *config.ConfigFile, flagValue string) string {
	var configTC string
	if cfg.Global != nil {
		configTC = cfg.Global.Toolchain
	}
	return resolveWithDefault(flagValue, configTC, toolchain.GetManager().GetDefaultToolchain())
}

func resolvePkgToolchain(cfg *config.ConfigFile, pkgName, defaultTc string) string {
	entry := config.GetEntry(cfg, pkgName)
	if v, ok := entry.Options["toolchain"].(string); ok && v != "" {
		return v
	}
	return defaultTc
}

func GetToolchain(cfg *config.ConfigFile, toolchainFlag string) (*toolchain.Toolchain, string, error) {
	mgr := toolchain.GetManager()
	tcName := ResolveToolchainName(cfg, toolchainFlag)
	tc, err := mgr.SelectToolchain(tcName)
	if err != nil {
		return nil, "", err
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
