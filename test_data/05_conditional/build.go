package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("debug").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable debug mode").
			SetGroup("General")

		ctx.Option("verbose").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable verbose output").
			SetGroup("General")

		ctx.Option("feature_a").
			SetType(api.OptionBool).
			SetDefault(true).
			SetDescription("Enable feature A").
			SetGroup("Features")

		ctx.Option("feature_b").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable feature B").
			SetGroup("Features")

		ctx.Option("platform").
			SetType(api.OptionChoice).
			SetDefault("linux").
			SetValues("linux", "macos", "windows").
			SetDescription("Target platform").
			SetGroup("Platform")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("conditional_app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDefines(ctx.If("debug", "DEBUG_MODE")).
			AddDefines(ctx.If("verbose", "VERBOSE")).
			AddDefines(ctx.If("feature_a", "FEATURE_A")).
			AddDefines(ctx.If("feature_b", "FEATURE_B")).
			AddDefines("PLATFORM=\"" + ctx.String("platform") + "\"").
			AddCFlags(ctx.If("debug", "-g", "-O0")).
			AddCFlags(ctx.IfNot("debug", "-O2")).
			AddCFlags(ctx.Select("platform", map[string]string{
				"linux":   "-DLINUX",
				"macos":   "-DMACOS",
				"windows": "-DWINDOWS",
			}))
	})
}
