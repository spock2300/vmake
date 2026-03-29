package main

import (
	"fmt"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("official/tinyexpr >=1.0.0")
	})

	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.ToolchainOption()

		ctx.Option("magic").
			SetType(api.OptionChoice).
			SetDefault("42").
			SetValues("42", "99", "256")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		magic := ctx.String("magic")
		ctx.Target("codegen").
			SetKind(api.TargetBinary).
			AddFiles("*.c").
			AddCFlags(fmt.Sprintf("-DMAGIC=%s", magic)).
			AddDeps("official/tinyexpr")
	})
}
