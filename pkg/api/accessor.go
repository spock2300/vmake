package api

func getTypedValue[T any](
	vals map[string]any,
	opts map[string]*Option,
	name string,
	getDefault func(*Option) T,
	zero T,
	coerce func(any) (T, bool),
) T {
	if val, ok := vals[name]; ok {
		if v, ok := val.(T); ok {
			return v
		}
		if coerce != nil {
			if v, ok := coerce(val); ok {
				return v
			}
		}
	}
	if opt, ok := opts[name]; ok {
		return getDefault(opt)
	}
	return zero
}

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
	return getTypedValue(a.CfgVals, a.Options, name, func(o *Option) bool {
		if d, ok := o.defaultVal.(bool); ok {
			return d
		}
		return false
	}, false, nil)
}

func (a *ConfigAccessor) String(name string) string {
	return getTypedValue(a.CfgVals, a.Options, name, func(o *Option) string {
		if d, ok := o.defaultVal.(string); ok {
			return d
		}
		return ""
	}, "", nil)
}

func (a *ConfigAccessor) Int(name string) int {
	return getTypedValue(a.CfgVals, a.Options, name, func(o *Option) int {
		if d, ok := o.defaultVal.(int); ok {
			return d
		}
		return 0
	}, 0, func(val any) (int, bool) {
		switch v := val.(type) {
		case int:
			return v, true
		case int64:
			return int(v), true
		case float64:
			return int(v), true
		}
		return 0, false
	})
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
	return getTypedValue(g.globalVals, g.globalOptions, name, func(o *Option) bool {
		if d, ok := o.Default().(bool); ok {
			return d
		}
		return false
	}, false, nil)
}

func (g *GlobalAccessor) GlobalString(name string) string {
	return getTypedValue(g.globalVals, g.globalOptions, name, func(o *Option) string {
		if d, ok := o.Default().(string); ok {
			return d
		}
		return ""
	}, "", nil)
}

func (g *GlobalAccessor) Mode() string {
	return g.GlobalString(ModeOptionName)
}
