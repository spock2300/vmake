package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("libcore")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		libcoreSo := ctx.DepOutput("libcore:core")

		ctx.Target("net").
			SetKind(api.TargetShared).
			AddFiles("src/*.c").
			AddPublicIncludes("include").
			AddCFlags("-fPIC").
			AddDeps("libcore:core").
			AddLdFlags(libcoreSo).
			SetExcludeLibs("libcore")
	})
}
