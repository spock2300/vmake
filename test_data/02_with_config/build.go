package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(b *api.Builder) {
	b.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("debug").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable debug mode").
			SetGroup("General")

		ctx.Option("optimization").
			SetType(api.OptionChoice).
			SetDefault("O2").
			SetValues("O0", "O1", "O2", "O3", "Os").
			SetDescription("Optimization level").
			SetGroup("General")

		ctx.Option("ssl").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable SSL support").
			SetGroup("SSL")
	})

	b.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("config_app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDefines(ctx.If("ssl", "USE_SSL")).
			AddDefines(ctx.If("debug", "DEBUG=1")).
			AddDefines("OPT_LEVEL=\"" + ctx.String("optimization") + "\"").
			AddCFlags(ctx.Select("optimization", map[string]string{
				"O0": "-O0", "O1": "-O1", "O2": "-O2", "O3": "-O3", "Os": "-Os",
			})).
			AddLinks(ctx.If("ssl", "ssl", "crypto"))
	})
}
