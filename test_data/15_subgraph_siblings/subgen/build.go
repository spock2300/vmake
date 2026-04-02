package main

import (
	"fmt"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.ToolchainOption()
		ctx.Option("magic").
			SetType(api.OptionInt).
			SetDefault(42)
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		magic := ctx.Int("magic")
		ctx.Target("gen").
			SetKind(api.TargetBinary).
			AddFiles("gen.c").
			AddCFlags(fmt.Sprintf("-DMAGIC=%d", magic))
	})
}
