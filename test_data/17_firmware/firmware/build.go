package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("rootfs", "app")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		rootfsBuildDir := filepath.Dir(ctx.DepOutput("rootfs:rootfs"))
		appBuildDir := filepath.Dir(ctx.DepOutput("app:app"))

		ctx.Target("firmware").SetKind(api.TargetVoid).AddDeps("rootfs:rootfs", "app:app").SetBuildFunc(func(pkg *api.Package) error {
			rootfsImg := filepath.Join(rootfsBuildDir, "rootfs.sqsh")
			appImg := filepath.Join(appBuildDir, "app.sqsh")

			for _, f := range []string{rootfsImg, appImg} {
				if _, err := os.Stat(f); err != nil {
					return fmt.Errorf("missing partition image: %s", f)
				}
			}

			layout := []Partition{
				{"rootfs", rootfsImg, 0},
				{"app", appImg, 0},
			}

			return packImage(layout, filepath.Join(pkg.BuildDir(), "firmware.img"))
		})
	})
}

type Partition struct {
	Name   string
	Source string
	Offset int64
}

func packImage(layout []Partition, output string) error {
	offset := int64(0)
	sizes := make([]int64, len(layout))

	for i, p := range layout {
		info, err := os.Stat(p.Source)
		if err != nil {
			return err
		}
		sz := info.Size()
		if p.Offset > 0 {
			offset = p.Offset
		}
		p.Offset = offset
		layout[i] = p
		sizes[i] = sz
		offset += sz
		if offset%512 != 0 {
			offset += 512 - offset%512
		}
	}

	f, err := os.Create(output)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := f.Truncate(offset); err != nil {
		return err
	}

	for i, p := range layout {
		data, err := os.ReadFile(p.Source)
		if err != nil {
			return err
		}
		if _, err := f.WriteAt(data, p.Offset); err != nil {
			return err
		}
		fmt.Printf("  [%s] offset=%d size=%d\n", p.Name, p.Offset, sizes[i])
	}

	fmt.Printf("  firmware.img: total=%d bytes\n", offset)
	return nil
}
