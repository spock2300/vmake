package api

type RequireFunc func(ctx *RequireContext)

type RequireInfo struct {
	Name       string
	Constraint string
}

type requiresHolder struct {
	requires []RequireInfo
}

func (h *requiresHolder) addRequires(deps ...string) {
	for _, dep := range deps {
		if dep == "" {
			continue
		}
		name, constraint := parseRequire(dep)
		h.requires = append(h.requires, RequireInfo{
			Name:       name,
			Constraint: constraint,
		})
	}
}

func (h *requiresHolder) getRequires() []RequireInfo {
	return h.requires
}

func (h *requiresHolder) resetRequires() {
	h.requires = make([]RequireInfo, 0)
}

type RequireContext struct {
	ConfigAccessor
	requiresHolder
	requireFuncs []RequireFunc
}

func NewRequireContext() *RequireContext {
	return &RequireContext{
		ConfigAccessor: NilCfgAccessor(),
		requireFuncs:   make([]RequireFunc, 0),
	}
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
	ctx.requiresHolder.addRequires(deps...)
	return ctx
}

func (ctx *RequireContext) GetRequires() []RequireInfo {
	return ctx.requiresHolder.getRequires()
}

func (ctx *RequireContext) ResetRequires() {
	ctx.requiresHolder.resetRequires()
}

func (ctx *RequireContext) GetRequireFuncs() []RequireFunc {
	return ctx.requireFuncs
}

func (ctx *RequireContext) RunFuncs() {
	for _, fn := range ctx.requireFuncs {
		fn(ctx)
	}
}

type PackageRequireContext struct {
	requiresHolder
}

func NewPackageRequireContext() *PackageRequireContext {
	return &PackageRequireContext{}
}

func (ctx *PackageRequireContext) AddRequires(deps ...string) *PackageRequireContext {
	ctx.requiresHolder.addRequires(deps...)
	return ctx
}

func (ctx *PackageRequireContext) GetRequires() []RequireInfo {
	return ctx.requiresHolder.getRequires()
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
