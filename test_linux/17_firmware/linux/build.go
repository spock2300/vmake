package main

import (
	"runtime"
	"strconv"

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
			SetDefault("x86_64_defconfig").
			SetMenuconfigCmd("make menuconfig")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("linux").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
			srcDir := pkg.SourceDir()
			pkg.EnsureConfig(srcDir)
			pkg.RunIn(srcDir, "make", "-j"+strconv.Itoa(runtime.NumCPU()))
			pkg.RunIn(srcDir, "make", "DESTDIR="+pkg.BuildDir(), "install")
			return nil
		})
	})
}
