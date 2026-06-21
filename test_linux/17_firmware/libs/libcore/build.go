package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("core").
			SetKind(api.TargetShared).
			AddFiles("src/*.c").
			AddPublicIncludes("include").
			AddCFlags("-fPIC").
			SetExcludeLibs("libcore")
	})
}
