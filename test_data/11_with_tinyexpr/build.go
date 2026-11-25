package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.AddRequires("official/tinyexpr")

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("tinyexpr_test").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			UseRequires()
	})
}
