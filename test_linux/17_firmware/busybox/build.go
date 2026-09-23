package main

import (
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://git.busybox.net/busybox")
		p.SetConfigFiles(".config")
	})

	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.KConfig("busybox").
			SetDescription("BusyBox applet configuration").
			SetSrcDir("src").
			AddPreset("defconfig").
			SetDefaultPreset("defconfig").
			SetKConfigPatches(map[string]string{
				"CONFIG_TC=y": "# CONFIG_TC is not set",
			})
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("busybox").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
			srcDir := pkg.SrcDir()
			pkg.EnsureConfig(srcDir)
			const ownFlags = "ifndef VMAKE_FIRMWARE_FLAGS_RESET\nundefine CFLAGS\nundefine CXXFLAGS\nundefine LDFLAGS\nexport VMAKE_FIRMWARE_FLAGS_RESET := 1\nendif"
			if err := pkg.Make("-C", srcDir, "--eval", ownFlags); err != nil {
				return err
			}
			installDir := filepath.Join(pkg.BuildDir(), "_install")
			return pkg.Make("-C", srcDir, "--eval", ownFlags, "CONFIG_PREFIX="+installDir, "install")
		})
	})

	p.OnClean(func(ctx *api.CleanContext) {
		ctx.RunIn(ctx.SrcDir(), "make", "clean")
	})
}
