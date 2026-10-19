package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.AddRequires("chip")

	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("app_verbose").
			SetDefault(true).
			SetDescription("Enable verbose output")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.SyncConfigDefines("chip")

		ctx.Target("app").SetKind(api.TargetBinary).SetDefault(true).
			AddFiles("src/*.c").
			AddIncludes("include").
			UseRequires()
	})
}
