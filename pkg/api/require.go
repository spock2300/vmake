package api

type RequireFunc func(ctx *RequireContext)

type RequireInfo struct {
	Name       string
	Constraint string
}

type RequireContext struct {
	requires []RequireInfo
}

func NewRequireContext() *RequireContext {
	return &RequireContext{
		requires: make([]RequireInfo, 0),
	}
}

func (ctx *RequireContext) AddRequires(deps ...string) {
	for _, dep := range deps {
		name, constraint := parseRequire(dep)
		ctx.requires = append(ctx.requires, RequireInfo{
			Name:       name,
			Constraint: constraint,
		})
	}
}

func (ctx *RequireContext) GetRequires() []RequireInfo {
	return ctx.requires
}

type PackageRequireContext struct {
	requires []RequireInfo
}

func NewPackageRequireContext() *PackageRequireContext {
	return &PackageRequireContext{
		requires: make([]RequireInfo, 0),
	}
}

func (ctx *PackageRequireContext) AddRequires(deps ...string) {
	for _, dep := range deps {
		name, constraint := parseRequire(dep)
		ctx.requires = append(ctx.requires, RequireInfo{
			Name:       name,
			Constraint: constraint,
		})
	}
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
