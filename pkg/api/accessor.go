package api

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
)

type ConfigAccessor struct {
	CfgVals  map[string]any
	Options  map[string]*Option
	owner    string
	strict   bool
	discover bool

	lookup        func(pkg, name string) (any, *Option, error)
	lenientCross  bool
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

func (a *ConfigAccessor) SetConfigLookup(fn func(pkg, name string) (any, *Option, error)) *ConfigAccessor {
	a.lookup = fn
	return a
}

func configRef(name string) (pkg, opt string, cross bool, err error) {
	index := strings.LastIndex(name, "/")
	if index < 0 {
		return "", "", false, nil
	}
	if index == 0 || index == len(name)-1 {
		return "", "", true, fmt.Errorf("invalid cross-package option reference %q", name)
	}
	return name[:index], name[index+1:], true, nil
}

func (a *ConfigAccessor) crossUnavailable(op, name string) {
	if a.discover {
		fatalScript(a.owner, op, "cross-package option %q is not available during dependency discovery; use a global option or move the read to OnBuild", name)
	}
	fatalScript(a.owner, op, "cross-package option %q is only available in OnBuild; declare a GlobalOption or read it in the owning package", name)
}

func (a *ConfigAccessor) resolveCross(name, op string, checkAccessor bool) (val any, opt *Option, hasVal, cross bool) {
	pkg, optName, isCross, err := configRef(name)
	if !isCross {
		return nil, nil, false, false
	}
	if err != nil {
		if a.lenientCross {
			return nil, nil, false, true
		}
		fatalScript(a.owner, op, "%v", err)
	}
	if a.lenientCross {
		return nil, nil, false, true
	}
	if a.lookup == nil {
		a.crossUnavailable(op, name)
	}
	val, opt, err = a.lookup(pkg, optName)
	if err != nil {
		fatalScript(a.owner, op, "read %q: %v", name, err)
	}
	if opt == nil {
		fatalScript(a.owner, op, "unknown option %q", name)
	}
	if checkAccessor {
		a.checkAccessorType(opt, op, name)
	}
	if val != nil {
		a.checkValueType(opt, val, name, op)
	}
	return val, opt, val != nil, true
}

func (a *ConfigAccessor) resolvedValue(name, op string, checkAccessor bool) (any, *Option, bool) {
	if val, opt, hasVal, cross := a.resolveCross(name, op, checkAccessor); cross {
		return val, opt, hasVal
	}
	if checkAccessor {
		a.valueRead(name, op)
	} else if a.strict && a.owner != "" {
		if _, hasVal := a.CfgVals[name]; !hasVal {
			if _, hasOpt := a.Options[name]; !hasOpt {
				fatalScript(a.owner, op, "unknown option %q (fix the name or declare it via ctx.Option first)", name)
			}
		}
		if opt := a.Options[name]; opt != nil {
			if val := a.CfgVals[name]; val != nil {
				a.checkValueType(opt, val, name, op)
			}
		}
	}
	val, hasVal := a.CfgVals[name]
	return val, a.Options[name], hasVal
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

func getTypedValueResolved[T any](
	val any,
	opt *Option,
	hasVal bool,
	getDefault func(*Option) T,
	zero T,
	coerce func(any) (T, bool),
) T {
	if hasVal {
		if v, ok := val.(T); ok {
			return v
		}
		if coerce != nil {
			if v, ok := coerce(val); ok {
				return v
			}
		}
	}
	if opt != nil {
		return getDefault(opt)
	}
	return zero
}

func boolDefault(o *Option) bool {
	if d, ok := o.defaultVal.(bool); ok {
		return d
	}
	return false
}

func stringDefault(o *Option) string {
	if d, ok := o.defaultVal.(string); ok {
		return d
	}
	return ""
}

func intDefault(o *Option) int {
	if d, ok := o.defaultVal.(int); ok {
		return d
	}
	return 0
}

func coerceInt(val any) (int, bool) {
	switch v := val.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	}
	return 0, false
}

func (a *ConfigAccessor) Bool(name string) bool {
	val, opt, hasVal := a.resolvedValue(name, "Bool", true)
	return getTypedValueResolved(val, opt, hasVal, boolDefault, false, nil)
}

func (a *ConfigAccessor) String(name string) string {
	val, opt, hasVal := a.resolvedValue(name, "String", true)
	return getTypedValueResolved(val, opt, hasVal, stringDefault, "", nil)
}

func (a *ConfigAccessor) Int(name string) int {
	val, opt, hasVal := a.resolvedValue(name, "Int", true)
	return getTypedValueResolved(val, opt, hasVal, intDefault, 0, coerceInt)
}

func (a *ConfigAccessor) BoolStr(name string) string {
	if a.Bool(name) {
		return "ON"
	}
	return "OFF"
}

func (a *ConfigAccessor) boolQuiet(name string) bool {
	val, hasVal := a.CfgVals[name]
	return getTypedValueResolved(val, a.Options[name], hasVal, boolDefault, false, nil)
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
	if a.discover {
		if _, _, cross, _ := configRef(option); cross {
			a.crossUnavailable("If", option)
		}
		return a.ifCond(a.boolQuiet(option), then...)
	}
	return a.ifCond(a.Bool(option), then...)
}

func (a *ConfigAccessor) Select(option string, mapping map[string]string) []string {
	if a.discover {
		if _, _, cross, _ := configRef(option); cross {
			a.crossUnavailable("Select", option)
		}
	}
	if a.CfgVals == nil {
		seen := make(map[string]bool, len(mapping))
		vals := make([]string, 0, len(mapping))
		for _, v := range mapping {
			if v != "" && !seen[v] {
				seen[v] = true
				vals = append(vals, v)
			}
		}
		sort.Strings(vals)
		return vals
	}
	val, opt, hasVal := a.resolvedValue(option, "String", true)
	selected := getTypedValueResolved(val, opt, hasVal, stringDefault, "", nil)
	if a.strict && a.owner != "" {
		if opt != nil && opt.Type() == OptionChoice && len(opt.Values()) > 0 {
			for k := range mapping {
				if !slices.Contains(opt.Values(), k) {
					fatalScript(a.owner, "Select", "option %q choice mapping key %q is not in SetValues(%v)", option, k, opt.Values())
				}
			}
		}
	}
	if mapped, ok := mapping[selected]; ok && mapped != "" {
		return []string{mapped}
	}
	return nil
}

func (a *ConfigAccessor) When(option string, values ...any) bool {
	return a.matchOptionValues(option, "When", values)
}

func (a *ConfigAccessor) WhenNot(option string, values ...any) bool {
	matched := a.matchOptionValues(option, "WhenNot", values)
	if _, _, cross, _ := configRef(option); cross {
		return !matched
	}
	if a.CfgVals == nil {
		return true
	}
	return !matched
}

func (a *ConfigAccessor) WhenAny(option string, values ...any) bool {
	return a.When(option, values...)
}

func (a *ConfigAccessor) matchOptionValues(option, method string, values []any) bool {
	if len(values) == 0 {
		fatalScript(a.owner, method, "option %q requires at least one comparison value", option)
	}
	_, _, cross, _ := configRef(option)
	if a.discover {
		if cross {
			a.crossUnavailable(method, option)
		}
		return true
	}
	if !cross && a.CfgVals == nil {
		return true
	}
	val, opt, hasVal := a.resolvedValue(option, method, false)
	if opt != nil {
		for _, value := range values {
			a.checkValueType(opt, value, option, method)
		}
	}
	if !hasVal && opt != nil {
		val = opt.Default()
	}
	for _, value := range values {
		if valuesEqual(val, value) {
			return true
		}
	}
	return false
}

func ValuesEqual(a, b any) bool {
	return valuesEqual(a, b)
}

func ValidateOptionValue(opt *Option, val any) error {
	if opt == nil {
		return nil
	}
	if val == nil {
		return fmt.Errorf("option %q value must not be nil", opt.Name())
	}
	switch opt.Type() {
	case OptionBool:
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("option %q is bool, got %T value %v", opt.Name(), val, val)
		}
	case OptionInt:
		if _, ok := exactIntValue(val); !ok {
			return fmt.Errorf("option %q is int, got %T value %v", opt.Name(), val, val)
		}
	case OptionString, OptionChoice:
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("option %q is %s, got %T value %v", opt.Name(), opt.Type(), val, val)
		}
		if opt.Type() == OptionChoice && len(opt.Values()) > 0 && !slices.Contains(opt.Values(), s) {
			return fmt.Errorf("option %q value %q is not in SetValues(%v)", opt.Name(), s, opt.Values())
		}
	}
	return nil
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
	if strings.Contains(name, "/") {
		fatalScript(a.owner, "Option", "option name %q must not contain '/'; cross-package reads are read-only, use ctx.Bool(%q)", name, "pkg/option")
	}
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
	if strings.Contains(o.name, "/") {
		return fmt.Errorf("option name %q must not contain '/'", o.name)
	}
	if o.macroName != "" {
		if strings.Contains(o.macroName, "%") {
			if strings.Count(o.macroName, "%") != 1 || (!strings.Contains(o.macroName, "%s") && !strings.Contains(o.macroName, "%v")) {
				return fmt.Errorf("option %q: SetMacroName template %q must contain exactly one %%s or %%v verb", o.name, o.macroName)
			}
			if o.optType == OptionChoice {
				for _, v := range o.values {
					if !validMacroName(fmt.Sprintf(o.macroName, v)) {
						return fmt.Errorf("option %q: SetMacroName %q renders invalid macro name for value %q", o.name, o.macroName, v)
					}
				}
			}
		} else if !validMacroName(o.macroName) {
			return fmt.Errorf("option %q: SetMacroName %q is not a valid C identifier", o.name, o.macroName)
		}
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
