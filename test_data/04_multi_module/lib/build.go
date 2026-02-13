package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(b *api.Builder) {
	b.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("utils").
			SetKind(api.TargetStatic).
			AddFiles("*.c").
			AddIncludes("internal").        // 私有头文件目录
			AddPublicIncludes("../include") // 公开头文件，依赖方自动继承
	})
}
