package api

import (
	"fmt"
	"math"
)

type ConfigAccessor struct {
	CfgVals  map[string]any
	Options  map[string]*Option
	owner    string
	strict   bool
	discover bool

	optionsLocked bool
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

func (a *ConfigAccessor) setStrictOwner(owner string) {
	a.owner = owner
	a.strict = true
}

func (a *ConfigAccessor) SetCfgVals(vals map[string]any) *ConfigAccessor {
	a.CfgVals = vals
	return a
}

func (a *ConfigAccessor) valueRead(name, op string) {
	if !a.strict || a.owner == "" {
		return
	}
	if a.discover {
		fatalScript(a.owner, op, "option values are unavailable during dependency discovery; use ctx.When(%q, v) or ctx.If(%q, ...) for discover-aware reads", name, name)
	}
	val, hasVal := a.CfgVals[name]
	opt, hasOpt := a.Options[name]
	if !hasVal && !hasOpt {
		fatalScript(a.owner, op, "unknown option %q (fix the name or declare it via ctx.Option first)", name)
	}
	if hasOpt {
		a.checkAccessorType(opt, op, name)
		if hasVal {
			a.checkValueType(opt, val, name, op)
		}
	}
}

func (a *ConfigAccessor) checkAccessorType(opt *Option, op, name string) {
	var want string
	switch opt.Type() {
	case OptionBool:
		want = "Bool"
	case OptionInt:
		want = "Int"
	case OptionString, OptionChoice:
		want = "String"
	}
	if want != "" && want != op {
		fatalScript(a.owner, op, "option %q is declared as %s; use ctx.%s(...) to read it", name, opt.Type(), want)
	}
}

func (a *ConfigAccessor) checkValueType(opt *Option, val any, name, op string) {
	switch opt.Type() {
	case OptionBool:
		if _, ok := val.(bool); !ok {
			fatalScript(a.owner, op, "option %q is bool, got %T value %v", name, val, val)
		}
	case OptionInt:
		switch val.(type) {
		case int, int64, float64:
		default:
			fatalScript(a.owner, op, "option %q is int, got %T value %v", name, val, val)
		}
	case OptionString, OptionChoice:
		if _, ok := val.(string); !ok {
			fatalScript(a.owner, op, "option %q is string, got %T value %v", name, val, val)
		}
	}
}

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

func (a *ConfigAccessor) Bool(name string) bool {
	a.valueRead(name, "Bool")
	return getTypedValue(a.CfgVals, a.Options, name, func(o *Option) bool {
		if d, ok := o.defaultVal.(bool); ok {
			return d
		}
		return false
	}, false, nil)
}

func (a *ConfigAccessor) String(name string) string {
	a.valueRead(name, "String")
	return getTypedValue(a.CfgVals, a.Options, name, func(o *Option) string {
		if d, ok := o.defaultVal.(string); ok {
			return d
		}
		return ""
	}, "", nil)
}

func (a *ConfigAccessor) Int(name string) int {
	a.valueRead(name, "Int")
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

func (a *ConfigAccessor) boolQuiet(name string) bool {
	return getTypedValue(a.CfgVals, a.Options, name, func(o *Option) bool {
		if d, ok := o.defaultVal.(bool); ok {
			return d
		}
		return false
	}, false, nil)
}

func (a *ConfigAccessor) ifCond(cond bool, then ...string) []string {
	if a.CfgVals == nil {
		return then
	}
	if cond {
		return then
	}
	return nil
}

func (a *ConfigAccessor) If(option string, then ...string) []string {
	if !a.discover {
		a.valueRead(option, "Bool")
	}
	return a.ifCond(a.boolQuiet(option), then...)
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
	if a.CfgVals == nil {
		return true
	}
	if a.strict && a.owner != "" {
		opt, hasOpt := a.Options[option]
		if _, hasVal := a.CfgVals[option]; !hasVal && !hasOpt {
			fatalScript(a.owner, "When", "unknown option %q (fix the name or declare it via ctx.Option first)", option)
		}
		if hasOpt {
			a.checkValueType(opt, value, option, "When")
		}
	}
	val := a.CfgVals[option]
	if val == nil {
		if opt, ok := a.Options[option]; ok {
			val = opt.Default()
		}
	}
	return valuesEqual(val, value)
}

func valuesEqual(a, b any) bool {
	if a == b {
		return true
	}
	if ai, aok := exactIntValue(a); aok {
		if bi, bok := exactIntValue(b); bok {
			return ai == bi
		}
	}
	af, aok := numericValue(a)
	bf, bok := numericValue(b)
	if aok && bok {
		return af == bf
	}
	return false
}

func exactIntValue(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n == math.Trunc(n) && n >= -9.223372036854776e18 && n < 9.223372036854776e18 {
			return int64(n), true
		}
	}
	return 0, false
}

func numericValue(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func (a *ConfigAccessor) Option(name string) *Option {
	if a.optionsLocked {
		fatalScript(a.owner, "Option", "option %q declared after the config phase; declare options in OnConfig", name)
	}
	if opt, ok := a.Options[name]; ok {
		return opt
	}
	opt := &Option{name: name}
	a.Options[name] = opt
	return opt
}

// LockOptionDeclaration marks the config phase as finished. Option() calls
// after this point fatal: late-declared options never reach option
// resolution and would be silently ignored.
func (a *ConfigAccessor) LockOptionDeclaration() {
	a.optionsLocked = true
}

func (a *ConfigAccessor) SetOptions(options map[string]*Option) *ConfigAccessor {
	a.Options = options
	return a
}

func (a *ConfigAccessor) MergeGlobals(globalOptions map[string]*Option, globalVals map[string]any) {
	if a.Options == nil {
		a.Options = make(map[string]*Option)
	}
	if a.CfgVals == nil {
		a.CfgVals = make(map[string]any)
	}
	mergeMapNoOverwrite(a.Options, globalOptions)
	mergeMapNoOverwrite(a.CfgVals, globalVals)
}

func NormalizeOptionValue(opt *Option, val any) any {
	if opt == nil || val == nil {
		return val
	}
	switch opt.Type() {
	case OptionInt:
		switch v := val.(type) {
		case float64:
			return int(v)
		case int64:
			return int(v)
		}
	}
	return val
}

func ValidateOption(o *Option) error {
	if o == nil {
		return nil
	}
	if o.defaultVal == nil {
		return nil
	}
	switch o.optType {
	case OptionBool:
		if _, ok := o.defaultVal.(bool); !ok {
			return fmt.Errorf("option %q: SetDefault value %T does not match type OptionBool; call SetType before SetDefault or fix the value", o.name, o.defaultVal)
		}
	case OptionInt:
		if _, ok := o.defaultVal.(int); !ok {
			return fmt.Errorf("option %q: SetDefault value %T does not match type OptionInt", o.name, o.defaultVal)
		}
	case OptionString, OptionChoice:
		s, ok := o.defaultVal.(string)
		if !ok {
			return fmt.Errorf("option %q: SetDefault value %T does not match type %s", o.name, o.defaultVal, o.optType)
		}
		if o.optType == OptionChoice && s != "" && len(o.values) > 0 {
			found := false
			for _, v := range o.values {
				if v == s {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("option %q: default %q is not in SetValues(%v)", o.name, s, o.values)
			}
		}
	}
	return nil
}
