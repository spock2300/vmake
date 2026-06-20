package main

import (
	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.BuildSubGraph("t15lib")

		sublibPath := ctx.DepOutput("t15lib:sublib")
		ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddIncludes("t15lib/include").
			AddLdFlags(sublibPath)
	})
}
