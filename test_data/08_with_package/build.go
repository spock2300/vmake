package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.AddRequires("official/zlib >=1.2")

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("zlib_test").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			UseRequires()
	})
}
