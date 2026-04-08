package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("mylib").
			SetKind(api.TargetStatic).
			AddFiles("src/mylib.c").
			AddIncludes("include")

		ctx.Target("myapp").
			SetKind(api.TargetBinary).
			AddFiles("src/main.c").
			AddIncludes("include").
			AddDeps("mylib")

		ctx.Target("tests").
			SetKind(api.TargetBinary).
			SetTest(true).
			AddFiles("tests/*.c").
			AddIncludes("include").
			AddDeps("mylib")
	})
}
