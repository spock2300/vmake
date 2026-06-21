package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("libcore")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		libcoreSo := ctx.DepOutput("libcore:core")

		ctx.Target("sensordaemon").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDeps("libcore:core").
			AddLdFlags(libcoreSo, "-Wl,-rpath=/usr/lib")
	})
}
