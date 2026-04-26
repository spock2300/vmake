package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("chip")
	})

	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("app_verbose").
			SetType(api.OptionBool).
			SetDefault(true).
			SetDescription("Enable verbose output")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.GenerateConfigDefines()
		ctx.ImportConfig("chip")

		ctx.Target("app").SetKind(api.TargetBinary).SetDefault(true).
			AddFiles("src/*.c").
			AddIncludes("include").
			AddDeps("chip:chip")
	})
}
