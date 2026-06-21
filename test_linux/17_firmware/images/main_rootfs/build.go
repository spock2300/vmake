package main

import (
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("busybox", "libcore", "libnet", "webapp", "sensordaemon", "controlpanel", "configs")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		busyboxBuildDir := ctx.DepBuildDir("busybox:busybox")
		webappOut := ctx.DepOutput("webapp:webapp")
		sensorOut := ctx.DepOutput("sensordaemon:sensordaemon")
		panelOut := ctx.DepOutput("controlpanel:controlpanel")
		libcoreSo := ctx.DepOutput("libcore:core")
		libnetSo := ctx.DepOutput("libnet:net")
		configsBuildDir := ctx.DepBuildDir("configs:configs")

		ctx.Target("main_rootfs").SetKind(api.TargetVoid).
			AddDeps(
				"busybox:busybox",
				"libcore:core", "libnet:net",
				"webapp:webapp", "sensordaemon:sensordaemon", "controlpanel:controlpanel",
				"configs:configs",
			).
			SetBuildFunc(func(pkg *api.Package) error {
				return buildSquashfs(
					pkg,
					"main_rootfs.sqsh",
					func(staging string) error {
						if err := api.CopyDir(filepath.Join(pkg.SourceDir(), "overlay"), staging); err != nil {
							return err
						}
						copyBusybox(staging, busyboxBuildDir)
						copyFiles(staging, "usr/bin", webappOut, sensorOut, panelOut)
						copyFiles(staging, "usr/lib", libcoreSo, libnetSo)
						copyConfigs(staging, filepath.Join(configsBuildDir, "output"))
						return nil
					})
			})
	})
}

func copyBusybox(staging, busyboxBuildDir string) {
	bbInstall := filepath.Join(busyboxBuildDir, "_install")
	if _, err := os.Stat(bbInstall); err != nil {
		return
	}
	api.CopyDirIfExists(filepath.Join(bbInstall, "bin"), filepath.Join(staging, "bin"))
	api.CopyDirIfExists(filepath.Join(bbInstall, "sbin"), filepath.Join(staging, "sbin"))
	api.CopyDirIfExists(filepath.Join(bbInstall, "usr"), filepath.Join(staging, "usr"))
}

func copyFiles(staging, subdir string, files ...string) {
	for _, f := range files {
		if f == "" {
			continue
		}
		dest := filepath.Join(staging, subdir, filepath.Base(f))
		os.MkdirAll(filepath.Dir(dest), 0755)
		api.CopyFile(f, dest)
	}
}

func copyConfigs(staging, configsStage string) {
	configsEtc := filepath.Join(configsStage, "etc")
	if _, err := os.Stat(configsEtc); err == nil {
		api.CopyDirIfExists(configsEtc, filepath.Join(staging, "etc"))
	}
}

func buildSquashfs(pkg *api.Package, imageName string, stage func(staging string) error) error {
	staging := filepath.Join(pkg.BuildDir(), "staging")
	imageFile := filepath.Join(pkg.BuildDir(), imageName)
	os.RemoveAll(staging)
	if err := stage(staging); err != nil {
		return err
	}
	os.Remove(imageFile)
	return pkg.Run("mksquashfs", staging, imageFile, "-noappend")
}
