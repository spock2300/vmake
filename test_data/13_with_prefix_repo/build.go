package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("test_build/mathlib >=1.0")
		ctx.AddRequires("test_build/greeter")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDeps("test_build/mathlib", "test_build/greeter")
	})
}
