package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("//libs/libcore", "//libs/libnet")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		libcoreSo := ctx.DepOutput("//libs/libcore:core")
		libnetSo := ctx.DepOutput("//libs/libnet:net")

		ctx.Target("controlpanel").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDeps("//libs/libcore:core", "//libs/libnet:net").
			AddLdFlags(libcoreSo, libnetSo, "-Wl,-rpath=/usr/lib")
	})
}
