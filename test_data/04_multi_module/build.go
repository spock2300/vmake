package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("feature_x").
			SetType(api.OptionBool).
			SetDefault(true).
			SetDescription("Enable feature X")
	})
}
