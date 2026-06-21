package main

import (
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("busybox", "libcore", "sensordaemon")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		busyboxBuildDir := ctx.DepBuildDir("busybox:busybox")
		sensorOut := ctx.DepOutput("sensordaemon:sensordaemon")
		libcoreSo := ctx.DepOutput("libcore:core")

		ctx.Target("recovery_rootfs").SetKind(api.TargetVoid).
			AddDeps("busybox:busybox", "libcore:core", "sensordaemon:sensordaemon").
			SetBuildFunc(func(pkg *api.Package) error {
				staging := filepath.Join(pkg.BuildDir(), "staging")
				imageFile := filepath.Join(pkg.BuildDir(), "recovery_rootfs.sqsh")
				os.RemoveAll(staging)

				if err := api.CopyDir(filepath.Join(pkg.SourceDir(), "overlay"), staging); err != nil {
					return err
				}

				bbInstall := filepath.Join(busyboxBuildDir, "_install")
				if _, err := os.Stat(bbInstall); err == nil {
					api.CopyDirIfExists(filepath.Join(bbInstall, "bin"), filepath.Join(staging, "bin"))
					api.CopyDirIfExists(filepath.Join(bbInstall, "sbin"), filepath.Join(staging, "sbin"))
				}

				if sensorOut != "" {
					dest := filepath.Join(staging, "usr", "bin", filepath.Base(sensorOut))
					os.MkdirAll(filepath.Dir(dest), 0755)
					api.CopyFile(sensorOut, dest)
				}
				if libcoreSo != "" {
					dest := filepath.Join(staging, "usr", "lib", filepath.Base(libcoreSo))
					os.MkdirAll(filepath.Dir(dest), 0755)
					api.CopyFile(libcoreSo, dest)
				}

				os.Remove(imageFile)
				return pkg.Run("mksquashfs", staging, imageFile, "-noappend")
			})
	})
}
