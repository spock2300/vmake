package main

import (
	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.BuildSubGraph("sublib")

		sublibPath := ctx.DepOutput("sublib:sublib")
		ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddIncludes("sublib/include").
			AddLdFlags(sublibPath)
	})
}
