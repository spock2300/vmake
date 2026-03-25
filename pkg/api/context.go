package api

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
	return ctx.GlobalOption(ModeOptionName).
		SetType(OptionChoice).
		SetDefault(ModeDebug).
		SetDescription("Build mode").
		SetValues(ModeDebug, ModeRelease)
}

type BuildContext struct {
	ConfigAccessor
	GlobalAccessor
	targets       map[string]*Target
	pkgName       string
	installItems  []InstallItem
	installFilter InstallFilterFunc
	packages      []string
}

func NewBuildContext(pkgName string, cfgVals map[string]any) *BuildContext {
	return &BuildContext{
		ConfigAccessor: NewConfigAccessor(cfgVals, nil),
		GlobalAccessor: NewGlobalAccessor(),
		targets:        make(map[string]*Target),
		pkgName:        pkgName,
		installItems:   make([]InstallItem, 0),
	}
}

func (ctx *BuildContext) Target(name string) *Target {
	if t, ok := ctx.targets[name]; ok {
		return t
	}
	t := &Target{name: name, isDefault: true}
	ctx.targets[name] = t
	return t
}

func (ctx *BuildContext) GetTargets() map[string]*Target {
	return ctx.targets
}

func (ctx *BuildContext) PackageName() string {
	return ctx.pkgName
}

func (ctx *BuildContext) IfGlobal(option string, then ...string) []string {
	if ctx.GlobalBool(option) {
		return then
	}
	return nil
}

func (ctx *BuildContext) SelectGlobal(option string, mapping map[string]string) string {
	val := ctx.GlobalString(option)
	if mapped, ok := mapping[val]; ok {
		return mapped
	}
	return ""
}

type InstallItem struct {
	Src  string
	Dest string
}

type InstallFilterFunc func(path string, isTargetOutput bool) bool

func (ctx *BuildContext) AddInstalls(src, dest string) *BuildContext {
	ctx.installItems = append(ctx.installItems, InstallItem{Src: src, Dest: dest})
	return ctx
}

func (ctx *BuildContext) GetInstallItems() []InstallItem {
	return ctx.installItems
}

func (ctx *BuildContext) SetInstallFilter(filter InstallFilterFunc) *BuildContext {
	ctx.installFilter = filter
	return ctx
}

func (ctx *BuildContext) GetInstallFilter() InstallFilterFunc {
	return ctx.installFilter
}

func (ctx *BuildContext) AddPackages(packages ...string) *BuildContext {
	ctx.packages = append(ctx.packages, packages...)
	return ctx
}

func (ctx *BuildContext) GetPackages() []string {
	return ctx.packages
}

type InstallContext struct {
	ConfigAccessor
	GlobalAccessor
	pkgName       string
	installItems  []InstallItem
	prefix        string
	prefixSet     bool
	installFilter InstallFilterFunc
}

func NewInstallContext(pkgName string, cfgVals map[string]any) *InstallContext {
	return &InstallContext{
		ConfigAccessor: NewConfigAccessor(cfgVals, nil),
		GlobalAccessor: NewGlobalAccessor(),
		pkgName:        pkgName,
		installItems:   make([]InstallItem, 0),
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
	ctx.installItems = append(ctx.installItems, InstallItem{Src: src, Dest: dest})
	return ctx
}

func (ctx *InstallContext) GetInstallItems() []InstallItem { return ctx.installItems }

func (ctx *InstallContext) SetInstallFilter(filter InstallFilterFunc) *InstallContext {
	ctx.installFilter = filter
	return ctx
}

func (ctx *InstallContext) GetInstallFilter() InstallFilterFunc {
	return ctx.installFilter
}
