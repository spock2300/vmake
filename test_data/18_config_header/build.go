package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("feature_foo").
			SetDefault(true).
			SetDescription("Enable feature foo")

		ctx.Option("feature_bar").
			SetDefault(false).
			SetDescription("Enable feature bar")

		ctx.Option("buffer_size").
			SetDefault(4096).
			SetDescription("Buffer size")

		ctx.Option("device_name").
			SetDefault("uart0").
			SetDescription("Device name")

		ctx.Option("platform").
			SetDefault("linux").
			SetValues("linux", "macos", "windows").
			SetDescription("Target platform")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.GenerateConfigHeader()

		ctx.Target("config_test").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c")
	})
}
