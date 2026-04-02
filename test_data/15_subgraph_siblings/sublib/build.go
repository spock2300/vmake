package main

import (
	"os"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.BuildSubGraph("subgen")

		os.MkdirAll("include", 0755)
		ctx.Exec(ctx.DepOutput("subgen:gen"), "include/subgen.h")

		ctx.Target("sublib").
			SetKind(api.TargetStatic).
			AddFiles("lib.c").
			AddIncludes("include")
	})
}
