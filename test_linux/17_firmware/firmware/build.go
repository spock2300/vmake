package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

const (
	partUboot      = 0x0000000
	partUbootSize  = 1 * 1024 * 1024
	partConfig     = partUboot + partUbootSize
	partConfigSize = 1 * 1024 * 1024
	partKernel     = partConfig + partConfigSize
	partKernelSize = 8 * 1024 * 1024
	partMainRootfs = partKernel + partKernelSize
	partMainSize   = 32 * 1024 * 1024
	partRecovery   = partMainRootfs + partMainSize
	partRecSize    = 16 * 1024 * 1024
	partApp        = partRecovery + partRecSize
	partAppSize    = 16 * 1024 * 1024
	totalImage     = partApp + partAppSize
)

type partition struct {
	name   string
	source string
	offset int64
	size   int64
}

func Main(p *api.Package) {
	p.SetRoot(true)

	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("uboot", "linux", "main_rootfs", "recovery_rootfs", "app_partition", "configs")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ubootBuildDir := ctx.DepBuildDir("uboot:uboot")
		linuxBuildDir := ctx.DepBuildDir("linux:linux")
		mainRootfsDir := ctx.DepBuildDir("main_rootfs:main_rootfs")
		recoveryDir := ctx.DepBuildDir("recovery_rootfs:recovery_rootfs")
		appDir := ctx.DepBuildDir("app_partition:app_partition")
		configsDir := ctx.DepBuildDir("configs:configs")

		ctx.Target("firmware").SetKind(api.TargetVoid).
			AddDeps(
				"uboot:uboot", "linux:linux",
				"main_rootfs:main_rootfs", "recovery_rootfs:recovery_rootfs",
				"app_partition:app_partition", "configs:configs",
			).
			SetBuildFunc(func(pkg *api.Package) error {
				layout := []partition{
					{"uboot", filepath.Join(ubootBuildDir, "u-boot.bin"), partUboot, partUbootSize},
					{"config", filepath.Join(configsDir, "output"), partConfig, partConfigSize},
					{"kernel", filepath.Join(linuxBuildDir, "zImage"), partKernel, partKernelSize},
					{"main_rootfs", filepath.Join(mainRootfsDir, "main_rootfs.sqsh"), partMainRootfs, partMainSize},
					{"recovery_rootfs", filepath.Join(recoveryDir, "recovery_rootfs.sqsh"), partRecovery, partRecSize},
					{"app_partition", filepath.Join(appDir, "app_partition.sqsh"), partApp, partAppSize},
				}
				return packImage(layout, filepath.Join(pkg.BuildDir(), "firmware.img"))
			})
	})
}

func packImage(layout []partition, output string) error {
	for _, p := range layout {
		if _, err := os.Stat(p.source); err != nil {
			return fmt.Errorf("missing partition source %s: %w", p.name, err)
		}
	}

	f, err := os.Create(output)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := f.Truncate(totalImage); err != nil {
		return err
	}

	padding := make([]byte, 4096)
	for i := range padding {
		padding[i] = 0xFF
	}

	for _, p := range layout {
		info, err := os.Stat(p.source)
		if err != nil {
			return err
		}
		size := info.Size()

		if info.IsDir() {
			if err := writeDirFlat(f, p.source, p.offset, p.size, padding); err != nil {
				return err
			}
		} else {
			if err := writeFileAt(f, p.source, p.offset); err != nil {
				return err
			}
			if size < p.size {
				padSize := p.size - size
				if err := padWith(f, p.offset+size, padSize, padding); err != nil {
					return err
				}
			}
		}
		fmt.Printf("  [%-15s] offset=0x%07x size=%d (%d MB reserved)\n",
			p.name, p.offset, size, p.size/(1024*1024))
	}

	fmt.Printf("  firmware.img: total=%d bytes (%.1f MB)\n", totalImage, float64(totalImage)/(1024*1024))
	return nil
}

func writeFileAt(f *os.File, src string, offset int64) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	_, err = f.WriteAt(data, offset)
	return err
}

func writeDirFlat(f *os.File, srcDir string, offset, size int64, padding []byte) error {
	written := int64(0)
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			return err
		}
		if _, err := f.WriteAt(data, offset+written); err != nil {
			return err
		}
		written += int64(len(data))
		if written >= size {
			break
		}
	}
	if written < size {
		return padWith(f, offset+written, size-written, padding)
	}
	return nil
}

func padWith(f *os.File, offset, size int64, padding []byte) error {
	for size > 0 {
		chunk := int64(len(padding))
		if chunk > size {
			chunk = size
		}
		if _, err := f.WriteAt(padding[:chunk], offset); err != nil {
			return err
		}
		offset += chunk
		size -= chunk
	}
	return nil
}
