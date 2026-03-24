package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("local/mylib >=1.0.0")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddPackages("local/mylib")
	})
}
