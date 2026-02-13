package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(b *api.Builder) {
	b.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("feature_x").
			SetType(api.OptionBool).
			SetDefault(true).
			SetDescription("Enable feature X")
	})
}
