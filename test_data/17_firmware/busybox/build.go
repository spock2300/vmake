package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://git.busybox.net/busybox")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("busybox").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
			pkg.RunIn(pkg.SrcDir(), "make", "defconfig")

			kconfigPath := filepath.Join(pkg.SrcDir(), ".config")
			if err := replaceKconfig(kconfigPath, map[string]string{
				"CONFIG_TC=y": "# CONFIG_TC is not set",
			}); err != nil {
				return err
			}

			pkg.RunIn(pkg.SrcDir(), "make", "-j"+strconv.Itoa(runtime.NumCPU()))

			installDir := filepath.Join(pkg.BuildDir(), "_install")
			os.RemoveAll(installDir)
			pkg.RunIn(pkg.SrcDir(), "make", "CONFIG_PREFIX="+installDir, "install")
			return nil
		})
	})
}

func replaceKconfig(path string, replacements map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)
	for old, new := range replacements {
		content = strings.ReplaceAll(content, old, new)
	}
	return os.WriteFile(path, []byte(content), 0644)
}
