# Firmware Build (KConfig, EnsureConfig, Partitions)

A complete firmware project using KConfig preset management, external incremental builds,
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

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
}
```

## U-Boot (KConfig + EnsureConfig)

```go
package main

import (
    "path/filepath"

    "github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
    p.OnPackage(func(p *api.Package) {
        p.SetConfigFiles(".config")
    })

    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.KConfig("u-boot").
            AddPreset("sandbox_defconfig").
            AddPreset("rk3568_defconfig").
            SetDefaultPreset("sandbox_defconfig").
            SetMenuconfigCmd("make", "menuconfig")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("uboot").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
            srcDir := pkg.SourceDir()
            pkg.EnsureConfig(srcDir)
            const ownFlags = "ifndef VMAKE_FIRMWARE_FLAGS_RESET\nundefine CFLAGS\nundefine CXXFLAGS\nundefine LDFLAGS\nexport VMAKE_FIRMWARE_FLAGS_RESET := 1\nendif"
            if err := pkg.Make("-C", filepath.ToSlash(srcDir), "--eval", ownFlags); err != nil {
                return err
            }
            return pkg.Make("-C", filepath.ToSlash(srcDir), "--eval", ownFlags, "DESTDIR="+filepath.ToSlash(pkg.BuildDir()), "install")
        })
    })
}
```

## Busybox (SetKConfigPatches + SetSrcDir)

```go
package main

import (
    "path/filepath"

    "github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
    p.OnPackage(func(p *api.Package) {
        p.SetConfigFiles(".config")
    })

    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.KConfig("busybox").
            AddPreset("defconfig").
            SetDefaultPreset("defconfig").
            SetSrcDir("src").
            SetKConfigPatches(map[string]string{
                "CONFIG_TC=y": "# CONFIG_TC is not set",
            })
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("busybox").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
            srcDir := pkg.SrcDir()
            pkg.EnsureConfig(srcDir)
            const ownFlags = "ifndef VMAKE_FIRMWARE_FLAGS_RESET\nundefine CFLAGS\nundefine CXXFLAGS\nundefine LDFLAGS\nexport VMAKE_FIRMWARE_FLAGS_RESET := 1\nendif"
            if err := pkg.Make("-C", filepath.ToSlash(srcDir), "--eval", ownFlags); err != nil {
                return err
            }
            installDir := filepath.Join(pkg.BuildDir(), "_install")
            return pkg.Make("-C", filepath.ToSlash(srcDir), "--eval", ownFlags, "CONFIG_PREFIX="+filepath.ToSlash(installDir), "install")
        })
    })
}
```

The guarded `ownFlags` reset preserves KBuild ownership of C/CXX/linker flags and does not clear flags in recursive Make invocations. `Make` selects the configured tool and inherits the session jobs budget.

`SetSrcDir("src")` tells vmake the source code is in `SourceDir/src/`. Use `pkg.SrcDir()` (not `pkg.SourceDir()`) to get this path. `EnsureConfig` looks for `.config` in `SrcDir`.

## myapp (Simple Binary)

```go
package main

import "github.com/spock2300/vmake/pkg/api"

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

    "github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires("busybox", "myapp")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        appOutput := ctx.DepOutput("myapp:myapp")
        busyboxBuildDir := ctx.DepBuildDir("busybox:busybox")

        ctx.Target("rootfs").SetKind(api.TargetVoid).
            AddDeps("busybox:busybox", "myapp:myapp").
            SetBuildFunc(func(pkg *api.Package) error {
            staging := filepath.Join(pkg.BuildDir(), "staging")
            os.RemoveAll(staging)
            os.MkdirAll(staging, 0755)

            if err := api.CopyDir(filepath.Join(pkg.SourceDir(), "overlay"), staging); err != nil {
                return err
            }

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
            pkg.Run("mksquashfs", staging, imageFile, "-noappend")
            return nil
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

    "github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires("uboot", "linux", "rootfs", "myapp")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ubootDir := ctx.DepBuildDir("uboot:uboot")
        linuxDir := ctx.DepBuildDir("linux:linux")
        rootfsDir := ctx.DepBuildDir("rootfs:rootfs")

        ctx.Target("firmware").SetKind(api.TargetVoid).
            AddDeps("uboot:uboot", "linux:linux", "rootfs:rootfs").
            SetBuildFunc(func(pkg *api.Package) error {
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
- **SetKConfigPatches** — Override specific config values after defconfig generation
- **SetSrcDir** — Source code in a subdirectory (`src/` for busybox)
- **SetConfigFiles** — Stores package configuration-file metadata; does not control target skipping
- **External incrementality** — Void callbacks run once per build session; Make checks configuration, sources, dependencies, and outputs
- **DepBuildDir** — `ctx.DepBuildDir("busybox:busybox")` returns the dependency's build directory
- **DepOutput** — `ctx.DepOutput("myapp:myapp")` returns the dependency's output binary path
- **api.CopyFile/CopyDir/CopyDirIfExists** — File copy utilities from the `api` package
- **Explicit AddDeps** — Targets must declare `AddDeps` for build-graph edges; `AddRequires` alone only handles package resolution
- **Empty root build.go** — All work happens in sub-packages; root only provides the workspace

## Key Points

- For a source-tree Makefile, call `pkg.Make("-C", filepath.ToSlash(pkg.SrcDir()), ...)`; return its error and let the helper apply the jobs budget
- Use `pkg.SrcDir()` (not `SourceDir()`) when the package has `SetSrcDir("src")`
- Local and remote void targets both run their callback once per session; a nonempty InstallDir does not skip the callback
- Presets are defconfig names passed to `make <preset>` — not complete `.config` files
- `EnsureConfig` also applies `SetKConfigPatches` patches after running `make <preset>`
- Guard `api.CopyDirIfExists` calls with `os.Stat` when the source may not exist
- Remove output files before regenerating (e.g., `os.Remove(imageFile)` before `mksquashfs`)
- Validate input files exist with `os.Stat` before packing firmware images

## See Also

- SKILL.md - KConfig Preset Management, Common Mistakes (pkg.Make vs pkg.RunIn)
- examples/embedded-rtos.md - Linker scripts, AddPostLink, AddBinHeader
- references/api.md - KConfigEntry, Package KConfig methods, DepBuildDir, DepOutput
