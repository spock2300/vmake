package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("helper").
			SetKind(api.TargetStatic).
			AddFiles("src/helper.c").
			AddPublicIncludes("include")

		ctx.Target("foo").
			SetKind(api.TargetShared).
			AddFiles("src/foo.c").
			AddPublicIncludes("include").
			AddDeps("helper").
			SetExcludeLibs("libhelper").
			SetSymbolBinding("static").
			SetExpectedExports("foo_api")

		ctx.Target("bar").
			SetKind(api.TargetShared).
			AddFiles("src/bar.c").
			AddPublicIncludes("include").
			SetVersionScript("bar.map").
			SetExpectedExports("bar_api")
	})
}
