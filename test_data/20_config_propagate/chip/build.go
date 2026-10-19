package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("chip_feature_a").
			SetDefault(true).
			SetDescription("Enable chip feature A")

		ctx.Option("chip_mode").
			SetDefault("fast").
			SetValues("fast", "slow", "eco").
			SetDescription("Chip operating mode")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.GenerateConfigDefines()

		ctx.Target("chip").SetKind(api.TargetStatic).
			AddFiles("src/*.c").
			AddPublicIncludes("include")
	})
}
