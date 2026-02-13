package api

type Option struct {
	name        string
	optType     OptionType
	defaultVal  any
	description string
	values      []string
	showIf      func(ctx *ConfigContext) bool
	group       string
}

func (o *Option) SetType(t OptionType) *Option {
	o.optType = t
	return o
}

func (o *Option) SetDefault(v any) *Option {
	o.defaultVal = v
	return o
}

func (o *Option) SetDescription(desc string) *Option {
	o.description = desc
	return o
}

func (o *Option) SetValues(vals ...string) *Option {
	o.values = vals
	return o
}

func (o *Option) SetShowIf(fn func(ctx *ConfigContext) bool) *Option {
	o.showIf = fn
	return o
}

func (o *Option) SetGroup(group string) *Option {
	o.group = group
	return o
}

func (o *Option) Name() string                          { return o.name }
func (o *Option) Type() OptionType                      { return o.optType }
func (o *Option) Default() any                          { return o.defaultVal }
func (o *Option) Description() string                   { return o.description }
func (o *Option) Values() []string                      { return o.values }
func (o *Option) ShowIf() func(ctx *ConfigContext) bool { return o.showIf }
func (o *Option) Group() string                         { return o.group }
