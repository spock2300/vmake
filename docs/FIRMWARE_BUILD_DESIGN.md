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
| **uboot** | — | `KConfig("u-boot")` | TargetVoid | `EnsureConfig && make` |
| **kernel** | — | `KConfig("linux")` | TargetVoid | `EnsureConfig && make` |
| **busybox** | — | `KConfig("busybox")` | TargetVoid | `EnsureConfig && make && make install` |
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

**无 InstallDir 的包**（闭包捕获 `DepOutput` / `DepBuildDir`）：

TargetVoid 目标没有自动安装产物，通过 `DepOutput` 获取产物路径或 `DepBuildDir` 获取 BuildDir：

```go
p.OnBuild(func(ctx *api.BuildContext) {
    appBin := ctx.DepOutput("myapp:myapp")
    busyboxBuildDir := ctx.DepBuildDir("busybox:busybox")
    ctx.Target("rootfs").
        SetKind(api.TargetVoid).
        SetBuildFunc(func(pkg *api.Package) error {
            // appBin, busyboxBuildDir 通过闭包传入
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
      "selected_preset": "sandbox_defconfig",
      "kconfig": "# CONFIG_LOCALVERSION_AUTO is not set\nCONFIG_SYS_TEXT_BASE=0x00200000\nCONFIG_CMD_BOOTM=y\nCONFIG_BOOTDELAY=3\n..."
    },
    "linux": {
      "version": "6.6",
      "selected_preset": "x86_64_defconfig",
      "kconfig": "#\n# Automatically generated file; DO NOT EDIT.\n# Linux/x86 6.6.0 Kernel Configuration\n#\nCONFIG_EXT4_FS=y\nCONFIG_NETFILTER=y\n..."
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
| 切换 preset | `make <presetName>` 生成 .config → 编码 → config.json kconfig |

---

## 3. KConfig 管理

### 3.1 新增类型

```go
type KConfigEntry struct {
    name           string
    description    string
    configPath     string
    srcDir         string
    menuconfigCmd  string
    presets        []string
    defaultPreset  string
    selectedPreset string
    patchValues    map[string]string
}
```

### 3.2 ConfigContext 扩展

```go
func (ctx *ConfigContext) KConfig(name string) *KConfigEntry
```

### 3.3 KConfigEntry API

```go
func (k *KConfigEntry) AddPreset(name string) *KConfigEntry
func (k *KConfigEntry) SetDefault(presetName string) *KConfigEntry
func (k *KConfigEntry) SetDescription(desc string) *KConfigEntry
func (k *KConfigEntry) SetConfigPath(path string) *KConfigEntry
func (k *KConfigEntry) SetSrcDir(dir string) *KConfigEntry
func (k *KConfigEntry) SetMenuconfigCmd(cmd string) *KConfigEntry
func (k *KConfigEntry) SetSelectedPreset(name string) *KConfigEntry
func (k *KConfigEntry) PatchKConfig(patches map[string]string) *KConfigEntry
func (k *KConfigEntry) Name() string
func (k *KConfigEntry) Description() string
func (k *KConfigEntry) ConfigPath() string
func (k *KConfigEntry) SrcDir() string
func (k *KConfigEntry) MenuconfigCmd() string
func (k *KConfigEntry) Presets() []string
func (k *KConfigEntry) DefaultPreset() string
func (k *KConfigEntry) SelectedPreset() string
func (k *KConfigEntry) Patches() map[string]string
```

### 3.4 使用模式

uboot、kernel、busybox 的 KConfig 用法完全一致，只是包名和预设名不同：

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.KConfig("u-boot").
        SetDescription("U-Boot configuration").
        AddPreset("sandbox_defconfig").
        AddPreset("rk3568_defconfig").
        SetDefault("sandbox_defconfig").
        SetMenuconfigCmd("make menuconfig")
})

p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.KConfig("linux").
        SetDescription("Linux kernel configuration").
        AddPreset("x86_64_defconfig").
        AddPreset("rk3568_defconfig").
        SetDefault("x86_64_defconfig").
        SetMenuconfigCmd("make menuconfig")
})

p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.KConfig("busybox").
        SetDescription("BusyBox applet configuration").
        SetSrcDir("src").
        AddPreset("defconfig").
        SetDefault("defconfig").
        PatchKConfig(map[string]string{
            "CONFIG_TC=y": "# CONFIG_TC is not set",
        })
})
```

### 3.5 预设配置

预设是 **defconfig 名称**（make target），不是文件路径。`make <presetName>` 生成 `.config`。

例如 `sandbox_defconfig`、`x86_64_defconfig`、`defconfig` 都是源码树中原生的 make target，直接传入 `AddPreset()` 即可。

---

## 3.6 EnsureConfig + PatchKConfig

### EnsureConfig

```go
func (p *Package) EnsureConfig(srcDir string) bool
```

`EnsureConfig` 检查 `.config` 是否存在且非空，若缺失则自动生成：

1. 检查 `srcDir/.config` 是否存在且 `size > 0`
2. 若缺失或为空，执行 `make <selectedPreset>` 生成 `.config`
3. 若包有 KConfig 条目，调用 `ApplyKConfigPatches` 应用 post-defconfig 补丁
4. 返回 `bool`：`true` 表示刚生成了配置，`false` 表示已存在

BuildFunc 中使用：

```go
srcDir := pkg.SourceDir()
pkg.EnsureConfig(srcDir)
pkg.RunIn(srcDir, "make", "-j"+strconv.Itoa(runtime.NumCPU()))
```

### PatchKConfig

```go
func (k *KConfigEntry) PatchKConfig(patches map[string]string) *KConfigEntry
```

Post-defconfig 值补丁，用于在 `make <preset>` 生成 `.config` 后覆盖特定配置项。例如：

```go
PatchKConfig(map[string]string{
    "CONFIG_TC=y": "# CONFIG_TC is not set",
})
```

补丁在三个位置生效：
- **EnsureConfig**：构建时自动应用
- **restoreKConfigFiles**：Phase 2.5 恢复 `.config` 后应用
- **TUI ensureConfigCmd**：交互式配置时应用

底层实现为 `api.ApplyKConfigPatches(configPath, patches)`，对 `.config` 文件做字符串替换。

---

## 3.7 SetConfigFiles + 构建缓存跳过

### SetConfigFiles

```go
func (p *Package) SetConfigFiles(files ...string) *Package
```

声明哪些文件的变化会导致构建缓存失效。通常在 `OnPackage` 中设置：

```go
p.OnPackage(func(p *api.Package) {
    p.SetConfigFiles(".config")
})
```

对于没有 InstallDir 的本地包，vmake 使用 `.vmake_stamp` 文件（位于 BuildDir）标记成功构建。当任何 ConfigFile 的时间戳比 stamp 文件新时，构建被视为 stale，会重新执行。

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
│   ├── preset           [sandbox_defconfig    ▶◀]
│   └── Run menuconfig...
├── linux
│   ├── preset           [x86_64_defconfig    ▶◀]
│   └── Run menuconfig...
└── busybox
    ├── preset           [defconfig            ▶◀]
    └── Run menuconfig...
```

### 4.2 preset 切换

使用左右箭头键循环切换预设。切换 preset 时会触发以下流程：

1. 若 `.config` 已存在，删除 `.config`（因为 preset 已变更，旧配置不再有效）
2. 更新 config.json 中的 `selected_preset`
3. 保存时将 kconfig 清空（preset 切换后尚未生成新 `.config`）

### 4.3 Run menuconfig 流程

menuconfig 采用两步执行：

**Step 1: ensureConfigCmd**
1. 检查 `.config` 是否存在
2. 若不存在，执行 `make <preset>` 生成初始配置
3. 若有 `PatchKConfig`，应用补丁

**Step 2: runMenuconfigCmd**
1. 执行 `KConfigEntry.MenuconfigCmd()`（默认 `make menuconfig`）
2. 用户退出后读取修改后的 `.config`
3. 编码为 JSON 字符串 → 更新 config.json

---

## 5. 交叉编译与 BuildDir

### 5.1 BuildDir 与 SourceDir 的关系

vmake 的 `Package` 有三个目录：`SourceDir`（源码目录）、`BuildDir`（构建目录）、`InstallDir`（安装目录）。

对于不同类型的包，BuildDir 的设置不同：

| 包类型 | BuildDir | 原因 |
|--------|----------|------|
| 本地包 | `<SourceDir>/build/<key>/` | 与源码分离，key 由工具链+模式+选项生成 |
| 远程包 | `<packagesDir>/<name>/<version>/<key>/build/` | 包管理目录下，与 InstallDir 同级 |

其中 `key` 由 `build.BuildKey(ccPath, mode, opts)` 生成，确保不同工具链/模式/选项的构建产物隔离。

### 5.2 交叉编译环境变量

vmake 的 `Toolchain` 结构已提供 `Host`（目标三元组）和 `Prefix`（编译器前缀）字段。`Toolchain.Env()` 会生成包含 `CROSS_COMPILE`、`CC`、`CXX` 等环境变量的 map。

`Package.Make()` 已自动传递 `pkg.Env()`：

```go
func (p *Package) Make(args ...string) error {
    makeArgs := []string{"-C", p.dirs.BuildDir}
    makeArgs = append(makeArgs, args...)
    return p.RunEnv(p.Env(), "make", makeArgs...)
}
```

对于 KBuild 系统（kernel/uboot/busybox），Makefile 在 SourceDir 中，需使用 `RunIn` 在源码目录执行 make：

```go
pkg.RunIn(srcDir, "make", "-j"+strconv.Itoa(runtime.NumCPU()))
```

### 5.3 Package 执行方法说明

| 方法 | 工作目录 | 环境变量 | 失败行为 |
|------|----------|----------|----------|
| `pkg.Make(args...)` | BuildDir | 自动传递 pkg.Env() | `os.Exit(1)` |
| `pkg.Run(cmd, args...)` | BuildDir | 无 | `os.Exit(1)` |
| `pkg.RunEnv(env, cmd, args...)` | BuildDir | 指定 env map | 返回 error |
| `pkg.RunIn(dir, cmd, args...)` | 指定 dir | 无 | `os.Exit(1)` |

> `Make()` 和 `Run()` 内部使用 `exec.RunFatal`，失败时直接 `os.Exit(1)` 不会返回 error。BuildFunc 中调用 `pkg.Make(...)` 无需检查返回值，进程已在失败时退出。注意 `pkg.Run()` 固定在 BuildDir 中执行，不可指定其他目录。

---

## 6. 典型包示例

### 6.1 uboot

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
            SetDescription("U-Boot configuration").
            AddPreset("sandbox_defconfig").
            AddPreset("rk3568_defconfig").
            AddPreset("stm32_defconfig").
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

### 6.2 kernel

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
        ctx.KConfig("linux").
            SetDescription("Linux kernel configuration").
            AddPreset("x86_64_defconfig").
            AddPreset("rk3568_defconfig").
            AddPreset("stm32_defconfig").
            SetDefault("x86_64_defconfig").
            SetMenuconfigCmd("make menuconfig")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("linux").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
            srcDir := pkg.SourceDir()
            pkg.EnsureConfig(srcDir)
            pkg.RunIn(srcDir, "make", "-j"+strconv.Itoa(runtime.NumCPU()))
            pkg.RunIn(srcDir, "make", "DESTDIR="+pkg.BuildDir(), "install")
            return nil
        })
    })
}
```

kernel 只创建一个 TargetVoid，BuildFunc 在源码目录完成构建。

### 6.3 busybox

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
        p.SetGit("https://git.busybox.net/busybox")
        p.SetConfigFiles(".config")
    })

    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.KConfig("busybox").
            SetDescription("BusyBox applet configuration").
            SetSrcDir("src").
            AddPreset("defconfig").
            SetDefault("defconfig").
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

Busybox 使用 `SetSrcDir("src")` 指定源码子目录，`PatchKConfig` 在 defconfig 生成后覆盖特定选项。`make CONFIG_PREFIX=<dir> install` 在 BuildDir 下生成 `_install` 目录：

```
<BuildDir>/_install/
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

### 6.4 应用（myapp）

```go
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
            imageFile := filepath.Join(pkg.BuildDir(), "rootfs.sqsh")
            os.RemoveAll(staging)

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

> 文件操作使用 `pkg/api/copy.go` 提供的 `api.CopyFile`、`api.CopyDir`、`api.CopyDirIfExists`，这些函数属于 `api` 包，build.go 插件可以直接 import 使用。

### 6.6 固件包（firmware）

firmware 只是依赖各分区的最后一个包，BuildFunc 收集各分区镜像文件合成最终固件：

```go
package main

import (
    "fmt"
    "os"
    "path/filepath"

    "gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires("uboot", "linux", "rootfs", "app")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ubootBuildDir := ctx.DepBuildDir("uboot:uboot")
        linuxBuildDir := ctx.DepBuildDir("linux:linux")
        rootfsBuildDir := ctx.DepBuildDir("rootfs:rootfs")
        appBuildDir := ctx.DepBuildDir("app:app")

        ctx.Target("firmware").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
            ubootBin := filepath.Join(ubootBuildDir, "u-boot.bin")
            zImage := filepath.Join(linuxBuildDir, "zImage")
            rootfsImg := filepath.Join(rootfsBuildDir, "rootfs.sqsh")
            appImg := filepath.Join(appBuildDir, "app.sqsh")

            for _, f := range []string{ubootBin, zImage, rootfsImg, appImg} {
                if _, err := os.Stat(f); err != nil {
                    return fmt.Errorf("missing partition image: %s", f)
                }
            }

            layout := []Partition{
                {"uboot", ubootBin, 0},
                {"kernel", zImage, 0},
                {"rootfs", rootfsImg, 0},
                {"app", appImg, 0},
            }

            return packImage(layout, filepath.Join(pkg.BuildDir(), "firmware.img"))
        })
    })
}
```

> firmware 的 BuildFunc 使用 `ctx.DepBuildDir()` 获取各依赖包的 BuildDir，再拼接分区镜像文件名。`DepBuildDir` 等效于 `filepath.Dir(DepOutput(...))`，是推荐 API。

---

## 7. 分区镜像路径约定

获取依赖包 BuildDir 的推荐方式是 `ctx.DepBuildDir("<pkg>:<target>")`：

```go
busyboxBuildDir := ctx.DepBuildDir("busybox:busybox")
```

路径关系：

```
DepBuildDir("busybox:busybox")  = <packagesDir>/busybox/<version>/<key>/build/
                                    ├── _install/        (make install 产物)
                                    └── busybox          (TargetVoid 占位文件)

DepBuildDir("rootfs:rootfs")    = <SourceDir>/build/<key>/
                                    ├── staging/          (中间目录)
                                    └── rootfs.sqsh       (分区镜像)
```

> `DepBuildDir(depRef)` 内部实现为 `filepath.Dir(ctx.DepOutput(depRef))`。TargetVoid 没有实际产物文件，`DepOutput` 返回的路径指向 `<BuildDir>/<targetName>`，该文件不存在但路径有效。下游包通过 `DepBuildDir` 获取 BuildDir，再按约定拼接文件名。

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
│   │
│   └── autoWireRequireDeps：为声明了 SetRequire 但未显式 AddDeps 的目标自动补全依赖边
│
├── Phase 2: OnConfig → 收集 Options + KConfig 条目
│   └── KConfig 条目注册完毕，但尚未恢复 .config
│
├── Phase 2.5: 恢复 .config（OnConfig 之后、OnBuild 之前）
│   └── 对每个有 kconfig 的包，按拓扑序调用 restoreKConfigFiles：
│       ├── config.json 无该包条目 → 跳过（不删除 .config）
│       ├── config.json 有条目但 kconfig 为空（preset 切换）→ 删除 .config
│       ├── config.json 有 kconfig 内容但与磁盘一致 → 跳过（避免 mtime 变化导致缓存失效）
│       └── config.json 有 kconfig 内容且与磁盘不同 → 写入 .config + ApplyKConfigPatches
│
├── Phase 3: OnBuild（拓扑序执行）
│   ├── myapp TargetBinary: 编译链接
│   ├── busybox TargetVoid: EnsureConfig + make + make install
│   ├── linux TargetVoid: EnsureConfig + make
│   ├── uboot TargetVoid: EnsureConfig + make
│   ├── rootfs TargetVoid: overlay + collect → staging → mksquashfs → rootfs.sqsh
│   ├── boot TargetVoid: zImage + dtb + overlay → staging → mkimage → boot.img
│   └── firmware TargetVoid: collect images → firmware.img
│
└── Phase 4: 保存配置（如有变更）
```

---

## 9. autoWireRequireDeps

`OnRequire`/`AddRequires` 仅声明依赖关系，**不会**自动创建构建图的边。目标必须通过 `AddDeps` 显式声明构建依赖（支持 `"pkg:target"` 指定 target、`"pkg:*"` 通配所有 target），否则拓扑排序不会产生正确的构建顺序。

为简化使用，vmake 提供 `autoWireRequireDeps()` 自动补全：

```go
func autoWireRequireDeps(pkg *api.Package, allTargets, localTargets map[string]map[string]*api.Target)
```

规则：若本地包的某个 Target 没有显式 `AddDeps`，但包级别声明了 `AddRequires("busybox")`，则自动将该 Target 的依赖指向 busybox 包的所有 Target。

这意味着大多数情况下，build.go 中只需写 `AddRequires` 而无需手动 `AddDeps`：

```go
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("busybox", "myapp")
})
```

`autoWireRequireDeps` 在 `build_cmd.go` 中 Phase 1 执行，自动为所有本地包的目标补全依赖边。

---

## 10. 完整目录结构

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
│   │   ├── src/                  (git clone 下载的源码)
│   │   │   └── ...
│   │   └── build.go
│   ├── linux/
│   │   └── build.go
│   ├── busybox/
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

## 11. 实施计划

| Phase | 内容 | 状态 |
|-------|------|------|
| **1** | `Package.Make()` 交叉编译扩展：自动传递 `pkg.Env()` | 已完成 |
| **2** | KConfig 基础：类型、API、config.json 扩展、编码/解码 | 已完成 |
| **3** | TUI 扩展：预设选择器、menuconfig 集成（两步执行） | 已完成 |
| **4** | 构建集成：.config 恢复（Phase 2.5）、EnsureConfig、PatchKConfig | 已完成 |
| **5** | 完整示例：test_data 固件项目（uboot + kernel + busybox + app + 分区 + firmware） | 已完成 |
| **6** | 高级功能：FIT Image、OTA A/B、签名、多板级管理 | 后续 |

---

## 12. 关键设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 核心原则 | 一切皆包 | uboot/kernel/busybox/app/partition/firmware 没有本质区别，统一模式降低复杂度 |
| .config 存储 | JSON 字符串 | 完整保留原始内容，含 `#` 注释 |
| KConfig | 统一 API | uboot/kernel/busybox 用完全相同的 KConfig API 管理配置 |
| TargetKind | TargetVoid | 复用现有机制，不增加复杂度 |
| 预设格式 | defconfig 名称（make target） | 兼容 U-Boot/Kernel/Busybox 原生格式，`make <preset>` 生成 .config |
| 配置生成 | EnsureConfig | 检查 .config 存在性，自动 `make <preset>` + PatchKConfig |
| 配置恢复 | restoreKConfigFiles skip rules | 无条目跳过、空 kconfig 删除、有内容仅变化时写入（避免 mtime 失效） |
| 交叉编译 | Make() 自动传递 Env() | 不改变 BuildFunc 使用方式，`pkg.Make()` 自动携带 CROSS_COMPILE |
| BuildDir | 与 SourceDir 分离 | 本地包 `<SourceDir>/build/<key>/`，远程包 `<packagesDir>/.../build/` |
| 构建缓存 | SetConfigFiles + stamp | ConfigFile 比 stamp 新则重新构建 |
| 分区 | 普通包 | BuildFunc 做 overlay + collect + 外部工具生成分区镜像，不新增 API |
| 固件 | 普通包 | 收集分区镜像文件 → 合成固件，完全用户可控 |
| 依赖产物路径 | DepBuildDir | 封装 `filepath.Dir(DepOutput(...))`，推荐 API |
| 文件操作 | api.CopyFile/CopyDir | 属于 pkg/api 包，build.go 插件可直接 import |
| autoWire | autoWireRequireDeps | AddRequires 自动补全构建图边，无需手动 AddDeps |
| pkg.Deps() | 有 InstallDir 即填充 | 不区分本地/远程，由 populateDepsFromGraph 统一处理 |
