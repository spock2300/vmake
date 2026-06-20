package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("feature_foo").
			SetType(api.OptionBool).
			SetDefault(true).
			SetDescription("Enable feature foo")

		ctx.Option("feature_bar").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable feature bar")

		ctx.Option("buffer_size").
			SetType(api.OptionInt).
			SetDefault(4096).
			SetDescription("Buffer size")

		ctx.Option("device_name").
			SetType(api.OptionString).
			SetDefault("uart0").
			SetDescription("Device name")

		ctx.Option("platform").
			SetType(api.OptionChoice).
			SetDefault("linux").
			SetValues("linux", "macos", "windows").
			SetDescription("Target platform")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.GenerateConfigDefines()

		ctx.Target("defines_test").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c")
	})
}
