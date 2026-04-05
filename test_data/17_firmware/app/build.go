package main

import (
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("myapp")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		appOutput := ctx.DepOutput("myapp:myapp")

		ctx.Target("app").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
			staging := filepath.Join(pkg.BuildDir(), "staging")
			imageFile := filepath.Join(pkg.BuildDir(), "app.sqsh")
			os.RemoveAll(staging)

			api.CopyDir(filepath.Join(pkg.SourceDir(), "overlay"), staging)

			if appOutput != "" {
				os.MkdirAll(filepath.Join(staging, "usr", "bin"), 0755)
				api.CopyFile(appOutput, filepath.Join(staging, "usr", "bin", filepath.Base(appOutput)))
			}

			os.Remove(imageFile)
			return pkg.Run("mksquashfs", staging, imageFile, "-noappend")
		})
	})
}
