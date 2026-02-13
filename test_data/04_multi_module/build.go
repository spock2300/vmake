package main

import "github.com/vmake/api"

func Main(b *api.Builder) {
	b.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("feature_x").
			SetType(api.OptionBool).
			SetDefault(true).
			SetDescription("Enable feature X")
	})
}
