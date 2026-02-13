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

type BuildContext struct {
	targets map[string]*Target
	pkgName string
	cfgVals map[string]any
	options map[string]*Option
}

func NewBuildContext(pkgName string, cfgVals map[string]any) *BuildContext {
	if cfgVals == nil {
		cfgVals = make(map[string]any)
	}
	return &BuildContext{
		targets: make(map[string]*Target),
		pkgName: pkgName,
		cfgVals: cfgVals,
		options: make(map[string]*Option),
	}
}

func (ctx *BuildContext) SetOptions(options map[string]*Option) {
	ctx.options = options
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
