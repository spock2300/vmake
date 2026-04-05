package main

import (
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("linux").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
			os.MkdirAll(pkg.BuildDir(), 0755)

			zImage := filepath.Join(pkg.BuildDir(), "zImage")
			if err := os.WriteFile(zImage, []byte("MOCK_LINUX_V6.6_ZIMAGE"), 0644); err != nil {
				return err
			}

			dtsDir := filepath.Join(pkg.BuildDir(), "dts")
			os.MkdirAll(dtsDir, 0755)
			return os.WriteFile(filepath.Join(dtsDir, "board.dtb"), []byte("MOCK_DTB_V1"), 0644)
		})
	})
}
