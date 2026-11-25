package api

import (
	"fmt"
	"strings"

	"github.com/spock2300/vmake/internal/issue"
)

type RequireFunc func(ctx *RequireContext)

type RequireInfo struct {
	Name string
}

type Requires struct {
	requires []RequireInfo
}

func (r *Requires) Add(deps ...any) {
	for _, dep := range flattenAny(deps) {
		if dep == "" {
			continue
		}
		r.requires = append(r.requires, RequireInfo{Name: dep})
	}
}

func (r *Requires) addStrict(owner, op string, deps ...any) {
	for _, dep := range flattenAny(deps) {
		if dep == "" {
			continue
		}
		if err := ValidatePackageRef(dep); err != nil {
			fatalScript(owner, op, "%v", err)
		}
		r.requires = append(r.requires, RequireInfo{Name: dep})
	}
}

func (r *Requires) Get() []RequireInfo {
	return r.requires
}

func (r *Requires) AddInfos(infos ...RequireInfo) {
	r.requires = append(r.requires, infos...)
}

func (r *Requires) Reset() {
	r.requires = make([]RequireInfo, 0)
}

type RequireContext struct {
	ConfigAccessor
	Requires
	requireFuncs []RequireFunc
}

func NewRequireContextForConfig(pkgName string, cfgVals map[string]any, options map[string]*Option, funcs []RequireFunc) *RequireContext {
	if options == nil {
		options = make(map[string]*Option)
	}
	accessor := ConfigAccessor{cfgVals: cfgVals, options: options}
	if pkgName != "" {
		accessor.setStrictOwner(pkgName)
		accessor.discover = cfgVals == nil
	}
	return &RequireContext{
		ConfigAccessor: accessor,
		requireFuncs:   funcs,
	}
}

func (ctx *RequireContext) AddRequires(deps ...any) *RequireContext {
	ctx.Requires.addStrict(ctx.owner, "AddRequires", deps...)
	return ctx
}

func (ctx *RequireContext) GetRequires() []RequireInfo {
	return ctx.Requires.Get()
}

func (ctx *RequireContext) ResetRequires() {
	ctx.Requires.Reset()
}

func (ctx *RequireContext) getRequireFuncs() []RequireFunc {
	return ctx.requireFuncs
}

func (ctx *RequireContext) RunFuncs() {
	for _, fn := range ctx.requireFuncs {
		fn(ctx)
	}
}

func CheckCycle(path []string, current string) error {
	for i, p := range path {
		if p == current {
			return fmt.Errorf("%w: %s -> %s",
				issue.ErrCircularDependency, strings.Join(path[i:], " -> "), current)
		}
	}
	return nil
}
