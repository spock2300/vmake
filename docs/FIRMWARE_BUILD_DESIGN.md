# VMake 嵌入式固件构建扩展设计

## 概述

扩展 vmake 从应用编译工具升级为能构建完整嵌入式 Linux/RTOS 固件的工具。

**核心原则：一切皆包，一切皆依赖。**

uboot、kernel、busybox、app、分区、firmware 都是包。它们遵循完全相同的生命周期（OnRequire → OnConfig → OnBuild → OnInstall），只是每个阶段的实现不同。vmake 通过依赖图自动确定拓扑序，从最底层的工具链组件到最终的固件镜像，逐层构建。

**设计思路：**
1. **KConfig 管理** — `vmake config` 嵌套管理 U-Boot/Kernel/Busybox 的 `.config`，统一的预设和 menuconfig 流程
2. **依赖驱动构建** — 包与包之间只有依赖关系，BuildFunc 只关心自己的事，从 `pkg.Deps()` 和 `DepOutput()` 获取依赖产物
3. **分区即包** — rootfs、boot、data 等分区只是 BuildFunc 做目录合成 + 调用外部工具生成分区镜像的普通包，没有特殊 API
4. **固件即包** — firmware 只是依赖各分区镜像文件的最后一个包

---

## 1. 统一构建模式

### 1.1 包的四阶段生命周期

每个包都遵循相同的四阶段生命周期：

```
OnRequire → OnConfig → OnBuild → OnInstall
```

| 阶段 | 职责 | 常见操作 |
|------|------|----------|
| **OnRequire** | 声明依赖 | `AddRequires("busybox", "myapp")` |
| **OnConfig** | 声明配置项 | `Option("debug")`、`KConfig("busybox")` |
| **OnBuild** | 定义构建目标 | `Target("xxx").SetKind(...)` + `SetBuildFunc(...)` |
| **OnInstall** | 声明安装项 | `AddInstalls("_install/bin", "bin")` |

### 1.2 包类型对比

所有包都遵循同一个模式，差异仅在于每个阶段的具体实现：

| 包 | OnRequire | OnConfig | OnBuild | BuildFunc 做什么 |
|----|-----------|----------|---------|-----------------|
| **uboot** | — | `KConfig("u-boot")` | TargetVoid | `make olddefconfig && make` |
| **kernel** | — | `KConfig("linux")` | TargetVoid | `make olddefconfig && make zImage dtbs` |
| **busybox** | — | `KConfig("busybox")` | TargetVoid | `make olddefconfig && make && make install` |
| **myapp** | — | `Option("debug")` | TargetBinary | vmake 自动编译链接 |
| **rootfs** | `[busybox, myapp]` | — | TargetVoid | overlay + collect → staging → `mksquashfs` → `rootfs.sqsh` |
| **boot** | `[linux]` | — | TargetVoid | zImage + dtb + overlay → staging → `mkimage` → `boot.img` |
| **data** | `[myapp]` | — | TargetVoid | overlay → staging → `genext2fs` → `data.ext4` |
| **firmware** | `[rootfs, boot]` | — | TargetVoid | 收集各分区镜像文件 → 合成 `firmware.img` |

**关键观察：**

- uboot/kernel/busybox 的 OnConfig 和 OnBuild 结构完全一致，只是 make 命令不同
- myapp 用 vmake 原生 TargetBinary 编译，不需要 BuildFunc
- 分区包（rootfs/boot/data）的 OnBuild 结构一致：闭包捕获依赖路径 + SetBuildFunc 合成 staging + 调用外部工具生成分区镜像
- firmware 只是最后一个分区包，它的 BuildFunc 收集各分区镜像文件合成最终固件

### 1.3 依赖产物访问

BuildFunc 中访问依赖产物有两种方式：

**有 InstallDir 的包**（`pkg.Deps()` 自动填充）：

`populateDepsFromGraph` 会为所有有 InstallDir 的依赖包填充 `pkg.Deps()`，不区分本地或远程。没有 InstallDir 的包会被跳过。

```go
for _, dep := range pkg.Deps() {
    dep.InstallDir  // 完整安装目录
    dep.BinDir      // bin 子目录
    dep.LibDir      // lib 子目录
}
```

**无 InstallDir 的包**（闭包捕获 `DepOutput`）：

TargetVoid 目标没有自动安装产物，通过 `DepOutput` 获取 BuildDir 路径：

```go
p.OnBuild(func(ctx *api.BuildContext) {
    appBin := ctx.DepOutput("myapp:myapp")
    ctx.Target("rootfs").
        SetKind(api.TargetVoid).
        SetBuildFunc(func(pkg *api.Package) error {
            // appBin 通过闭包传入
            return nil
        })
})
```

---

## 2. 配置存储

### 2.1 config.json 格式

`.config` 文件（含 `#` 注释行）完整编码为 JSON 字符串：

```json
{
  "version": "1",
  "global": {
    "toolchain": "host",
    "mode": "release"
  },
  "entries": {
    "u-boot": {
      "version": "2024.01",
      "selected_preset": "rockchip_rk3568_defconfig",
      "kconfig": "# CONFIG_LOCALVERSION_AUTO is not set\nCONFIG_SYS_TEXT_BASE=0x00200000\nCONFIG_CMD_BOOTM=y\nCONFIG_BOOTDELAY=3\n..."
    },
    "linux": {
      "version": "6.6",
      "selected_preset": "multi_v7_defconfig",
      "kconfig": "#\n# Automatically generated file; DO NOT EDIT.\n# Linux/arm 6.6.0 Kernel Configuration\n#\nCONFIG_EXT4_FS=y\nCONFIG_NETFILTER=y\n..."
    },
    "busybox": {
      "selected_preset": "defconfig",
      "kconfig": "#\n# Automatically generated make config: don't edit\n# CONFIG_FEATURE_MOUNT_NFS is not set\nCONFIG_FEATURE_SH_IS_ASH=y\n..."
    }
  }
}
```

### 2.2 编码规则

- 原始 `.config` 内容按 UTF-8 读取
- 换行符编码为 `\n`，双引号转义 `\"`，反斜杠转义 `\\`
- 完整保留所有 `#` 注释行

### 2.3 同步流程

| 时机 | 操作 |
|------|------|
| OnConfig 之后、OnBuild 之前 | config.json kconfig → 源码目录 `.config` |
| menuconfig 后 | 源码目录 `.config` → config.json kconfig |
| 切换 preset | 预设文件 → 编码 → config.json kconfig |

---

## 3. KConfig 管理

### 3.1 新增类型

```go
// pkg/api/kconfig.go
type KConfigEntry struct {
    name          string
    presets       map[string]string  // preset_name → file_path
    defaultPreset string
    currentConfig string             // 当前配置（原始 .config 字符串）
}
```

### 3.2 ConfigContext 扩展

```go
func (ctx *ConfigContext) KConfig(name string) *KConfigEntry
```

### 3.3 KConfigEntry API

```go
func (k *KConfigEntry) AddPreset(name, configPath string) *KConfigEntry
func (k *KConfigEntry) SetDefault(presetName string) *KConfigEntry
func (k *KConfigEntry) Presets() []string
func (k *KConfigEntry) DefaultPreset() string
func (k *KConfigEntry) PresetPath(name string) string
func (k *KConfigEntry) CurrentConfig() string
func (k *KConfigEntry) SetCurrentConfig(config string) *KConfigEntry
```

### 3.4 使用模式

uboot、kernel、busybox 的 KConfig 用法完全一致，只是包名和预设文件不同：

```go
// packages/uboot/build.go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.KConfig("u-boot").
        AddPreset("rockchip_rk3568", "configs/rockchip_rk3568_defconfig").
        AddPreset("minimal", "configs/minimal.config").
        SetDefault("rockchip_rk3568")
})

// packages/linux/build.go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.KConfig("linux").
        AddPreset("multi_v7", "configs/multi_v7_defconfig").
        AddPreset("rockchip", "configs/rockchip_defconfig").
        SetDefault("rockchip")
})

// packages/busybox/build.go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.KConfig("busybox").
        AddPreset("defconfig", "configs/defconfig").
        AddPreset("full", "configs/full.config").
        SetDefault("defconfig")
})
```

### 3.5 预设配置目录

每个包在 SourceDir 下维护预设目录：

```
packages/<pkg>/
├── configs/
│   ├── <preset1>_defconfig
│   ├── <preset2>.config
│   └── ...
└── build.go
```

预设仅支持完整 `.config` 格式，包含所有选项和 `#` 注释行，可直接使用。

---

## 4. vmake config TUI 扩展

### 4.1 界面

```
VMake Configuration
├── [Global]
│   ├── toolchain        [host                    ]
│   └── mode             [release                 ]
├── myapp
│   └── debug            [ ] Enable debug mode
├── u-boot
│   ├── preset           [rockchip_rk3568         ▼]
│   └── Run menuconfig...
├── linux
│   ├── preset           [rockchip                 ▼]
│   └── Run menuconfig...
└── busybox
    ├── preset           [defconfig               ▼]
    └── Run menuconfig...
```

### 4.2 Run menuconfig 流程

1. 从 config.json 解码 kconfig → 写入源码目录 `.config`
2. `cd <source_dir> && make menuconfig`
3. 用户退出后读取 `.config`
4. 编码为 JSON 字符串 → 更新 config.json

---

## 5. 交叉编译与 BuildDir

### 5.1 BuildDir 与 SourceDir 的关系

vmake 的 `Package` 有三个目录：`SourceDir`（源码目录）、`BuildDir`（构建目录）、`InstallDir`（安装目录）。

对于不同类型的包，BuildDir 的设置不同：

| 包类型 | BuildDir | 原因 |
|--------|----------|------|
| kernel/uboot/busybox | = SourceDir | 这类项目使用 KBuild 系统，要求在源码树内构建（`make -C <dir>`） |
| myapp（TargetBinary） | = SourceDir | vmake 自动编译，BuildDir 就是源码目录 |
| 分区/firmware（TargetVoid） | = SourceDir | BuildFunc 使用 `pkg.BuildDir()` 作为 staging 和镜像输出目录 |

`Package.Make()` 内部实现为 `make -C <BuildDir>`，因此当 BuildDir = SourceDir 时，等效于在源码树内执行 make。

### 5.2 交叉编译环境变量

vmake 的 `Toolchain` 结构已提供 `Host`（目标三元组）和 `Prefix`（编译器前缀）字段。`Toolchain.Env()` 会生成包含 `CROSS_COMPILE`、`CC`、`CXX` 等环境变量的 map。

**问题：当前 `Package.Make()` 不传递环境变量。**

```go
// 当前实现：不传 env
func (p *Package) Make(args ...string) error {
    makeArgs := []string{"-C", p.dirs.BuildDir}
    makeArgs = append(makeArgs, args...)
    return p.Run("make", makeArgs...)
}
```

需要扩展 `Make()` 在工具链非 host 时自动传递 `pkg.Env()`：

```go
func (p *Package) Make(args ...string) error {
    makeArgs := []string{"-C", p.dirs.BuildDir}
    makeArgs = append(makeArgs, args...)
    if len(p.Env()) > 0 {
        return p.RunEnv(p.Env(), "make", makeArgs...)
    }
    return p.Run("make", makeArgs...)
}
```

扩展后，kernel/uboot/busybox 的 BuildFunc 使用 `pkg.Make("olddefconfig")` 即可自动获得 `CROSS_COMPILE=arm-linux-gnueabihf-` 等环境变量。

### 5.3 Package 执行方法说明

| 方法 | 工作目录 | 环境变量 | 失败行为 |
|------|----------|----------|----------|
| `pkg.Make(args...)` | BuildDir | 自动传递 pkg.Env()（扩展后） | `os.Exit(1)` |
| `pkg.Run(cmd, args...)` | BuildDir | 无 | `os.Exit(1)` |
| `pkg.RunEnv(env, cmd, args...)` | BuildDir | 指定 env map | 返回 error |
| `pkg.RunIn(dir, cmd, args...)` | 指定 dir | 无 | `os.Exit(1)` |

> `Make()` 和 `Run()` 内部使用 `exec.RunFatal`，失败时直接 `os.Exit(1)` 不会返回 error。BuildFunc 中调用 `pkg.Make(...)` 无需检查返回值，进程已在失败时退出。

---

## 6. 典型包示例

### 6.1 uboot

```go
// packages/uboot/build.go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.KConfig("u-boot").
            AddPreset("rockchip_rk3568", "configs/rockchip_rk3568_defconfig").
            SetDefault("rockchip_rk3568")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("uboot").
            SetKind(api.TargetVoid).
            SetBuildFunc(func(pkg *api.Package) error {
                pkg.Make("olddefconfig")
                pkg.Make()
                return nil
            })
    })
}
```

### 6.2 kernel

```go
// packages/linux/build.go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.KConfig("linux").
            AddPreset("rockchip", "configs/rockchip_defconfig").
            SetDefault("rockchip")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("linux").
            SetKind(api.TargetVoid).
            SetBuildFunc(func(pkg *api.Package) error {
                pkg.Make("olddefconfig")
                pkg.Make("zImage", "dtbs")
                return nil
            })
    })
}
```

kernel 只创建一个 TargetVoid，BuildFunc 一次完成 `zImage` 和 `dtbs` 的构建。

### 6.3 busybox

```go
// packages/busybox/build.go
package main

import (
    "os"
    "path/filepath"

    "gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.KConfig("busybox").
            AddPreset("defconfig", "configs/defconfig").
            AddPreset("full", "configs/full.config").
            SetDefault("defconfig")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("busybox").
            SetKind(api.TargetVoid).
            SetBuildFunc(func(pkg *api.Package) error {
                installDir := filepath.Join(pkg.BuildDir(), "_install")

                pkg.Make("olddefconfig")
                pkg.Make()

                os.RemoveAll(installDir)
                pkg.Run("make", "CONFIG_PREFIX="+installDir, "install")
                return nil
            })
    })
}
```

Busybox 的 `make CONFIG_PREFIX=<dir> install` 在 BuildDir 下生成 `_install` 目录：

```
packages/busybox/build/<buildKey>/_install/
├── bin/
│   ├── sh -> busybox
│   ├── ls -> busybox
│   └── ...
├── sbin/
│   ├── init -> busybox
│   └── ...
└── usr/
    ├── bin/
    └── sbin/
```

> 注意：`_install` 放在 `pkg.BuildDir()` 下而非 `pkg.SourceDir()` 下，确保所有构建产物都在 BuildDir 内。`pkg.Run("make", ...)` 内部通过 `exec.RunFatal` 执行，失败时进程直接退出。

### 6.4 应用（myapp）

```go
// packages/myapp/build.go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("debug").SetType(api.OptionBool).SetDefault(false)
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("myapp").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c").
            AddIncludes("include")
    })
}
```

vmake 自动编译、链接，`vmake install` 自动安装到 `<prefix>/bin/myapp`。

### 6.5 分区包（rootfs）

```go
// partitions/rootfs/build.go
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
        appBin := ctx.DepOutput("myapp:myapp")
        busyboxBuildDir := filepath.Dir(ctx.DepOutput("busybox:busybox"))

        ctx.Target("rootfs").
            SetKind(api.TargetVoid).
            SetBuildFunc(func(pkg *api.Package) error {
                staging := filepath.Join(pkg.BuildDir(), "staging")
                imageFile := filepath.Join(pkg.BuildDir(), "rootfs.sqsh")
                os.RemoveAll(staging)

                os.MkdirAll(staging, 0755)
                copyDir(filepath.Join(pkg.SourceDir(), "overlay"), staging)

                bbInstall := filepath.Join(busyboxBuildDir, "_install")
                if _, err := os.Stat(bbInstall); err == nil {
                    copyIfExists(filepath.Join(bbInstall, "bin"), filepath.Join(staging, "bin"))
                    copyIfExists(filepath.Join(bbInstall, "sbin"), filepath.Join(staging, "sbin"))
                    copyIfExists(filepath.Join(bbInstall, "usr"), filepath.Join(staging, "usr"))
                }

                if appBin != "" {
                    os.MkdirAll(filepath.Join(staging, "usr/bin"), 0755)
                    copyFile(appBin, filepath.Join(staging, "usr/bin", filepath.Base(appBin)))
                }

                os.Remove(imageFile)
                return pkg.Run("mksquashfs", staging, imageFile, "-noappend")
            })
    })
}
```

rootfs 的 overlay 是分区的基础目录骨架：

```
partitions/rootfs/
├── overlay/
│   ├── etc/
│   │   ├── init.d/
│   │   ├── passwd
│   │   ├── fstab
│   │   └── inittab
│   └── var/
│       └── log/
└── build.go
```

BuildFunc 先复制 overlay，再收集依赖产物覆盖写入，最后调用外部工具（如 `mksquashfs`）将 staging 目录打包为分区镜像文件。依赖产物优先级高于 overlay 同名文件。

> `copyDir`、`copyFile`、`copyIfExists` 为 build.go 中用户自定义的辅助函数。vmake 的 `pkg/build/copy.go` 提供了 `CopyFile`、`CopyDir`、`CopyDirWithFilter`，但属于 internal 包，build.go 插件无法直接 import。用户可在 build.go 中自行实现或引入第三方文件操作库。

### 6.6 分区包（boot）

```go
// partitions/boot/build.go
package main

import (
    "os"
    "path/filepath"

    "gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires("linux")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        linuxBuildDir := filepath.Dir(ctx.DepOutput("linux:linux"))

        ctx.Target("boot").
            SetKind(api.TargetVoid).
            SetBuildFunc(func(pkg *api.Package) error {
                staging := filepath.Join(pkg.BuildDir(), "staging")
                imageFile := filepath.Join(pkg.BuildDir(), "boot.img")

                os.MkdirAll(staging, 0755)
                copyDir(filepath.Join(pkg.SourceDir(), "overlay"), staging)
                copyIfExists(filepath.Join(linuxBuildDir, "arch/arm/boot/zImage"), filepath.Join(staging, "zImage"))
                copyIfExists(filepath.Join(linuxBuildDir, "arch/arm/boot/dts"), filepath.Join(staging, "dtbs"))

                return pkg.Run("mkimage", "-A", "arm", "-O", "linux", "-T", "kernel",
                    "-C", "none", "-a", "0x8000", "-e", "0x8000",
                    "-n", "Linux", "-d", filepath.Join(staging, "zImage"), imageFile)
            })
    })
}
```

每个分区包可以自由选择镜像生成工具：`mksquashfs`、`mkimage`、`genext2fs`、`mkfs.ext4` 等，由 BuildFunc 决定。

### 6.7 固件包（firmware）

firmware 只是依赖各分区的最后一个包，BuildFunc 收集各分区镜像文件合成最终固件：

```go
// firmware/build.go
package main

import (
    "path/filepath"

    "gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires("rootfs", "boot")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        rootfsBuildDir := filepath.Dir(ctx.DepOutput("rootfs:rootfs"))
        bootBuildDir := filepath.Dir(ctx.DepOutput("boot:boot"))

        ctx.Target("firmware").
            SetKind(api.TargetVoid).
            SetBuildFunc(func(pkg *api.Package) error {
                layout := []Partition{
                    {"boot",  filepath.Join(bootBuildDir, "boot.img"),    0x000000, 0x004000},
                    {"root",  filepath.Join(rootfsBuildDir, "rootfs.sqsh"), 0x004000, 0x100000},
                }
                return packImage(layout, filepath.Join(pkg.BuildDir(), "firmware.img"))
            })
    })
}

type Partition struct {
    Name   string
    Source string
    Offset int64
    Size   int64
}

func packImage(layout []Partition, output string) error {
    f, err := os.Create(output)
    if err != nil {
        return err
    }
    defer f.Close()
    for _, p := range layout {
        data, err := os.ReadFile(p.Source)
        if err != nil {
            return err
        }
        if _, err := f.WriteAt(data, p.Offset); err != nil {
            return err
        }
    }
    return nil
}
```

> firmware 的 BuildFunc 通过 `DepOutput` 获取各分区包的 BuildDir 路径（`filepath.Dir()` 去掉 target 名后缀），再拼接分区镜像文件名。分区镜像文件的具体名称（`rootfs.sqsh`、`boot.img` 等）由各分区包的 BuildFunc 决定，firmware 只需要知道约定。Partition 的 Offset 和 Size 单位为字节。

---

## 7. 分区镜像路径约定

`DepOutput("<pkg>:<target>")` 返回 `<pkgDir>/build/<buildKey>/<targetName>`，是一个**文件路径**而非目录路径。分区包使用 `filepath.Dir()` 提取 BuildDir，再拼接镜像文件名：

```
DepOutput("rootfs:rootfs")  = partitions/rootfs/build/<buildKey>/rootfs
    filepath.Dir(...)        = partitions/rootfs/build/<buildKey>/
                               ├── staging/           (中间目录)
                               └── rootfs.sqsh       (分区镜像)

DepOutput("boot:boot")      = partitions/boot/build/<buildKey>/boot
    filepath.Dir(...)        = partitions/boot/build/<buildKey>/
                               ├── staging/
                               └── boot.img

DepOutput("data:data")      = partitions/data/build/<buildKey>/data
    filepath.Dir(...)        = partitions/data/build/<buildKey>/
                               ├── staging/
                               └── data.ext4
```

> TargetVoid 没有实际产物文件，`DepOutput` 返回的路径指向 `<BuildDir>/<targetName>`，该文件不存在但路径有效。分区包的 BuildFunc 将 staging 目录和镜像文件都放在同一 BuildDir 下。下游包通过 `filepath.Dir(DepOutput(...))` 获取 BuildDir，再按约定拼接文件名。

---

## 8. 构建流程

```
vmake build
│
├── Phase 0: 加载配置
│   └── 读取 config.json（含 kconfig 字符串）
│
├── Phase 1: OnRequire → 解析依赖图
│   │
│   │   myapp ──┐
│   │   busybox ─┤
│   │            ├── rootfs ──┐
│   │   linux ──┤             ├── firmware
│   │            ├── boot ────┘
│   │   uboot ──────────────────┘
│
├── Phase 2: OnConfig → 收集 Options + KConfig 条目
│   └── KConfig 条目注册完毕，但尚未恢复 .config
│
├── Phase 2.5: 恢复 .config（OnConfig 之后、OnBuild 之前）
│   └── 对每个有 kconfig 的包：从 config.json 解码 → 写入 pkg.SourceDir()/.config
│   └── 确保 BuildFunc 中 make olddefconfig 能找到正确的 .config
│
├── Phase 3: OnBuild（拓扑序执行）
│   ├── myapp TargetBinary: 编译链接
│   ├── busybox TargetVoid: make olddefconfig && make && make install
│   ├── linux TargetVoid: make olddefconfig && make zImage dtbs
│   ├── uboot TargetVoid: make olddefconfig && make
│   ├── rootfs TargetVoid: overlay + collect → staging → mksquashfs → rootfs.sqsh
│   ├── boot TargetVoid: zImage + dtb + overlay → staging → mkimage → boot.img
│   └── firmware TargetVoid: collect rootfs.sqsh + boot.img → firmware.img
│
└── Phase 4: 保存配置（如有变更）
```

---

## 9. 完整目录结构

```
my-firmware/
├── .vmake/
│   └── config.json
├── partitions/
│   ├── rootfs/
│   │   ├── overlay/
│   │   │   ├── etc/
│   │   │   └── var/
│   │   └── build.go
│   ├── boot/
│   │   ├── overlay/
│   │   └── build.go
│   └── data/
│       ├── overlay/
│       └── build.go
├── packages/
│   ├── uboot/
│   │   ├── configs/
│   │   └── build.go
│   ├── linux/
│   │   ├── configs/
│   │   └── build.go
│   ├── busybox/
│   │   ├── configs/
│   │   └── build.go
│   └── myapp/
│       ├── src/
│       ├── include/
│       └── build.go
├── firmware/
│   └── build.go
└── keys/
    └── private.pem
```

---

## 10. 实施计划

| Phase | 内容 | 周期 |
|-------|------|------|
| **1** | `Package.Make()` 交叉编译扩展：自动传递 `pkg.Env()` | 0.5 周 |
| **2** | KConfig 基础：类型、API、config.json 扩展、编码/解码 | 1-2 周 |
| **3** | TUI 扩展：预设选择器、menuconfig 集成 | 1-2 周 |
| **4** | 构建集成：.config 恢复（Phase 2.5）、配置同步 | 1 周 |
| **5** | 完整示例：test_data 固件项目（uboot + kernel + busybox + app + 分区 + firmware） | 1-2 周 |
| **6** | 高级功能：FIT Image、OTA A/B、签名、多板级管理 | 后续 |

---

## 11. 关键设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 核心原则 | 一切皆包 | uboot/kernel/busybox/app/partition/firmware 没有本质区别，统一模式降低复杂度 |
| .config 存储 | JSON 字符串 | 完整保留原始内容，含 `#` 注释 |
| KConfig | 统一 API | uboot/kernel/busybox 用完全相同的 KConfig API 管理配置 |
| TargetKind | TargetVoid | 复用现有机制，不增加复杂度 |
| 预设格式 | 原始 .config/defconfig | 兼容 U-Boot/Kernel/Busybox 原生格式 |
| 配置同步 | OnConfig 之后恢复 | 确保 BuildFunc 中 `make olddefconfig` 能找到正确的 .config |
| 交叉编译 | 扩展 Make() 自动传递 Env() | 不改变 BuildFunc 使用方式，`pkg.Make("olddefconfig")` 自动携带 CROSS_COMPILE |
| BuildDir | = SourceDir | 所有包类型统一，简化路径推导 |
| 分区 | 普通包 | BuildFunc 做 overlay + collect + 外部工具生成分区镜像，不新增 API |
| 固件 | 普通包 | 收集分区镜像文件 → 合成固件，完全用户可控 |
| 依赖产物路径 | 闭包捕获 DepOutput + filepath.Dir | 现有 API，不新增接口 |
| pkg.Deps() | 有 InstallDir 即填充 | 不区分本地/远程，由 populateDepsFromGraph 统一处理 |
