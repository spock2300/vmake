package main

import (
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("configs").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
			stageDir := filepath.Join(pkg.BuildDir(), "output")
			os.RemoveAll(stageDir)
			return api.CopyDir(filepath.Join(pkg.SourceDir(), "overlay"), stageDir)
		})
	})
}
