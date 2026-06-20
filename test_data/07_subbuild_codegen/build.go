package main

import (
	"os"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.BuildSubGraph("tools")

		os.MkdirAll("output", 0755)
		ctx.Exec(ctx.DepOutput("tools:codegen"), "output/generated.h")

		ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddCFlags("-I.")
	})
}
