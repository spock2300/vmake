package main

import "github.com/vmake/api"

func Main(b *api.Builder) {
	b.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("*.c").
			AddIncludes("../include").
			AddDeps("utils")
	})
}
