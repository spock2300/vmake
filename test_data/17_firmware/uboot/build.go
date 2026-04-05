package main

import (
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("uboot").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
			bin := filepath.Join(pkg.BuildDir(), "u-boot.bin")
			os.MkdirAll(pkg.BuildDir(), 0755)
			return os.WriteFile(bin, []byte("MOCK_U-BOOT_V2024.01"), 0644)
		})
	})
}
