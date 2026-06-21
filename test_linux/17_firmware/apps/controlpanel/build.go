package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("libcore", "libnet")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		libcoreSo := ctx.DepOutput("libcore:core")
		libnetSo := ctx.DepOutput("libnet:net")

		ctx.Target("controlpanel").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDeps("libcore:core", "libnet:net").
			AddLdFlags(libcoreSo, libnetSo, "-Wl,-rpath=/usr/lib")
	})
}
