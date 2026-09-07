package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("sub_b")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("utils_a").
			SetKind(api.TargetStatic).
			AddFiles("src/*.c").
			AddPublicIncludes("include").
			AddDeps("sub_b:utils_b")
	})
}
