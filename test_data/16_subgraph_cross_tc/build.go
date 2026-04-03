package main

import (
	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.BuildSubGraph("t16lib")

		sublibPath := ctx.DepOutput("t16lib:sublib")
		ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddIncludes("t16lib/include").
			AddLdFlags(sublibPath)
	})
}
