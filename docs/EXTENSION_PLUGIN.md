# VMake 扩展插件指南

扩展插件通过 Go 插件（`.so`）扩展 vmake 的 CLI 命令和工具链管理能力。插件存储在 `~/.vmake/extensions/<repo>/<plugin>/`。

## 快速开始

创建一个最小插件，包含 `plugin.json` 和 `src/main.go`：

**plugin.json**:
```json
{
  "name": "hello",
  "version": "1.0.0",
  "description": "Hello extension plugin",
  "entry": "src/main.go",
  "enabled": true
}
```

**src/main.go**:
```go
package main

import (
    "fmt"

    "gitee.com/spock2300/vmake/pkg/plugin"
    "github.com/spf13/cobra"
)

func Main(ctx *plugin.Context) {
    ctx.AddSubCommand(&cobra.Command{
        Use:   "world",
        Short: "Print hello world",
        Run: func(cmd *cobra.Command, args []string) {
            fmt.Println("Hello from extension plugin!")
        },
    })
}
```

添加扩展仓库并运行：

```bash
vmake ext add myext https://gitee.com/myorg/myext.git
vmake hello world
```

vmake 在启动时自动发现并编译插件。首次运行编译后重启 vmake 即可使用。

## 目录结构

```
~/.vmake/extensions/
└── <repo-name>/
    ├── <plugin-a>/              # 插件 A
    │   ├── plugin.json
    │   ├── src/main.go
    │   └── plugin.so            # 编译产物（自动生成）
    ├── <plugin-b>/              # 插件 B
    │   ├── plugin.json
    │   └── src/main.go
    ├── <toolchain-name>/        # 工具链声明
    │   └── toolchain.json
    └── assets/toolchains/       # 工具链压缩包（Git LFS）
        └── *.tar.gz
```

每个扩展仓库是一个 Git 仓库。仓库根目录下的每个子目录可以是一个插件（含 `plugin.json`）或一个工具链声明（含 `toolchain.json`）。一个仓库可以包含任意数量的插件和工具链，vmake 启动时自动发现并加载所有插件。

示例：一个仓库 `embedded-tools` 包含烧录和监控两个插件，以及一个 arm-gcc 工具链声明：

```
~/.vmake/extensions/embedded-tools/
├── flash/
│   ├── plugin.json           # name: "flash"
│   └── src/main.go
├── monitor/
│   ├── plugin.json           # name: "monitor"
│   └── src/main.go
├── arm-gcc-12.2/
│   └── toolchain.json
└── assets/
    └── toolchains/
        └── arm-gcc-12.2.0.tar.gz
```

对应的 CLI 命令为 `vmake flash ...` 和 `vmake monitor ...`。工具链通过 `tc` 插件自动发现注册。

## plugin.json

插件元信息文件，必须放在插件目录根下。

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 插件名，同时也是 CLI 命令名（`vmake <name>`） |
| `version` | string | 是 | 语义版本号 |
| `description` | string | 否 | 一句话描述，显示在 `vmake ext list` |
| `entry` | string | 是 | 入口 Go 源文件的相对路径（如 `src/main.go`） |
| `enabled` | bool | 否 | 是否启用，默认 `true`。设为 `false` 时跳过该插件 |

示例：

```json
{
  "name": "esp32-toolchain",
  "version": "2.0.0",
  "description": "ESP32 cross-compilation toolchain manager",
  "entry": "src/main.go",
  "enabled": true
}
```

## plugin.Context 接口

插件入口函数 `Main` 接收一个 `*plugin.Context` 参数，包含以下字段和方法。

### 只读字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `VMakeDir` | `string` | `~/.vmake` 目录的绝对路径 |
| `PluginDir` | `string` | 当前插件目录的绝对路径 |
| `RepoDir` | `string` | 当前插件所在仓库目录的绝对路径 |
| `CommandName` | `string` | 当前插件的命令名（即 `plugin.json` 中的 `name`） |

### AddSubCommand

```go
AddSubCommand func(cmd *cobra.Command)
```

为当前插件注册一个子命令。插件启动时 vmake 已创建一个与插件同名的根命令（`vmake <plugin-name>`），`AddSubCommand` 在此根命令下添加子命令。

```go
func Main(ctx *plugin.Context) {
    ctx.AddSubCommand(&cobra.Command{
        Use:   "flash",
        Short: "Flash firmware to device",
        Args:  cobra.ExactArgs(1),
        Run: func(cmd *cobra.Command, args []string) {
            fmt.Printf("Flashing %s...\n", args[0])
        },
    })
}
```

最终形成命令：`vmake esp32-toolchain flash <file>`

### RegisterToolchain

```go
RegisterToolchain func(name string, tc *toolchain.Toolchain)
```

注册一个自定义工具链。注册后用户可以通过 `--toolchain <name>` 或在 `build.go` 中设置 `toolchain` 选项来选择该工具链。

```go
ctx.RegisterToolchain("riscv32", &toolchain.Toolchain{
    Name:        "riscv32",
    DisplayName: "RISC-V 32-bit",
    Host:        "x86_64-linux-gnu",
    Prefix:      "riscv32-unknown-elf",
    Tools: toolchain.Tools{
        CC:  "riscv32-unknown-elf-gcc",
        CXX: "riscv32-unknown-elf-g++",
        AR:  "riscv32-unknown-elf-ar",
    },
    DefaultFlags: toolchain.DefaultFlags{
        CFlags:   []string{"-O2", "-march=rv32im", "-mabi=ilp32"},
        CxxFlags: []string{"-O2", "-march=rv32im", "-mabi=ilp32"},
    },
    InstallPath: filepath.Join(ctx.VMakeDir, "toolchains", "riscv32"),
})
```

### GetToolchains

```go
GetToolchains func() map[string]*toolchain.Toolchain
```

获取所有已注册的工具链（内置 `host` + 扩展注册的工具链）。

```go
for name, tc := range ctx.GetToolchains() {
    fmt.Printf("%s: %s\n", name, tc.DisplayName)
}
```

### SetOnMissing

```go
SetOnMissing func(toolchainName string, onMissing func(name string) (*toolchain.Toolchain, error))
```

设置工具链缺失回调。第一个参数是工具链名称，用于区分不同工具链的缺失回调，支持每个工具链独立的下载逻辑。

```go
ctx.SetOnMissing("arm-gcc", func(name string) (*toolchain.Toolchain, error) {
    // 下载 arm-gcc 工具链...
})
```

### AddGlobalFlags

```go
AddGlobalFlags func(cflags, cxxflags []string)
```

为所有构建目标注入全局 C/CXX 编译选项。影响所有使用 vmake 构建的项目。

```go
ctx.AddGlobalFlags(
    []string{"-ffunction-sections", "-fdata-sections"},
    []string{"-ffunction-sections", "-fdata-sections"},
)
```

### AddGlobalLdFlags

```go
AddGlobalLdFlags func(flags ...string)
```

为所有构建目标注入全局链接选项。影响所有使用 vmake 构建的项目。

```go
ctx.AddGlobalLdFlags("-Wl,--gc-sections", "-Wl,--as-needed")
```

### DownloadFile

```go
DownloadFile func(url, dest string) error
```

从 URL 下载文件到本地路径。使用 `curl -L -o` 实现，自动创建目标目录的父目录。

```go
err := ctx.DownloadFile(
    "https://github.com/espressif/esp-idf/releases/download/v5.1/esp-idf.tar.gz",
    filepath.Join(ctx.VMakeDir, "cache", "esp-idf.tar.gz"),
)
```

### ExtractToDir

```go
ExtractToDir func(archive, dest, format string) error
```

解压归档文件到目标目录。支持格式：`tar.gz`、`tar.xz`、`tar.bz2`、`zip`。`format` 为空时根据文件扩展名自动检测。

```go
err := ctx.ExtractToDir(
    filepath.Join(ctx.VMakeDir, "cache", "esp-idf.tar.gz"),
    filepath.Join(ctx.VMakeDir, "toolchains"),
    "tar.gz",
)
```

### RunGitLFS

```go
RunGitLFS func(repoDir string, args ...string) error
```

在指定目录执行 `git lfs` 命令。典型用途是拉取扩展仓库中通过 Git LFS 存储的工具链压缩包。

```go
err := ctx.RunGitLFS(pluginDir, "pull", "--include", "assets/toolchains/aarch64-gcc.tar.gz")
```

### RegisterToolchainsFromRepo

```go
RegisterToolchainsFromRepo func()
```

扫描插件仓库中子目录的 `toolchain.json` 文件，注册声明的工具链并为含 `install` 配置的工具链设置自动下载回调。通常由 `tc` 插件在 `Main` 中调用。

```go
ctx.RegisterToolchainsFromRepo()
```

### LoadToolchainDef

```go
LoadToolchainDef func() (*toolchain.ToolchainDef, error)
```

从插件目录加载 `toolchain.json` 文件，返回 `ToolchainDef`。

```go
def, err := ctx.LoadToolchainDef()
```

## 工具链类型

### toolchain.Toolchain

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 工具链标识符（如 `"aarch64-linux-gnu"`） |
| `DisplayName` | `string` | 可读名称（如 `"ARM GCC 12.2.0"`），`vmake toolchain list` 显示 |
| `Host` | `string` | 宿主平台三元组（如 `"x86_64-linux-gnu"`） |
| `Prefix` | `string` | 交叉编译前缀（如 `"aarch64-linux-gnu"`），设为 `""` 表示无前缀 |
| `Tools` | `Tools` | 各工具的可执行文件名 |
| `DefaultFlags` | `DefaultFlags` | 默认编译/链接选项 |
| `InstallPath` | `string` | 工具链安装目录的绝对路径 |

### toolchain.Tools

| 字段 | 类型 | 说明 | 示例 |
|------|------|------|------|
| `CC` | `string` | C 编译器 | `"aarch64-linux-gnu-gcc"` |
| `CXX` | `string` | C++ 编译器 | `"aarch64-linux-gnu-g++"` |
| `AR` | `string` | 静态库打包工具 | `"aarch64-linux-gnu-ar"` |
| `LD` | `string` | 链接器 | `"aarch64-linux-gnu-ld"` |
| `STRIP` | `string` | 符号剥离工具 | `"aarch64-linux-gnu-strip"` |
| `RANLIB` | `string` | 归档索引生成 | `"aarch64-linux-gnu-ranlib"` |
| `OBJCOPY` | `string` | 目标文件转换 | `"aarch64-linux-gnu-objcopy"` |
| `SIZE` | `string` | 大小报告 | `"aarch64-linux-gnu-size"` |
| `OBJDUMP` | `string` | 反汇编 | `"aarch64-linux-gnu-objdump"` |
| `NM` | `string` | 符号列表 | `"aarch64-linux-gnu-nm"` |

`CC` 和 `CXX` 是必填项，其余可选。

### toolchain.DefaultFlags

| 字段 | 类型 | 说明 |
|------|------|------|
| `CFlags` | `[]string` | 默认 C 编译选项 |
| `CxxFlags` | `[]string` | 默认 C++ 编译选项 |
| `LdFlags` | `[]string` | 默认链接选项 |

### Toolchain.Env()

`Toolchain` 提供一个 `Env()` 方法，返回环境变量映射：

| 变量 | 来源 |
|------|------|
| `CC` | `Tools.CC` |
| `CXX` | `Tools.CXX` |
| `LD` | `Tools.LD` |
| `AR` | `Tools.AR` |
| `CFLAGS` | `DefaultFlags.CFlags`（空格拼接） |
| `CXXFLAGS` | `DefaultFlags.CxxFlags`（空格拼接） |
| `LDFLAGS` | `DefaultFlags.LdFlags`（空格拼接） |
| `CROSS_COMPILE` | `Prefix`（仅当非空时） |
| `OBJCOPY` | `Tools.OBJCOPY`（仅当非空时） |
| `SIZE` | `Tools.SIZE`（仅当非空时） |
| `OBJDUMP` | `Tools.OBJDUMP`（仅当非空时） |
| `NM` | `Tools.NM`（仅当非空时） |

## 工具链资源

扩展可随仓库提供预编译工具链，通过 `toolchain.json` 声明。

### toolchain.json 格式

```json
{
  "name": "arm-gcc",
  "version": "12.2.0",
  "display_name": "ARM GCC 12.2.0",
  "host": "arm-linux-gnueabihf",
  "prefix": "arm-linux-gnueabihf",
  "tools": {
    "cc": "arm-linux-gnueabihf-gcc",
    "cxx": "arm-linux-gnueabihf-g++",
    "ar": "arm-linux-gnueabihf-ar",
    "ld": "arm-linux-gnueabihf-ld",
    "strip": "arm-linux-gnueabihf-strip"
  },
  "default_flags": {
    "cflags": ["-mcpu=cortex-a7", "-mfpu=neon-vfpv4", "-mfloat-abi=hard"],
    "cxxflags": ["-mcpu=cortex-a7", "-mfpu=neon-vfpv4", "-mfloat-abi=hard"],
    "ldflags": ["-mcpu=cortex-a7", "-mfpu=neon-vfpv4", "-mfloat-abi=hard"]
  },
  "install": {
    "method": "lfs",
    "file": "arm-gcc-12.2.0.tar.gz",
    "format": "tar.gz"
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 工具链标识符，用于 `--toolchain <name>` |
| `version` | string | 是 | 版本号 |
| `display_name` | string | 否 | 可读名称，默认同 `name` |
| `host` | string | 是 | 宿主平台三元组 |
| `prefix` | string | 是 | 交叉编译前缀，为空表示无前缀（native） |
| `tools` | object | 是 | 各工具的可执行文件名（同 `toolchain.Tools`，`cc` 和 `cxx` 必填） |
| `default_flags` | object | 否 | 默认编译/链接选项（`cflags`/`cxxflags`/`ldflags`） |
| `install` | object | 否 | 自动下载配置，不配置则需手动安装 |

**install 对象字段**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `method` | string | 是 | 下载方法：`"lfs"`（Git LFS）或 `"http"` |
| `file` | string | 是 | 压缩包文件名（放在扩展仓库 `assets/toolchains/` 下） |
| `url` | string | 否 | HTTP 下载URL（method 为 http 时必填） |
| `format` | string | 否 | 压缩格式（`tar.gz`/`tar.xz`/`tar.bz2`/`zip`），为空时自动检测 |
| `sha256` | string | 否 | SHA256 校验和，可选 |

### 自动下载机制

工具链自动下载通过 `tc` 插件 + `RegisterToolchainsFromRepo()` 实现：

1. `tc` 插件的 `Main` 函数调用 `ctx.RegisterToolchainsFromRepo()`
2. 该方法使用 `ScanRepoToolchains()` 扫描扩展仓库根目录下的所有子目录，查找 `toolchain.json`
3. 对每个包含 `install` 字段的工具链，调用 `SetOnMissing` 注册按需下载回调
4. 当用户通过 `--toolchain <name>` 或在 `build.go` 选择未安装的工具链时：
   - `method: "lfs"` → 执行 `git lfs pull` 拉取压缩包 → 解压到 `~/.vmake/toolchains/<name>-<version>/`
   - `method: "http"` → 从 `url` 下载压缩包 → 解压到 `~/.vmake/toolchains/<name>-<version>/`
5. 下载完成后自动注册工具链，后续可直接使用

压缩包应通过 Git LFS 存储，避免克隆扩展仓库时下载大文件。

## 实战示例

### 示例 1：简单的 CLI 扩展

创建一个提供 `vmake flash` 子命令的插件：

```go
package main

import (
    "fmt"

    "gitee.com/spock2300/vmake/pkg/plugin"
    "github.com/spf13/cobra"
)

func Main(ctx *plugin.Context) {
    flashCmd := &cobra.Command{
        Use:   "flash <binary> [port]",
        Short: "Flash binary to embedded device",
        Args:  cobra.RangeArgs(1, 2),
        Run: func(cmd *cobra.Command, args []string) {
            binary := args[0]
            port := "/dev/ttyUSB0"
            if len(args) > 1 {
                port = args[1]
            }
            fmt.Printf("Flashing %s to %s\n", binary, port)
            // 实际的烧录逻辑
        },
    }

    ctx.AddSubCommand(flashCmd)
}
```

### 示例 2：交叉编译工具链管理插件

提供 `vmake xcompile list` 和 `vmake xcompile use` 命令，同时管理工具链自动下载：

```go
package main

import (
    "fmt"
    "path/filepath"

    "gitee.com/spock2300/vmake/pkg/plugin"
    "gitee.com/spock2300/vmake/pkg/toolchain"
    "github.com/spf13/cobra"
)

func Main(ctx *plugin.Context) {
    // 注册工具链
    registerToolchains(ctx)

    // 设置自动下载回调
    ctx.SetOnMissing("arm-none-eabi", func(name string) (*toolchain.Toolchain, error) {
        return downloadToolchain(ctx, name)
    })

    // 添加全局编译标志
    ctx.AddGlobalFlags(
        []string{"-ffunction-sections", "-fdata-sections"},
        []string{"-ffunction-sections", "-fdata-sections"},
    )

    // list 子命令
    ctx.AddSubCommand(&cobra.Command{
        Use:   "list",
        Short: "List available cross-compilation toolchains",
        Run: func(cmd *cobra.Command, args []string) {
            for name, tc := range ctx.GetToolchains() {
                fmt.Printf("  %-25s %s\n", name, tc.DisplayName)
            }
        },
    })
}

func registerToolchains(ctx *plugin.Context) {
    toolchainsDir := filepath.Join(ctx.VMakeDir, "toolchains")

    ctx.RegisterToolchain("arm-none-eabi", &toolchain.Toolchain{
        Name:        "arm-none-eabi",
        DisplayName: "ARM GCC 12.2.1",
        Host:        "x86_64-linux-gnu",
        Prefix:      "arm-none-eabi",
        Tools: toolchain.Tools{
            CC:      "arm-none-eabi-gcc",
            CXX:     "arm-none-eabi-g++",
            AR:      "arm-none-eabi-ar",
            OBJCOPY: "arm-none-eabi-objcopy",
            SIZE:    "arm-none-eabi-size",
            OBJDUMP: "arm-none-eabi-objdump",
            NM:      "arm-none-eabi-nm",
        },
        DefaultFlags: toolchain.DefaultFlags{
            CFlags:   []string{"-Os", "-mcpu=cortex-m4", "-mthumb"},
            CxxFlags: []string{"-Os", "-mcpu=cortex-m4", "-mthumb"},
            LdFlags:  []string{"-specs=nosys.specs"},
        },
        InstallPath: filepath.Join(toolchainsDir, "arm-none-eabi-12.2.1"),
    })
}

func downloadToolchain(ctx *plugin.Context, name string) (*toolchain.Toolchain, error) {
    pluginDir := ctx.PluginDir
    archivePath := filepath.Join(pluginDir, "assets", "toolchains", name+".tar.gz")
    toolchainsDir := filepath.Join(ctx.VMakeDir, "toolchains")

    // 通过 Git LFS 拉取
    if err := ctx.RunGitLFS(pluginDir, "pull", "--include", "assets/toolchains/"+name+".tar.gz"); err != nil {
        return nil, fmt.Errorf("download failed: %w", err)
    }

    // 解压
    if err := ctx.ExtractToDir(archivePath, toolchainsDir, ""); err != nil {
        return nil, fmt.Errorf("extract failed: %w", err)
    }

    fmt.Printf("Toolchain %s installed\n", name)
    // 返回已注册的工具链（此处省略具体构造）
    return nil, nil
}
```

### 示例 3：仅提供工具链资源（无插件）

如果扩展仓库不包含插件代码，仅提供工具链资源，只需在仓库根目录下创建以工具链命名的子目录，每个子目录包含一个 `toolchain.json`。

目录结构：

```
my-toolchains/
├── arm-gcc-12.2/
│   └── toolchain.json
├── riscv-gcc-13.1/
│   └── toolchain.json
└── assets/
    └── toolchains/
        ├── arm-gcc-12.2.0.tar.gz      (Git LFS)
        └── riscv-gcc-13.1.0.tar.gz    (Git LFS)
```

通过 `vmake ext add` 添加该仓库后，使用 `tc` 插件即可自动发现并注册工具链。

如需手动注册，可在自建插件中调用 `ctx.RegisterToolchainsFromRepo()`。
