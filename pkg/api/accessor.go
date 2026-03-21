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
	if a.Bool(option) {
		return then
	}
	return nil
}

func (a *ConfigAccessor) IfNot(option string, then ...string) []string {
	if !a.Bool(option) {
		return then
	}
	return nil
}

func (a *ConfigAccessor) Select(option string, mapping map[string]string) string {
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
