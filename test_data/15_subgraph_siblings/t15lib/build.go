package main

import (
	"os"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.BuildSubGraph("t15gen")

		os.MkdirAll("include", 0755)
		ctx.Exec(ctx.DepOutput("t15gen:gen"), "include/subgen.h")

		ctx.Target("sublib").
			SetKind(api.TargetStatic).
			AddFiles("lib.c").
			AddIncludes("include").
			AddPublicIncludes("include")
	})
}
