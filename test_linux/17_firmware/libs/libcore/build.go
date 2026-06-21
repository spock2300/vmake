package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("core").
			SetKind(api.TargetShared).
			AddFiles("src/*.c").
			AddPublicIncludes("include").
			AddCFlags("-fPIC").
			SetExcludeLibs("libcore").
			SetExpectedExports(
				"core_init",
				"core_get_version",
				"core_build_id",
				"core_log",
				"core_log_set_fd",
				"core_hash",
			)
	})
}
