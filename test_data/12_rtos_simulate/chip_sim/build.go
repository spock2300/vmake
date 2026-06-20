package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("chip_model").SetType(api.OptionChoice).
			SetDefault("sim_v1").
			SetValues("sim_v1", "sim_v2").
			SetOnApply(func(ctx *api.ConfigContext, val any) {
				switch val.(string) {
				case "sim_v1":
					ctx.AddGlobalCFlags("-DSIM_V1")
				case "sim_v2":
					ctx.AddGlobalCFlags("-DSIM_V2")
				}
				ctx.AddGlobalCFlags("-Wall", "-Wextra", "-ffunction-sections", "-fdata-sections")
				ctx.AddGlobalLdFlags("-Wl,--gc-sections")
				ctx.SetProvidedLinkerScript("linker/sim.ld")
			})
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("chip").SetKind(api.TargetStatic).SetDefault(true).
			AddFiles("src/*.c").
			AddPublicIncludes("include")
	})
}
