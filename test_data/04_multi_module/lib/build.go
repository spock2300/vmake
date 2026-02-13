package main

import "github.com/vmake/api"

func Main(b *api.Builder) {
	b.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("utils").
			SetKind(api.TargetStatic).
			AddFiles("*.c").
			AddIncludes("../include")
	})
}
