package main

import "github.com/vmake/api"

func Main(b *api.Builder) {
	b.OnBuild(func(ctx *api.BuildContext) {
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
			AddFiles("tests/*.c").
			AddIncludes("include").
			AddDeps("mylib").
			SetDefault(false)
	})
}
