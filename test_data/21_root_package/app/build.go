package main

import (
	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("lib_a")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("root_app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddLdFlags(ctx.DepOutput("lib_a:lib_a"))
	})
}
