package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("subtest/mother >=1.0.0", "subtest/mother/sub_a")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDeps("subtest/mother:base", "subtest/mother/sub_a:*")
	})
}
