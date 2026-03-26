package main

import (
	"os"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.SubBuild("host", "./tools")

		os.MkdirAll("output", 0755)
		ctx.Exec("tools/build/host-debug/codegen", "output/generated.h")

		ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddCFlags("-I.")
	})
}
