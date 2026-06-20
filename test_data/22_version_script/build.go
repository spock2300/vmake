package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.SetDefaultVisibilityHidden()
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("libfoo").
			SetKind(api.TargetShared).
			AddFiles("src/foo_api.c", "src/foo_internal.c").
			AddPublicIncludes("include").
			SetVersionScript("export.map").
			SetExpectedExports("foo_api", "foo_init")

		ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/main.c").
			AddDeps("libfoo")
	})
}
