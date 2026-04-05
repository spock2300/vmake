package main

import (
	"runtime"
	"strconv"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetConfigFiles(".config")
	})

	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.KConfig("u-boot").
			SetDescription("U-Boot configuration").
			AddPreset("sandbox_defconfig").
			AddPreset("rk3568_defconfig").
			AddPreset("stm32_defconfig").
			SetDefault("sandbox_defconfig").
			SetMenuconfigCmd("make menuconfig")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("uboot").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
			srcDir := pkg.SourceDir()
			pkg.EnsureConfig(srcDir)
			pkg.RunIn(srcDir, "make", "-j"+strconv.Itoa(runtime.NumCPU()))
			pkg.RunIn(srcDir, "make", "DESTDIR="+pkg.BuildDir(), "install")
			return nil
		})
	})
}
