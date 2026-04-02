package main

import (
	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.ToolchainOption()
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("sublib").
			SetKind(api.TargetStatic).
			AddFiles("lib.c").
			AddIncludes("include").
			AddPublicIncludes("include")
	})
}
