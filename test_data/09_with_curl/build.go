package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("official/curl >=8.5")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("curl_test").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDeps("official/curl")
	})
}
