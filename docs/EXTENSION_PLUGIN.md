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
    ├── <plugin-a>/
    │   ├── plugin.json       # 插件 A 元信息
    │   ├── src/
    │   │   └── main.go       # 入口文件
    │   └── plugin.so         # 编译产物（自动生成，可删除重建）
    ├── <plugin-b>/
    │   ├── plugin.json       # 插件 B 元信息
    │   └── src/
    │       └── main.go
    └── assets/               # 可选：仓库共享资源
        └── toolchains/
            ├── manifest.json # 工具链声明
            └── *.tar.gz      # 工具链压缩包（Git LFS）
```

每个扩展仓库是一个 Git 仓库。仓库根目录下的每个子目录（含 `plugin.json`）是一个独立的插件。一个仓库可以包含任意数量的插件，vmake 启动时自动发现并加载所有插件。

示例：一个仓库 `embedded-tools` 包含烧录和监控两个插件：

```
~/.vmake/extensions/embedded-tools/
├── flash/
│   ├── plugin.json           # name: "flash"
│   └── src/main.go
├── monitor/
│   ├── plugin.json           # name: "monitor"
│   └── src/main.go
└── assets/
    └── toolchains/manifest.json
```

对应的 CLI 命令为 `vmake flash ...` 和 `vmake monitor ...`。

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
SetOnMissing func(onMissing func(name string) (*toolchain.Toolchain, error))
```

设置工具链缺失回调。当用户请求一个未注册的工具链名称时，vmake 调用此回调。典型用途是实现工具链自动下载。

```go
ctx.SetOnMissing(func(name string) (*toolchain.Toolchain, error) {
    if name == "aarch64-linux-gnu" {
        fmt.Println("Auto-downloading aarch64 toolchain...")
        // 下载、解压、注册...
        return &toolchain.Toolchain{...}, nil
    }
    return nil, fmt.Errorf("unknown toolchain: %s", name)
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

### ExtractArchive

```go
ExtractArchive func(archive, dest string) error
```

解压 `.tar.gz` 归档到目标目录。自动创建目标目录。

```go
err := ctx.ExtractArchive(
    filepath.Join(ctx.VMakeDir, "cache", "esp-idf.tar.gz"),
    filepath.Join(ctx.VMakeDir, "toolchains", "esp-idf"),
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

## 工具链类型

### toolchain.Toolchain

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 工具链标识符（如 `"aarch64-linux-gnu"`） |
| `DisplayName` | `string` | 可读名称（如 `"aarch64-linux-gnu 13.2.0"`），`vmake toolchain list` 显示 |
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

扩展可随仓库提供预编译工具链，通过 `assets/toolchains/manifest.json` 声明。

### manifest.json 格式

```json
{
  "toolchains": [
    {
      "name": "aarch64-linux-gnu",
      "version": "13.2.0",
      "host": "x86_64-linux-gnu",
      "prefix": "aarch64-linux-gnu",
      "file": "aarch64-linux-gnu-13.2.0.tar.gz",
      "tools": {
        "cc": "aarch64-linux-gnu-gcc",
        "cxx": "aarch64-linux-gnu-g++",
        "ar": "aarch64-linux-gnu-ar",
        "ld": "aarch64-linux-gnu-ld",
        "strip": "aarch64-linux-gnu-strip",
        "ranlib": "aarch64-linux-gnu-ranlib",
        "objcopy": "aarch64-linux-gnu-objcopy",
        "size": "aarch64-linux-gnu-size",
        "objdump": "aarch64-linux-gnu-objdump",
        "nm": "aarch64-linux-gnu-nm"
      },
      "default_flags": {
        "cflags": ["-O2"],
        "cxxflags": ["-O2"],
        "ldflags": []
      }
    }
  ]
}
```

| 字段 | 说明 |
|------|------|
| `name` | 工具链标识符，用于 `--toolchain <name>` |
| `version` | 版本号 |
| `host` | 运行工具链的宿主平台 |
| `prefix` | 交叉编译前缀，工具链 bin 目录下的可执行文件此前缀命名 |
| `file` | `assets/toolchains/` 下的压缩包文件名 |
| `tools` | 各工具的可执行文件名（同 `toolchain.Tools`） |
| `default_flags` | 默认编译/链接选项（同 `toolchain.DefaultFlags`） |

### 自动下载机制

1. vmake 启动时扫描所有扩展仓库的 `assets/toolchains/manifest.json`
2. 将声明的工具链注册到管理器
3. 当用户首次请求某个工具链时，自动触发下载：
   - 通过 Git LFS 拉取压缩包
   - 解压到 `~/.vmake/toolchains/<name>-<version>/`
   - 注册工具链供后续使用

压缩包应通过 Git LFS 存储，以避免克隆扩展仓库时下载大文件。

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
    ctx.SetOnMissing(func(name string) (*toolchain.Toolchain, error) {
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
    if err := ctx.ExtractArchive(archivePath, toolchainsDir); err != nil {
        return nil, fmt.Errorf("extract failed: %w", err)
    }

    fmt.Printf("Toolchain %s installed\n", name)
    // 返回已注册的工具链（此处省略具体构造）
    return nil, nil
}
```

### 示例 3：通过 manifest.json 提供工具链资源

如果扩展仓库不包含插件代码，仅提供工具链资源，只需放置 `assets/toolchains/manifest.json` 和对应的 `.tar.gz`（通过 Git LFS），vmake 会自动识别并注册。

目录结构：

```
my-toolchains/
└── assets/
    └── toolchains/
        ├── manifest.json
        └── aarch64-linux-gnu-13.2.0.tar.gz   (Git LFS)
```

manifest.json 声明工具链名称、版本和工具路径后，vmake 在启动时自动注册。用户首次使用时自动下载解压。
