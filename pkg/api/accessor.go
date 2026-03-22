package api

type ConfigAccessor struct {
	CfgVals map[string]any
	Options map[string]*Option
}

func NewConfigAccessor(cfgVals map[string]any, options map[string]*Option) ConfigAccessor {
	if cfgVals == nil {
		cfgVals = make(map[string]any)
	}
	if options == nil {
		options = make(map[string]*Option)
	}
	return ConfigAccessor{CfgVals: cfgVals, Options: options}
}

// NilCfgAccessor creates a ConfigAccessor with nil CfgVals.
// Used by RequireContext in discoverAll mode: If/IfNot/Select
// return all possible values when CfgVals is nil.
func NilCfgAccessor() ConfigAccessor {
	return ConfigAccessor{CfgVals: nil, Options: make(map[string]*Option)}
}

func (a *ConfigAccessor) Bool(name string) bool {
	if val, ok := a.CfgVals[name]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	if opt, ok := a.Options[name]; ok {
		if d, ok := opt.defaultVal.(bool); ok {
			return d
		}
	}
	return false
}

func (a *ConfigAccessor) String(name string) string {
	if val, ok := a.CfgVals[name]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	if opt, ok := a.Options[name]; ok {
		if d, ok := opt.defaultVal.(string); ok {
			return d
		}
	}
	return ""
}

func (a *ConfigAccessor) Int(name string) int {
	if val, ok := a.CfgVals[name]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	if opt, ok := a.Options[name]; ok {
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

func (a *ConfigAccessor) BoolStr(name string) string {
	if a.Bool(name) {
		return "ON"
	}
	return "OFF"
}

func (a *ConfigAccessor) If(option string, then ...string) []string {
	if a.CfgVals == nil {
		return then
	}
	if a.Bool(option) {
		return then
	}
	return nil
}

func (a *ConfigAccessor) IfNot(option string, then ...string) []string {
	if a.CfgVals == nil {
		return then
	}
	if !a.Bool(option) {
		return then
	}
	return nil
}

// Equal returns dep when CfgVals[option] == value. In discoverAll mode (CfgVals==nil), always returns dep.
func (a *ConfigAccessor) Equal(option, value, dep string) string {
	if a.CfgVals == nil {
		return dep
	}
	if a.String(option) == value {
		return dep
	}
	return ""
}

func (a *ConfigAccessor) Select(option string, mapping map[string]string) string {
	if a.CfgVals == nil {
		return ""
	}
	val := a.String(option)
	if mapped, ok := mapping[val]; ok {
		return mapped
	}
	return ""
}

func (a *ConfigAccessor) When(option string, value any) bool {
	val := a.CfgVals[option]
	return val == value
}

func (a *ConfigAccessor) Option(name string) *Option {
	if opt, ok := a.Options[name]; ok {
		return opt
	}
	opt := &Option{name: name}
	a.Options[name] = opt
	return opt
}

func (a *ConfigAccessor) SetOptions(options map[string]*Option) {
	a.Options = options
}

type GlobalAccessor struct {
	globalVals    map[string]any
	globalOptions map[string]*Option
}

func NewGlobalAccessor() GlobalAccessor {
	return GlobalAccessor{
		globalVals:    make(map[string]any),
		globalOptions: make(map[string]*Option),
	}
}

func (g *GlobalAccessor) SetGlobalOptions(options map[string]*Option) {
	g.globalOptions = options
}

func (g *GlobalAccessor) SetGlobalValues(vals map[string]any) {
	g.globalVals = vals
}

func (g *GlobalAccessor) GlobalBool(name string) bool {
	if val, ok := g.globalVals[name]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	if opt, ok := g.globalOptions[name]; ok {
		if d, ok := opt.Default().(bool); ok {
			return d
		}
	}
	return false
}

func (g *GlobalAccessor) GlobalString(name string) string {
	if val, ok := g.globalVals[name]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	if opt, ok := g.globalOptions[name]; ok {
		if d, ok := opt.Default().(string); ok {
			return d
		}
	}
	return ""
}

func (g *GlobalAccessor) Mode() string {
	return g.GlobalString(ModeOptionName)
}
