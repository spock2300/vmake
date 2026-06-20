package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("linenoise").
			SetKind(api.TargetStatic).
			AddFiles("src/*.c").
			SetSymbolPrefix("ln_")
	})
}
