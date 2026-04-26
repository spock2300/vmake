package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("chip_feature_a").
			SetType(api.OptionBool).
			SetDefault(true).
			SetDescription("Enable chip feature A")

		ctx.Option("chip_mode").
			SetType(api.OptionChoice).
			SetDefault("fast").
			SetValues("fast", "slow", "eco").
			SetDescription("Chip operating mode")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.GenerateConfigDefines()
		ctx.ExportConfig()

		ctx.Target("chip").SetKind(api.TargetStatic).
			AddFiles("src/*.c").
			AddPublicIncludes("include")
	})
}
