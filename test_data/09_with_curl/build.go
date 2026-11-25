package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.AddRequires("official/curl")

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("curl_test").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			UseRequires()
	})
}
