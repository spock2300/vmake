package main

import (
	"path/filepath"
	"runtime"
	"strconv"

	"gitee.com/spock2300/vmake/pkg/api"
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
			SetDefault("defconfig").
			PatchKConfig(map[string]string{
				"CONFIG_TC=y": "# CONFIG_TC is not set",
			})
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("busybox").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
			srcDir := pkg.SrcDir()
			pkg.EnsureConfig(srcDir)
			pkg.RunIn(srcDir, "make", "-j"+strconv.Itoa(runtime.NumCPU()))
			installDir := filepath.Join(pkg.BuildDir(), "_install")
			pkg.RunIn(srcDir, "make", "CONFIG_PREFIX="+installDir, "install")
			return nil
		})
	})
}
