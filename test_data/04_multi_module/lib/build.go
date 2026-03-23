package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("utils").
			SetKind(api.TargetStatic).
			AddFiles("*.c").
			AddPublicIncludes("../include")
	})
}
