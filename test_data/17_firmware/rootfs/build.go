package main

import (
	"io"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("busybox", "myapp")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		appOutput := ctx.DepOutput("myapp:myapp")
		busyboxBuildDir := filepath.Dir(ctx.DepOutput("busybox:busybox"))

		ctx.Target("rootfs").SetKind(api.TargetVoid).AddDeps("busybox:busybox", "myapp:myapp").SetBuildFunc(func(pkg *api.Package) error {
			staging := filepath.Join(pkg.BuildDir(), "staging")
			imageFile := filepath.Join(pkg.BuildDir(), "rootfs.sqsh")
			os.RemoveAll(staging)

			copyDirRecursive(filepath.Join(pkg.SourceDir(), "overlay"), staging)

			bbInstall := filepath.Join(busyboxBuildDir, "_install")
			if _, err := os.Stat(bbInstall); err == nil {
				copyDirIfExists(filepath.Join(bbInstall, "bin"), filepath.Join(staging, "bin"))
				copyDirIfExists(filepath.Join(bbInstall, "sbin"), filepath.Join(staging, "sbin"))
				copyDirIfExists(filepath.Join(bbInstall, "usr"), filepath.Join(staging, "usr"))
			}

			if appOutput != "" {
				os.MkdirAll(filepath.Join(staging, "usr", "bin"), 0755)
				copyFile(appOutput, filepath.Join(staging, "usr", "bin", filepath.Base(appOutput)))
			}

			os.Remove(imageFile)
			return pkg.Run("mksquashfs", staging, imageFile, "-noappend")
		})
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDirRecursive(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func copyDirIfExists(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil || !info.IsDir() {
		return nil
	}
	return copyDirRecursive(src, dst)
}
