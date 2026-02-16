package api

import (
	"strings"
)

type ConfigContext struct {
	options map[string]*Option
	pkgName string
	cfgVals map[string]any
}

func NewConfigContext(pkgName string) *ConfigContext {
	return &ConfigContext{
		options: make(map[string]*Option),
		pkgName: pkgName,
		cfgVals: make(map[string]any),
	}
}

func (ctx *ConfigContext) Option(name string) *Option {
	if opt, ok := ctx.options[name]; ok {
		return opt
	}
	opt := &Option{name: name}
	ctx.options[name] = opt
	return opt
}

func (ctx *ConfigContext) Bool(name string) bool {
	if val, ok := ctx.cfgVals[name]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	if opt, ok := ctx.options[name]; ok {
		if d, ok := opt.defaultVal.(bool); ok {
			return d
		}
	}
	return false
}

func (ctx *ConfigContext) String(name string) string {
	if val, ok := ctx.cfgVals[name]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	if opt, ok := ctx.options[name]; ok {
		if d, ok := opt.defaultVal.(string); ok {
			return d
		}
	}
	return ""
}

func (ctx *ConfigContext) Int(name string) int {
	if val, ok := ctx.cfgVals[name]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	if opt, ok := ctx.options[name]; ok {
		switch v := opt.defaultVal.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return 0
}

func (ctx *ConfigContext) SetConfigValue(name string, val any) {
	ctx.cfgVals[name] = val
}

func (ctx *ConfigContext) GetOptions() map[string]*Option {
	return ctx.options
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
	targets       map[string]*Target
	pkgName       string
	cfgVals       map[string]any
	options       map[string]*Option
	globalVals    map[string]any
	globalOptions map[string]*Option
	installItems  []InstallItem
	installFilter InstallFilterFunc
	packages      []string
}

func NewBuildContext(pkgName string, cfgVals map[string]any) *BuildContext {
	if cfgVals == nil {
		cfgVals = make(map[string]any)
	}
	return &BuildContext{
		targets:       make(map[string]*Target),
		pkgName:       pkgName,
		cfgVals:       cfgVals,
		options:       make(map[string]*Option),
		globalVals:    make(map[string]any),
		globalOptions: make(map[string]*Option),
		installItems:  make([]InstallItem, 0),
	}
}

func (ctx *BuildContext) SetOptions(options map[string]*Option) {
	ctx.options = options
}

func (ctx *BuildContext) SetGlobalOptions(options map[string]*Option) {
	ctx.globalOptions = options
}

func (ctx *BuildContext) SetGlobalValues(vals map[string]any) {
	ctx.globalVals = vals
}

func (ctx *BuildContext) Target(name string) *Target {
	if t, ok := ctx.targets[name]; ok {
		return t
	}
	t := &Target{name: name, isDefault: true}
	ctx.targets[name] = t
	return t
}

func (ctx *BuildContext) If(option string, then ...string) []string {
	if ctx.Bool(option) {
		return then
	}
	return nil
}

func (ctx *BuildContext) IfNot(option string, then ...string) []string {
	if !ctx.Bool(option) {
		return then
	}
	return nil
}

func (ctx *BuildContext) Select(option string, mapping map[string]string) string {
	val := ctx.String(option)
	if mapped, ok := mapping[val]; ok {
		return mapped
	}
	return ""
}

func (ctx *BuildContext) When(option string, value any) bool {
	val := ctx.cfgVals[option]
	return val == value
}

func (ctx *BuildContext) Bool(name string) bool {
	if val, ok := ctx.cfgVals[name]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	if opt, ok := ctx.options[name]; ok {
		if d, ok := opt.defaultVal.(bool); ok {
			return d
		}
	}
	return false
}

func (ctx *BuildContext) String(name string) string {
	if val, ok := ctx.cfgVals[name]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	if opt, ok := ctx.options[name]; ok {
		if d, ok := opt.defaultVal.(string); ok {
			return d
		}
	}
	return ""
}

func (ctx *BuildContext) Int(name string) int {
	if val, ok := ctx.cfgVals[name]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	if opt, ok := ctx.options[name]; ok {
		switch v := opt.defaultVal.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return 0
}

func (ctx *BuildContext) GetTargets() map[string]*Target {
	return ctx.targets
}

func (ctx *BuildContext) PackageName() string {
	return ctx.pkgName
}

func (ctx *BuildContext) GlobalBool(name string) bool {
	if val, ok := ctx.globalVals[name]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	if opt, ok := ctx.globalOptions[name]; ok {
		if d, ok := opt.Default().(bool); ok {
			return d
		}
	}
	return false
}

func (ctx *BuildContext) GlobalString(name string) string {
	if val, ok := ctx.globalVals[name]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	if opt, ok := ctx.globalOptions[name]; ok {
		if d, ok := opt.Default().(string); ok {
			return d
		}
	}
	return ""
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

func (ctx *BuildContext) Mode() string {
	return ctx.GlobalString(ModeOptionName)
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
	pkgName       string
	cfgVals       map[string]any
	options       map[string]*Option
	globalVals    map[string]any
	globalOptions map[string]*Option
	installItems  []InstallItem
	prefix        string
	prefixSet     bool
	installFilter InstallFilterFunc
}

func NewInstallContext(pkgName string, cfgVals map[string]any) *InstallContext {
	if cfgVals == nil {
		cfgVals = make(map[string]any)
	}
	return &InstallContext{
		pkgName:       pkgName,
		cfgVals:       cfgVals,
		options:       make(map[string]*Option),
		globalVals:    make(map[string]any),
		globalOptions: make(map[string]*Option),
		installItems:  make([]InstallItem, 0),
	}
}

func (ctx *InstallContext) SetOptions(options map[string]*Option) {
	ctx.options = options
}

func (ctx *InstallContext) SetGlobalOptions(options map[string]*Option) {
	ctx.globalOptions = options
}

func (ctx *InstallContext) SetGlobalValues(vals map[string]any) {
	ctx.globalVals = vals
}

func (ctx *InstallContext) SetPrefix(prefix string) {
	ctx.prefix = prefix
	ctx.prefixSet = true
}

func (ctx *InstallContext) Prefix() string      { return ctx.prefix }
func (ctx *InstallContext) PrefixSet() bool     { return ctx.prefixSet }
func (ctx *InstallContext) PackageName() string { return ctx.pkgName }

func (ctx *InstallContext) AddInstalls(src, dest string) {
	ctx.installItems = append(ctx.installItems, InstallItem{Src: src, Dest: dest})
}

func (ctx *InstallContext) GetInstallItems() []InstallItem { return ctx.installItems }

func (ctx *InstallContext) SetInstallFilter(filter InstallFilterFunc) {
	ctx.installFilter = filter
}

func (ctx *InstallContext) GetInstallFilter() InstallFilterFunc {
	return ctx.installFilter
}

func (ctx *InstallContext) Bool(name string) bool {
	if val, ok := ctx.cfgVals[name]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	if opt, ok := ctx.options[name]; ok {
		if d, ok := opt.defaultVal.(bool); ok {
			return d
		}
	}
	return false
}

func (ctx *InstallContext) String(name string) string {
	if val, ok := ctx.cfgVals[name]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	if opt, ok := ctx.options[name]; ok {
		if d, ok := opt.defaultVal.(string); ok {
			return d
		}
	}
	return ""
}

func (ctx *InstallContext) GlobalBool(name string) bool {
	if val, ok := ctx.globalVals[name]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	if opt, ok := ctx.globalOptions[name]; ok {
		if d, ok := opt.Default().(bool); ok {
			return d
		}
	}
	return false
}

func (ctx *InstallContext) GlobalString(name string) string {
	if val, ok := ctx.globalVals[name]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	if opt, ok := ctx.globalOptions[name]; ok {
		if d, ok := opt.Default().(string); ok {
			return d
		}
	}
	return ""
}

func (ctx *InstallContext) Mode() string {
	return ctx.GlobalString(ModeOptionName)
}

func flattenStrings(slices ...[]string) []string {
	var result []string
	for _, s := range slices {
		for _, item := range s {
			if item != "" && !containsString(result, item) {
				result = append(result, item)
			}
		}
	}
	return result
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func ExpandGlob(pattern string) []string {
	if !strings.Contains(pattern, "*") {
		return []string{pattern}
	}
	return []string{pattern}
}
