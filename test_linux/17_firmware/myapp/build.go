package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("myapp").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddIncludes("include")
	})
}
