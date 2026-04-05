# Firmware Build (KConfig, EnsureConfig, Partitions)

A complete firmware project using KConfig preset management, stamp-based skip,
dependency-driven partition assembly, and `DepBuildDir` for accessing build
artifacts from other packages.

## Project Structure

```
my-firmware/
├── build.go              # Root: empty (all work in sub-packages)
├── packages/
│   ├── uboot/
│   │   └── build.go
│   ├── linux/
│   │   └── build.go
│   ├── busybox/
│   │   └── build.go
│   └── myapp/
│       ├── src/
│       ├── include/
│       └── build.go
├── partitions/
│   └── rootfs/
│       ├── overlay/
│       └── build.go
└── firmware/
    └── build.go
```

The root `build.go` is empty — the project is composed entirely of sub-packages. Each sub-package is an independent unit managed by vmake's dependency system.

## Root build.go

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
}
```

## U-Boot (KConfig + EnsureConfig)

```go
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
            AddPreset("sandbox_defconfig").
            AddPreset("rk3568_defconfig").
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
```

## Busybox (PatchKConfig + SetSrcDir)

```go
package main

import (
    "path/filepath"
    "runtime"
    "strconv"

    "gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
    p.OnPackage(func(p *api.Package) {
        p.SetConfigFiles(".config")
    })

    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.KConfig("busybox").
            AddPreset("defconfig").
            SetDefault("defconfig").
            SetSrcDir("src").
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
```

`SetSrcDir("src")` tells vmake the source code is in `SourceDir/src/`. Use `pkg.SrcDir()` (not `pkg.SourceDir()`) to get this path. `EnsureConfig` looks for `.config` in `SrcDir`.

## myapp (Simple Binary)

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("myapp").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c").
            AddIncludes("include")
    })
}
```

## Rootfs (Partition Assembly with DepBuildDir)

```go
package main

import (
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
        busyboxBuildDir := ctx.DepBuildDir("busybox:busybox")

        ctx.Target("rootfs").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
            staging := filepath.Join(pkg.BuildDir(), "staging")
            os.RemoveAll(staging)
            os.MkdirAll(staging, 0755)

            api.CopyDir(filepath.Join(pkg.SourceDir(), "overlay"), staging)

            bbInstall := filepath.Join(busyboxBuildDir, "_install")
            if _, err := os.Stat(bbInstall); err == nil {
                api.CopyDirIfExists(filepath.Join(bbInstall, "bin"), filepath.Join(staging, "bin"))
                api.CopyDirIfExists(filepath.Join(bbInstall, "sbin"), filepath.Join(staging, "sbin"))
                api.CopyDirIfExists(filepath.Join(bbInstall, "usr"), filepath.Join(staging, "usr"))
            }

            if appOutput != "" {
                os.MkdirAll(filepath.Join(staging, "usr", "bin"), 0755)
                api.CopyFile(appOutput, filepath.Join(staging, "usr", "bin", filepath.Base(appOutput)))
            }

            imageFile := filepath.Join(pkg.BuildDir(), "rootfs.sqsh")
            os.Remove(imageFile)
            return pkg.Run("mksquashfs", staging, imageFile, "-noappend")
        })
    })
}
```

`DepBuildDir` returns the dependency's `BuildDir` — used to locate build artifacts like busybox's `_install` directory. `DepOutput` returns the output binary path.

## Firmware (Final Assembly)

```go
package main

import (
    "os"
    "path/filepath"

    "gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires("uboot", "linux", "rootfs", "app")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ubootDir := ctx.DepBuildDir("uboot:uboot")
        linuxDir := ctx.DepBuildDir("linux:linux")
        rootfsDir := ctx.DepBuildDir("rootfs:rootfs")

        ctx.Target("firmware").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
            inputs := []string{
                filepath.Join(ubootDir, "u-boot.bin"),
                filepath.Join(linuxDir, "zImage"),
                filepath.Join(rootfsDir, "rootfs.sqsh"),
            }
            for _, f := range inputs {
                if _, err := os.Stat(f); err != nil {
                    return err
                }
            }
            return packImage(inputs, filepath.Join(pkg.BuildDir(), "firmware.img"))
        })
    })
}
```

## What This Demonstrates

- **KConfig presets** — `AddPreset("defconfig")` registers a defconfig name as a make target
- **EnsureConfig** — `pkg.EnsureConfig(srcDir)` checks `.config` exists + non-empty, runs `make <preset>` if missing
- **PatchKConfig** — Override specific config values after defconfig generation
- **SetSrcDir** — Source code in a subdirectory (`src/` for busybox)
- **SetConfigFiles** — Registers files that invalidate the build stamp on change
- **Stamp-based skip** — `.vmake_stamp` in BuildDir; stale when config files are newer
- **DepBuildDir** — `ctx.DepBuildDir("busybox:busybox")` returns the dependency's build directory
- **DepOutput** — `ctx.DepOutput("myapp:myapp")` returns the dependency's output binary path
- **api.CopyFile/CopyDir/CopyDirIfExists** — File copy utilities from the `api` package
- **autoWireRequireDeps** — `AddRequires` automatically creates build graph edges (no need for explicit `AddDeps` on local targets)
- **Empty root build.go** — All work happens in sub-packages; root only provides the workspace

## Key Points

- Use `pkg.RunIn(srcDir, "make", ...)` when the Makefile is in the source tree (NOT `pkg.Make()`)
- Use `pkg.SrcDir()` (not `SourceDir()`) when the package has `SetSrcDir("src")`
- `SetConfigFiles` + stamp skip applies only to void targets without `InstallDir`
- Presets are defconfig names passed to `make <preset>` — not complete `.config` files
- `EnsureConfig` also applies `PatchKConfig` patches after running `make <preset>`
- Guard `api.CopyDirIfExists` calls with `os.Stat` when the source may not exist
- Remove output files before regenerating (e.g., `os.Remove(imageFile)` before `mksquashfs`)
- Validate input files exist with `os.Stat` before packing firmware images

## See Also

- SKILL.md - KConfig Preset Management, Common Mistakes (pkg.Make vs pkg.RunIn)
- examples/embedded-rtos.md - Linker scripts, AddPostLink, AddBinHeader
- references/api.md - KConfigEntry, Package KConfig methods, DepBuildDir, DepOutput
