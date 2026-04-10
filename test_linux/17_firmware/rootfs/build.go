package main

import (
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("busybox", "myapp")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		appOutput := ctx.DepOutput("myapp:myapp")
		busyboxBuildDir := ctx.DepBuildDir("busybox:busybox")

		ctx.Target("rootfs").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
			staging := filepath.Join(pkg.BuildDir(), "staging")
			imageFile := filepath.Join(pkg.BuildDir(), "rootfs.sqsh")
			os.RemoveAll(staging)

			api.CopyDir(filepath.Join(pkg.SourceDir(), "overlay"), staging)

			bbInstall := filepath.Join(busyboxBuildDir, "_install")
			if _, err := os.Stat(bbInstall); err == nil {
				api.CopyDirIfExists(filepath.Join(bbInstall, "bin"), filepath.Join(staging, "bin"))
				api.CopyDirIfExists(filepath.Join(bbInstall, "sbin"), filepath.Join(staging, "sbin"))
				api.CopyDirIfExists(filepath.Join(bbInstall, "usr"), filepath.Join(staging, "usr"))
			}

			if appOutput != "" {
				os.MkdirAll(filepath.Join(staging, "usr", "bin"), 0755)
				api.CopyFile(appOutput, filepath.Join(staging, "usr", "bin", filepath.Base(appOutput)))
			}

			os.Remove(imageFile)
			return pkg.Run("mksquashfs", staging, imageFile, "-noappend")
		})
	})
}
