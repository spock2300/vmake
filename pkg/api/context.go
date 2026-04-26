package api

import (
	"path/filepath"
	"sort"
	"strings"

	"gitee.com/spock2300/vmake/internal/exec"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type pkgBase struct {
	pkgName string
}

func (b *pkgBase) PackageName() string {
	return b.pkgName
}

type ConfigContext struct {
	ConfigAccessor
	pkgBase
	pkg               *Package
	addGlobalCFlags   func(...string)
	addGlobalCxxFlags func(...string)
	addGlobalLdFlags  func(...string)
}

func NewConfigContext(pkgName string) *ConfigContext {
	return &ConfigContext{
		ConfigAccessor: NewConfigAccessor(nil, nil),
		pkgBase:        pkgBase{pkgName: pkgName},
	}
}

func NewConfigContextWithPackage(pkgName string, pkg *Package) *ConfigContext {
	return &ConfigContext{
		ConfigAccessor: NewConfigAccessor(nil, nil),
		pkgBase:        pkgBase{pkgName: pkgName},
		pkg:            pkg,
	}
}

func (ctx *ConfigContext) SetGlobalCFlagsFunc(fn func(...string)) *ConfigContext {
	ctx.addGlobalCFlags = fn
	return ctx
}

func (ctx *ConfigContext) SetGlobalCxxFlagsFunc(fn func(...string)) *ConfigContext {
	ctx.addGlobalCxxFlags = fn
	return ctx
}

func (ctx *ConfigContext) SetGlobalLdFlagsFunc(fn func(...string)) *ConfigContext {
	ctx.addGlobalLdFlags = fn
	return ctx
}

func (ctx *ConfigContext) AddGlobalCFlags(flags ...string) {
	if ctx.addGlobalCFlags != nil {
		ctx.addGlobalCFlags(flags...)
	}
}

func (ctx *ConfigContext) AddGlobalCxxFlags(flags ...string) {
	if ctx.addGlobalCxxFlags != nil {
		ctx.addGlobalCxxFlags(flags...)
	}
}

func (ctx *ConfigContext) AddGlobalLdFlags(flags ...string) {
	if ctx.addGlobalLdFlags != nil {
		ctx.addGlobalLdFlags(flags...)
	}
}

func (ctx *ConfigContext) SetProvidedLinkerScript(path string) *ConfigContext {
	if ctx.pkg != nil {
		ctx.pkg.SetProvidedLinkerScript(path)
	}
	return ctx
}

func (ctx *ConfigContext) KConfig(name string) *KConfigEntry {
	if ctx.pkg != nil {
		return ctx.pkg.AddKConfig(name)
	}
	return &KConfigEntry{name: name}
}

func (ctx *ConfigContext) SetConfigValue(name string, val any) *ConfigContext {
	ctx.CfgVals[name] = val
	return ctx
}

func (ctx *ConfigContext) GetOptions() map[string]*Option {
	return ctx.Options
}

func (ctx *ConfigContext) Toolchains() []string {
	tcs, err := toolchain.GetManager().ListToolchains()
	if err != nil {
		return []string{"host"}
	}
	names := make([]string, 0, len(tcs))
	for name := range tcs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (ctx *ConfigContext) GlobalOption(name string) *Option {
	opt := ctx.Option(name)
	opt.group = "Global"
	return opt
}

func (ctx *ConfigContext) GlobalMode() *Option {
	if builtIn, ok := BuiltInGlobalOptions[ModeOptionName]; ok {
		return ctx.GlobalOption(ModeOptionName).
			SetType(builtIn.Type()).
			SetDefault(builtIn.Default()).
			SetDescription(builtIn.Description()).
			SetValues(builtIn.Values()...)
	}
	return ctx.GlobalOption(ModeOptionName)
}

func (ctx *ConfigContext) ToolchainOption() *Option {
	return ctx.Option(ToolchainOptionName).
		SetType(OptionChoice).
		SetDefault("host").
		SetDescription("Build toolchain").
		SetValues(ctx.Toolchains()...)
}

type BuildContext struct {
	ConfigAccessor
	*TargetRegistry
	*InstallItemHolder
	pkgBase
	pkg               *Package
	genConfigHeader   bool
	genConfigDefines  bool
	buildSubGraphFunc func(pkgName string) error
	depOutputFunc     func(depRef string) string
	dryRun            bool
}

func NewBuildContext(pkgName string, cfgVals map[string]any) *BuildContext {
	return &BuildContext{
		ConfigAccessor:    NewConfigAccessor(cfgVals, nil),
		TargetRegistry:    NewTargetRegistry(),
		InstallItemHolder: &InstallItemHolder{},
		pkgBase:           pkgBase{pkgName: pkgName},
	}
}

func (ctx *BuildContext) SetDryRun(v bool) *BuildContext {
	ctx.dryRun = v
	return ctx
}

func (ctx *BuildContext) SetPackage(pkg *Package) *BuildContext {
	ctx.pkg = pkg
	return ctx
}

func (ctx *BuildContext) SetBuildSubGraphFunc(fn func(string) error) *BuildContext {
	ctx.buildSubGraphFunc = fn
	return ctx
}

func (ctx *BuildContext) SetDepOutputFunc(fn func(string) string) *BuildContext {
	ctx.depOutputFunc = fn
	return ctx
}

func (ctx *BuildContext) BuildSubGraph(pkgName string) {
	if ctx.dryRun {
		return
	}
	if ctx.buildSubGraphFunc == nil {
		vlog.Fatal("BuildSubGraph: not available")
	}
	if err := ctx.buildSubGraphFunc(pkgName); err != nil {
		vlog.Fatal("BuildSubGraph %s: %v", pkgName, err)
	}
}

func (ctx *BuildContext) DepOutput(depRef string) string {
	if ctx.dryRun {
		return ""
	}
	if ctx.depOutputFunc == nil {
		vlog.Fatal("DepOutput: not available")
	}
	return ctx.depOutputFunc(depRef)
}

func (ctx *BuildContext) DepBuildDir(depRef string) string {
	return filepath.Dir(ctx.DepOutput(depRef))
}

func (ctx *BuildContext) GenerateConfigHeader() *BuildContext {
	ctx.genConfigHeader = true
	return ctx
}

func (ctx *BuildContext) GenerateConfigDefines() *BuildContext {
	ctx.genConfigDefines = true
	return ctx
}

func (ctx *BuildContext) GenConfigHeaderFlag() bool  { return ctx.genConfigHeader }
func (ctx *BuildContext) GenConfigDefinesFlag() bool { return ctx.genConfigDefines }

func (ctx *BuildContext) Exec(name string, args ...string) {
	if ctx.dryRun {
		return
	}
	vlog.Info("  %s %s", name, strings.Join(args, " "))
	exec.RunFatal("", name, args...)
}

type InstallContext struct {
	ConfigAccessor
	*InstallItemHolder
	pkgBase
	prefix    string
	prefixSet bool
}

func NewInstallContext(pkgName string, cfgVals map[string]any) *InstallContext {
	return &InstallContext{
		ConfigAccessor:    NewConfigAccessor(cfgVals, nil),
		InstallItemHolder: &InstallItemHolder{},
		pkgBase:           pkgBase{pkgName: pkgName},
	}
}

func (ctx *InstallContext) Prefix() string  { return ctx.prefix }
func (ctx *InstallContext) PrefixSet() bool { return ctx.prefixSet }

func (ctx *InstallContext) SetPrefix(prefix string) *InstallContext {
	ctx.prefix = prefix
	ctx.prefixSet = true
	return ctx
}
