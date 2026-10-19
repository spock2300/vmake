package main

import (
	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.SetRoot(true)
	p.AddRequires("lib_a")

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("root_app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			UseRequires().
			AddLdFlags(ctx.DepOutput("lib_a:lib_a"))
	})
}
