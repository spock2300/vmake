package api

type RequireFunc func(ctx *RequireContext)

type RequireInfo struct {
	Name       string
	Constraint string
}

type RequireContext struct {
	ConfigAccessor
	requires     []RequireInfo
	requireFuncs []RequireFunc
}

func NewRequireContext() *RequireContext {
	return &RequireContext{
		ConfigAccessor: NilCfgAccessor(),
		requires:       make([]RequireInfo, 0),
	}
}

// NewRequireContextForConfig creates a RequireContext with config values.
// When cfgVals is nil (discoverAll mode), If/IfNot/Select return all values.
func NewRequireContextForConfig(cfgVals map[string]any, options map[string]*Option, funcs []RequireFunc) *RequireContext {
	if options == nil {
		options = make(map[string]*Option)
	}
	accessor := ConfigAccessor{CfgVals: cfgVals, Options: options}
	return &RequireContext{
		ConfigAccessor: accessor,
		requires:       make([]RequireInfo, 0),
		requireFuncs:   funcs,
	}
}

func (ctx *RequireContext) AddRequires(deps ...string) *RequireContext {
	for _, dep := range deps {
		if dep == "" {
			continue
		}
		name, constraint := parseRequire(dep)
		ctx.requires = append(ctx.requires, RequireInfo{
			Name:       name,
			Constraint: constraint,
		})
	}
	return ctx
}

func (ctx *RequireContext) GetRequires() []RequireInfo {
	return ctx.requires
}

func (ctx *RequireContext) ResetRequires() {
	ctx.requires = make([]RequireInfo, 0)
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
	requires []RequireInfo
}

func NewPackageRequireContext() *PackageRequireContext {
	return &PackageRequireContext{
		requires: make([]RequireInfo, 0),
	}
}

func (ctx *PackageRequireContext) AddRequires(deps ...string) *PackageRequireContext {
	for _, dep := range deps {
		if dep == "" {
			continue
		}
		name, constraint := parseRequire(dep)
		ctx.requires = append(ctx.requires, RequireInfo{
			Name:       name,
			Constraint: constraint,
		})
	}
	return ctx
}

func (ctx *PackageRequireContext) GetRequires() []RequireInfo {
	return ctx.requires
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
