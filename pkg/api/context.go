package api

import (
	"sort"
	"strings"

	"gitee.com/spock2300/vmake/internal/exec"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type ConfigContext struct {
	ConfigAccessor
	pkgName string
}

func NewConfigContext(pkgName string) *ConfigContext {
	return &ConfigContext{
		ConfigAccessor: NewConfigAccessor(nil, nil),
		pkgName:        pkgName,
	}
}

func (ctx *ConfigContext) SetConfigValue(name string, val any) *ConfigContext {
	ctx.CfgVals[name] = val
	return ctx
}

func (ctx *ConfigContext) GetOptions() map[string]*Option {
	return ctx.Options
}

func (ctx *ConfigContext) PackageName() string {
	return ctx.pkgName
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

type BuildContext struct {
	ConfigAccessor
	*TargetRegistry
	*InstallItemHolder
	pkgName           string
	buildSubGraphFunc func(pkgName string) error
	depOutputFunc     func(depRef string) string
	dryRun            bool
}

func NewBuildContext(pkgName string, cfgVals map[string]any) *BuildContext {
	return &BuildContext{
		ConfigAccessor:    NewConfigAccessor(cfgVals, nil),
		TargetRegistry:    NewTargetRegistry(),
		InstallItemHolder: &InstallItemHolder{},
		pkgName:           pkgName,
	}
}

func (ctx *BuildContext) SetDryRun(v bool) *BuildContext {
	ctx.dryRun = v
	return ctx
}

func (ctx *BuildContext) PackageName() string {
	return ctx.pkgName
}

type InstallItem struct {
	Src  string
	Dest string
}

type InstallFilterFunc func(path string, isTargetOutput bool) bool

func (ctx *BuildContext) SetBuildSubGraphFunc(fn func(string) error) {
	ctx.buildSubGraphFunc = fn
}

func (ctx *BuildContext) SetDepOutputFunc(fn func(string) string) {
	ctx.depOutputFunc = fn
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
	pkgName   string
	prefix    string
	prefixSet bool
}

func NewInstallContext(pkgName string, cfgVals map[string]any) *InstallContext {
	return &InstallContext{
		ConfigAccessor:    NewConfigAccessor(cfgVals, nil),
		InstallItemHolder: &InstallItemHolder{},
		pkgName:           pkgName,
	}
}

func (ctx *InstallContext) Prefix() string      { return ctx.prefix }
func (ctx *InstallContext) PrefixSet() bool     { return ctx.prefixSet }
func (ctx *InstallContext) PackageName() string { return ctx.pkgName }

func (ctx *InstallContext) SetPrefix(prefix string) *InstallContext {
	ctx.prefix = prefix
	ctx.prefixSet = true
	return ctx
}
