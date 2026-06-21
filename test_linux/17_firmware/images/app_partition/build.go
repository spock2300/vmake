package main

import (
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("libcore", "libnet", "webapp", "sensordaemon", "controlpanel")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		webappOut := ctx.DepOutput("webapp:webapp")
		sensorOut := ctx.DepOutput("sensordaemon:sensordaemon")
		panelOut := ctx.DepOutput("controlpanel:controlpanel")
		libcoreSo := ctx.DepOutput("libcore:core")
		libnetSo := ctx.DepOutput("libnet:net")

		ctx.Target("app_partition").SetKind(api.TargetVoid).
			AddDeps(
				"libcore:core", "libnet:net",
				"webapp:webapp", "sensordaemon:sensordaemon", "controlpanel:controlpanel",
			).
			SetBuildFunc(func(pkg *api.Package) error {
				staging := filepath.Join(pkg.BuildDir(), "staging")
				imageFile := filepath.Join(pkg.BuildDir(), "app_partition.sqsh")
				os.RemoveAll(staging)

				if err := api.CopyDir(filepath.Join(pkg.SourceDir(), "overlay"), staging); err != nil {
					return err
				}

				copyOne(staging, "apps/webapp", webappOut)
				copyOne(staging, "apps/sensordaemon", sensorOut)
				copyOne(staging, "apps/controlpanel", panelOut)
				copyOne(staging, "lib", libcoreSo)
				copyOne(staging, "lib", libnetSo)

				os.Remove(imageFile)
				return pkg.Run("mksquashfs", staging, imageFile, "-noappend")
			})
	})
}

func copyOne(staging, subdir, src string) {
	if src == "" {
		return
	}
	dest := filepath.Join(staging, subdir, filepath.Base(src))
	os.MkdirAll(filepath.Dir(dest), 0755)
	api.CopyFile(src, dest)
}
