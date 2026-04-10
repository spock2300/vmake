package api

type RequireFunc func(ctx *RequireContext)

type RequireInfo struct {
	Name       string
	Constraint string
}

type Requires struct {
	requires []RequireInfo
}

func (r *Requires) Add(deps ...string) {
	for _, dep := range deps {
		if dep == "" {
			continue
		}
		name, constraint := parseRequire(dep)
		r.requires = append(r.requires, RequireInfo{
			Name:       name,
			Constraint: constraint,
		})
	}
}

func (r *Requires) Get() []RequireInfo {
	return r.requires
}

func (r *Requires) Reset() {
	r.requires = make([]RequireInfo, 0)
}

type RequireContext struct {
	ConfigAccessor
	Requires
	requireFuncs []RequireFunc
}

func NewRequireContextForConfig(cfgVals map[string]any, options map[string]*Option, funcs []RequireFunc) *RequireContext {
	if options == nil {
		options = make(map[string]*Option)
	}
	accessor := ConfigAccessor{CfgVals: cfgVals, Options: options}
	return &RequireContext{
		ConfigAccessor: accessor,
		requireFuncs:   funcs,
	}
}

func (ctx *RequireContext) AddRequires(deps ...string) *RequireContext {
	ctx.Requires.Add(deps...)
	return ctx
}

func (ctx *RequireContext) GetRequires() []RequireInfo {
	return ctx.Requires.Get()
}

func (ctx *RequireContext) ResetRequires() {
	ctx.Requires.Reset()
}

func (ctx *RequireContext) GetRequireFuncs() []RequireFunc {
	return ctx.requireFuncs
}

func (ctx *RequireContext) RunFuncs() {
	for _, fn := range ctx.requireFuncs {
		fn(ctx)
	}
}

func parseRequire(dep string) (name, constraint string) {
	for i := 0; i < len(dep); i++ {
		c := dep[i]
		if c == '>' || c == '<' || c == '=' || c == '~' || c == ' ' || c == '@' {
			name = dep[:i]
			constraint = dep[i:]
			for len(constraint) > 0 && constraint[0] == ' ' {
				constraint = constraint[1:]
			}
			return
		}
	}
	return dep, ""
}
