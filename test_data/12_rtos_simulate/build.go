package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("chip_sim")
	})

	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("debug").
			SetType(api.OptionBool).
			SetDefault(true).
			SetDescription("Enable debug symbols")

		ctx.Option("optimize").
			SetType(api.OptionChoice).
			SetDefault("O2").
			SetValues("O0", "O2", "Os").
			SetDescription("Optimization level")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("firmware").SetKind(api.TargetBinary).SetDefault(true).
			AddFiles("src/*.c").
			AddIncludes("include").
			AddCFlags(
				ctx.Select("optimize", map[string]string{"O0": "-O0", "O2": "-O2", "Os": "-Os"}),
				ctx.If("debug", "-g"),
			).
			AddLdFlags(
				"-Wl,--print-memory-usage",
				"-nostartfiles",
			).
			AddDeps("chip_sim:chip").
			UseDependencyLinkerScript().
			AddPostLinkHex().
			AddPostLinkBin().
			AddPostLinkSize()

		ctx.Target("test_runner").SetKind(api.TargetBinary).SetDefault(false).
			AddFiles("src/*.c").
			AddIncludes("include").
			AddCFlags("-DUNIT_TEST", "-Wall", ctx.If("debug", "-g"))
	})
}
