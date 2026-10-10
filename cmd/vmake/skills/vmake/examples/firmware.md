# Firmware Build (KConfig, EnsureConfig, Partitions)

A multi-package firmware project using KConfig preset management, external incremental
builds, dependency-driven partition assembly, and `DepBuildDir` for accessing build
artifacts from other packages. The complete working example is the
`test_linux/17_firmware` fixture (U-Boot + Linux + BusyBox + apps + rootfs partitions +
raw firmware image); `docs/FIRMWARE_BUILD_DESIGN.md` explains the design.

## Prerequisites

| Requirement | Why |
|-------------|-----|
| `make` | `pkg.Make()` and `pkg.EnsureConfig()` run the selected toolchain's make; the KBuild packages build in their own source tree |
| `mksquashfs` (squashfs-tools) | the rootfs package calls it to produce `rootfs.sqsh` — an external host dependency, not bundled with vmake |
| `git` | `SetGit` downloads BusyBox (and U-Boot/kernel when you wrap upstream trees); a declared `AddVersion` pins the selected commit in `.vmake/vmake.lock` |
| a C compiler for the selected toolchain | `myapp` is compiled and linked by vmake itself |

The fixture's U-Boot/Linux trees are small Makefiles that mimic the real KBuild flow, so the
example builds without downloading a kernel. BusyBox is a real `SetGit` checkout.

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

The root `build.go` is empty — the project is composed entirely of sub-packages. Each sub-package is an independent unit managed by vmake's dependency system. The `firmware` package calls `p.SetRoot(true)` (see below) so vmake knows where to start the build; without a declared root, vmake falls back to heuristics and prints a hint. See `SKILL.md - Build Scope`.

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

## Linux (KConfig + EnsureConfig)

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
        ctx.KConfig("linux").
            AddPreset("x86_64_defconfig").
            AddPreset("rk3568_defconfig").
            SetDefaultPreset("x86_64_defconfig").
            SetMenuconfigCmd("make", "menuconfig")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("linux").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
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

U-Boot and Linux are local packages: their sources live directly in the package directory, so `pkg.SourceDir()` is the correct path. To wrap a real U-Boot/kernel checkout instead, add `p.SetGit(...)` in `OnPackage` and read `pkg.SrcDir()` — see BusyBox below.

## BusyBox (SetGit + KConfig)

```go
package main

import (
    "path/filepath"

    "github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
    p.OnPackage(func(p *api.Package) {
        p.SetGit("https://git.busybox.net/busybox")
        p.AddVersion("1.36.1", "1_36_1") // optional pin; omit to track the default branch
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

`SetGit` is what makes `SrcDir()` return `SourceDir()/src/` for this local package, which is why the build function uses `pkg.SrcDir()`. The KConfig entry's `SetSrcDir("src")` is a different setting: it only tells the config TUI and the `.config` restore logic where the KConfig `.config` lives relative to `SourceDir()`; it does not change `pkg.SrcDir()`. `EnsureConfig(srcDir)` takes the directory as an argument and looks for the KConfig entry's `ConfigPath()` file (default `.config`) under it, so pass `pkg.SrcDir()` here. The package-level `p.SetSrcDir(...)` is the raw source-directory override behind `SrcDir()`; with `SetGit` you get `SourceDir()/src` automatically, so you normally do not call it.

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
                if err := os.MkdirAll(staging, 0755); err != nil {
                    return err
                }

                if err := api.CopyDir(filepath.Join(pkg.SourceDir(), "overlay"), staging); err != nil {
                    return err
                }

                bbInstall := filepath.Join(busyboxBuildDir, "_install")
                if err := api.CopyDirIfExists(filepath.Join(bbInstall, "bin"), filepath.Join(staging, "bin")); err != nil {
                    return err
                }
                if err := api.CopyDirIfExists(filepath.Join(bbInstall, "sbin"), filepath.Join(staging, "sbin")); err != nil {
                    return err
                }
                if err := api.CopyDirIfExists(filepath.Join(bbInstall, "usr"), filepath.Join(staging, "usr")); err != nil {
                    return err
                }

                if appOutput != "" {
                    if err := os.MkdirAll(filepath.Join(staging, "usr", "bin"), 0755); err != nil {
                        return err
                    }
                    if err := api.CopyFile(appOutput, filepath.Join(staging, "usr", "bin", filepath.Base(appOutput))); err != nil {
                        return err
                    }
                }

                imageFile := filepath.Join(pkg.BuildDir(), "rootfs.sqsh")
                os.Remove(imageFile)
                pkg.Run("mksquashfs", staging, imageFile, "-noappend")
                return nil
            })
    })
}
```

`DepBuildDir` returns the dependency's `BuildDir` — used to locate build artifacts like busybox's `_install` directory. `DepOutput` returns the output binary path (empty during dry runs, hence the `appOutput != ""` guard).

`CopyDirIfExists` already stats its source and returns `nil` when the directory does not exist (for example an absent `_install/usr`); it only fails on a real copy error. Check its returned error instead of guarding with `os.Stat`.

## Firmware (Final Assembly, SetRoot)

```go
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
    partKernel     = partUboot + partUbootSize
    partKernelSize = 8 * 1024 * 1024
    partRootfs     = partKernel + partKernelSize
    partRootfsSize = 32 * 1024 * 1024
    totalImage     = partRootfs + partRootfsSize
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
        ctx.AddRequires("uboot", "linux", "rootfs")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ubootDir := ctx.DepBuildDir("uboot:uboot")
        linuxDir := ctx.DepBuildDir("linux:linux")
        rootfsDir := ctx.DepBuildDir("rootfs:rootfs")

        ctx.Target("firmware").SetKind(api.TargetVoid).
            AddDeps("uboot:uboot", "linux:linux", "rootfs:rootfs").
            SetBuildFunc(func(pkg *api.Package) error {
                layout := []partition{
                    {"uboot", filepath.Join(ubootDir, "u-boot.bin"), partUboot, partUbootSize},
                    {"kernel", filepath.Join(linuxDir, "zImage"), partKernel, partKernelSize},
                    {"rootfs", filepath.Join(rootfsDir, "rootfs.sqsh"), partRootfs, partRootfsSize},
                }
                return packImage(layout, filepath.Join(pkg.BuildDir(), "firmware.img"))
            })
    })
}

func packImage(layout []partition, output string) error {
    for _, part := range layout {
        if _, err := os.Stat(part.source); err != nil {
            return fmt.Errorf("missing partition source %s: %w", part.name, err)
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

    for _, part := range layout {
        data, err := os.ReadFile(part.source)
        if err != nil {
            return err
        }
        if _, err := f.WriteAt(data, part.offset); err != nil {
            return err
        }
        written := int64(len(data))
        if written < part.size {
            if err := padWith(f, part.offset+written, part.size-written, padding); err != nil {
                return err
            }
        }
        fmt.Printf("  [%-12s] offset=0x%07x size=%d (%d MB reserved)\n",
            part.name, part.offset, written, part.size/(1024*1024))
    }

    fmt.Printf("  firmware.img: total=%d bytes (%.1f MB)\n", totalImage, float64(totalImage)/(1024*1024))
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
```

`p.SetRoot(true)` marks the entry-point package; at most one local package may declare it, and vmake traverses the build graph from that root. `packImage` validates every source first, creates `firmware.img` truncated to the fixed total size, writes each payload at its partition offset, and pads the remaining partition space with `0xFF` (the flash erase value). The fixture's `packImage` also supports directory sources (`writeDirFlat` in `test_linux/17_firmware/firmware/build.go`) for a plain-file config partition.

`firmware.img` is a raw disk image laid out at fixed offsets; vmake never touches the device. Flash it with vendor tooling (write the image to SD/eMMC, or use a vendor download tool), or wrap your flashing tool in an extension plugin that registers a `vmake <plugin> flash` subcommand (see `docs/EXTENSION_PLUGIN.md`).

## Post-Link Artifacts (HEX / BIN / Size / Strip)

The firmware image above is assembled by `packImage`. When a target should instead emit extra artifacts after linking (ROM images, `objcopy` conversions, size reports), use the post-link shorthands:

```go
ctx.Target("myapp").
    SetKind(api.TargetBinary).
    AddFiles("src/*.c").
    AddPostLinkSize().
    AddPostLinkHex().
    AddPostLinkBin().
    AddPostLinkStrip()
```

- `AddPostLinkHex`/`AddPostLinkBin`/`AddPostLinkStrip` run `objcopy`/`strip` and declare their outputs (`{output}.hex`, `{output}.bin`, `{output}.stripped`) automatically.
- `AddPostLink(tool, args...)` runs a custom step; use the `{output}` placeholder and `AddPostLinkOutputs(...)` to declare what it writes — command arguments never imply outputs.
- `AddPostLinkDeps(...)` declares extra input files (SourceDir-relative) that must trigger relinking when changed.
- `Package.ObjCopy()`, `Size()`, `ObjDump()`, `NM()` return the selected toolchain's tools for custom commands.

See `examples/embedded-rtos.md` for the full post-link table and `SKILL.md - RTOS / Embedded`.

## Cross-Compiling

The targets vmake compiles itself (`myapp`, plus any `AddFiles` target in a wrapper) follow the project platform. Declare it as global options and select the compiler with `--toolchain`:

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    // Values may stay unset when the selected toolchain declares target_os/target_triple defaults.
    ctx.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("linux")
    ctx.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString).SetDefault("arm-linux-gnueabihf")
    ctx.AddGlobalCFlags("-mcpu=cortex-a53") // CPU/ABI flags always come from the project
})
```

KBuild packages (`uboot`, `linux`, `busybox`) inherit the selected toolchain through `pkg.Make()`/`pkg.Env()` (`CROSS_COMPILE`, `CC`, `CXX`, …), so their own Makefiles cross-compile without extra wiring. Toolchains contributed by extensions may carry target defaults but never CPU flags — supply those yourself. See `SKILL.md - Cross-Compiling`.

## Running / Verifying

```bash
# optional: pick KConfig presets or run menuconfig interactively (presets are not options,
# so they are selected in the TUI; vmake config --set ... configures regular options only)
vmake config

# build the whole graph and assemble the image
vmake build
```

Expected result:

- `vmake build` logs `Executing OnBuild...`, runs each KBuild package once, prints the firmware partition table (`[uboot] offset=0x... size=...`), and finishes with `firmware.img: total=... bytes`.
- Artifacts land under each package's `build/<buildKey>/`: `firmware/build/<buildKey>/firmware.img`, `rootfs/build/<buildKey>/rootfs.sqsh`, `busybox/build/<buildKey>/_install/`, `linux/build/<buildKey>/zImage`.
- KConfig `.config` files are restored from the active configuration before `OnBuild`; switching a preset clears the stored config and regenerates it on the next build.

## What This Demonstrates

- **KConfig presets** — `AddPreset("defconfig")` registers a defconfig name as a make target
- **EnsureConfig** — `pkg.EnsureConfig(srcDir)` checks the `ConfigPath()` file (default `<srcDir>/.config`) exists + non-empty, runs `make <preset>` if missing
- **SetKConfigPatches** — Override specific config values after defconfig generation
- **SetGit** — Local packages download source to `SourceDir()/src`; `SrcDir()` returns that tree
- **KConfig SetSrcDir** — Locates the KConfig `.config` for the config TUI and restore logic; it does not set the package source dir
- **SetConfigFiles** — Stores package configuration-file metadata; does not control target skipping
- **SetRoot** — One package declares `p.SetRoot(true)` to anchor the build graph
- **External incrementality** — Void callbacks run once per build session; Make checks configuration, sources, dependencies, and outputs
- **DepBuildDir** — `ctx.DepBuildDir("busybox:busybox")` returns the dependency's build directory
- **DepOutput** — `ctx.DepOutput("myapp:myapp")` returns the dependency's output binary path
- **api.CopyFile/CopyDir/CopyDirIfExists** — File copy utilities from the `api` package; `CopyDirIfExists` is a no-op for a missing source
- **Explicit AddDeps** — Targets must declare `AddDeps` for build-graph edges; `AddRequires` alone only handles package resolution
- **Empty root build.go** — All work happens in sub-packages; the firmware package provides the root

## Key Points

- For a source-tree Makefile, call `pkg.Make("-C", filepath.ToSlash(pkg.SrcDir()), ...)`; return its error and let the helper apply the jobs budget
- Use `pkg.SrcDir()` when the package downloads source with `SetGit`; package-level `SetSrcDir("src")` and `SetGit` are what map `SrcDir()` to `SourceDir()/src`
- `EnsureConfig` also applies `SetKConfigPatches` patches after running `make <preset>`, and fails the build if no preset is selected
- Local and remote void targets both run their callback once per session; a nonempty InstallDir does not skip the callback
- Presets are defconfig names passed to `make <preset>` — not complete `.config` files
- `CopyDirIfExists` returns `nil` when the source is missing; check its returned error rather than pre-checking with `os.Stat`
- Remove output files before regenerating (e.g., `os.Remove(imageFile)` before `mksquashfs`)
- Validate input files exist with `os.Stat` before packing firmware images
- `mksquashfs` is an external host dependency; `vmake doctor` does not install it

## See Also

- SKILL.md - KConfig Preset Management (Firmware)
- SKILL.md - Cross-Compiling
- SKILL.md - Build Scope
- SKILL.md - Common Mistakes
- examples/embedded-rtos.md - post-link Hex/Bin/Size/Strip, AddPostLinkOutputs, AddBinHeader
- references/api.md - KConfigEntry, Package KConfig methods, DepBuildDir, DepOutput
- references/dirs.md - SrcDir/SourceDir rules for local SetGit packages
