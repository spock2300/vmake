package main

import "github.com/vmake/api"

func Main(b *api.Builder) {
	b.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("hello").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c")
	})
}
