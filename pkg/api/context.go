package api

import (
	"strings"

	"gitee.com/spock2300/vmake/internal/exec"
	vlog "gitee.com/spock2300/vmake/pkg/log"
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

func (ctx *ConfigContext) GlobalOption(name string) *Option {
	opt := ctx.Option(name)
	opt.isGlobal = true
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
	installHolder InstallItemHolder
	pkgName       string
	subBuildFunc  func(tcName, dir string, args ...string) error
}

func NewBuildContext(pkgName string, cfgVals map[string]any) *BuildContext {
	return &BuildContext{
		ConfigAccessor: NewConfigAccessor(cfgVals, nil),
		TargetRegistry: NewTargetRegistry(),
		pkgName:        pkgName,
	}
}

func (ctx *BuildContext) PackageName() string {
	return ctx.pkgName
}

type InstallItem struct {
	Src  string
	Dest string
}

type InstallFilterFunc func(path string, isTargetOutput bool) bool

func (ctx *BuildContext) AddInstalls(src, dest string) *BuildContext {
	ctx.installHolder.addInstall(src, dest)
	return ctx
}

func (ctx *BuildContext) GetInstallItems() []InstallItem {
	return ctx.installHolder.getInstallItems()
}

func (ctx *BuildContext) SetInstallFilter(filter InstallFilterFunc) *BuildContext {
	ctx.installHolder.setInstallFilter(filter)
	return ctx
}

func (ctx *BuildContext) GetInstallFilter() InstallFilterFunc {
	return ctx.installHolder.getInstallFilter()
}

func (ctx *BuildContext) SetSubBuildFunc(fn func(string, string, ...string) error) {
	ctx.subBuildFunc = fn
}

func (ctx *BuildContext) SubBuild(tcName, dir string, args ...string) {
	if ctx.subBuildFunc == nil {
		vlog.Fatal("SubBuild: not available")
	}
	if err := ctx.subBuildFunc(tcName, dir, args...); err != nil {
		vlog.Fatal("SubBuild %s (tc=%s): %v", dir, tcName, err)
	}
}

func (ctx *BuildContext) Exec(name string, args ...string) {
	vlog.Info("  %s %s", name, strings.Join(args, " "))
	exec.RunFatal("", name, args...)
}

type InstallContext struct {
	ConfigAccessor
	installHolder InstallItemHolder
	pkgName       string
	prefix        string
	prefixSet     bool
}

func NewInstallContext(pkgName string, cfgVals map[string]any) *InstallContext {
	return &InstallContext{
		ConfigAccessor: NewConfigAccessor(cfgVals, nil),
		pkgName:        pkgName,
	}
}

func (ctx *InstallContext) SetPrefix(prefix string) *InstallContext {
	ctx.prefix = prefix
	ctx.prefixSet = true
	return ctx
}

func (ctx *InstallContext) Prefix() string      { return ctx.prefix }
func (ctx *InstallContext) PrefixSet() bool     { return ctx.prefixSet }
func (ctx *InstallContext) PackageName() string { return ctx.pkgName }

func (ctx *InstallContext) AddInstalls(src, dest string) *InstallContext {
	ctx.installHolder.addInstall(src, dest)
	return ctx
}

func (ctx *InstallContext) GetInstallItems() []InstallItem {
	return ctx.installHolder.getInstallItems()
}

func (ctx *InstallContext) SetInstallFilter(filter InstallFilterFunc) *InstallContext {
	ctx.installHolder.setInstallFilter(filter)
	return ctx
}

func (ctx *InstallContext) GetInstallFilter() InstallFilterFunc {
	return ctx.installHolder.getInstallFilter()
}
