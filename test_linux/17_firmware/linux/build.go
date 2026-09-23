package main

import (
	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetConfigFiles(".config")
	})

	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.KConfig("linux").
			SetDescription("Linux kernel configuration").
			AddPreset("x86_64_defconfig").
			AddPreset("rk3568_defconfig").
			AddPreset("stm32_defconfig").
			SetDefaultPreset("x86_64_defconfig").
			SetMenuconfigCmd("make", "menuconfig")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("linux").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
			srcDir := pkg.SourceDir()
			pkg.EnsureConfig(srcDir)
			const ownFlags = "ifndef VMAKE_FIRMWARE_FLAGS_RESET\nundefine CFLAGS\nundefine CXXFLAGS\nundefine LDFLAGS\nexport VMAKE_FIRMWARE_FLAGS_RESET := 1\nendif"
			if err := pkg.Make("-C", srcDir, "--eval", ownFlags); err != nil {
				return err
			}
			return pkg.Make("-C", srcDir, "--eval", ownFlags, "DESTDIR="+pkg.BuildDir(), "install")
		})
	})
}
