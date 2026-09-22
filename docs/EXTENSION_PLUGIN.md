# VMake 扩展插件指南

扩展插件通过 [yaegi](https://github.com/traefik/yaegi) Go 解释器动态加载，扩展 vmake 自身的能力。插件存储在 `~/.vmake/extensions/<repo>/<plugin>/`。无需编译，vmake 启动时即时解释执行插件源码。

扩展只做三件事：

1. 提供新的 CLI 命令，执行外部程序
2. 注册与项目无关的全局编译/链接选项
3. 按宿主 OS/架构声明、下载并注册编译器

“编译什么、怎么编译”由项目的 `build.go` 决定。扩展不参与编译过程，也不向构建脚本提供 API。

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

    "github.com/spock2300/vmake/pkg/plugin"
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

vmake 在启动时自动发现并用 yaegi 解释执行插件源码，无需编译，即时可用。

`vmake ext add` 和 `vmake ext update` 只获取普通 Git 文件及 LFS 指针，不自动下载 LFS 大文件。构建首次使用某个未安装的工具链时，vmake 才下载该工具链对应当前宿主 OS/架构的压缩包。更新扩展不会预取新版本，也不会删除已有压缩包、LFS 缓存或工具链安装目录。这项策略仅用于扩展仓库，不改变项目源码依赖的 Git 下载行为。

## 目录结构

```
~/.vmake/extensions/
└── <repo-name>/
    ├── <plugin-a>/              # 插件 A
    │   ├── plugin.json
    │   └── src/main.go
    ├── <plugin-b>/              # 插件 B
    │   ├── plugin.json
    │   └── src/main.go
    ├── <toolchain-name>/        # 工具链声明
    │   └── toolchain.json
    └── assets/toolchains/       # 工具链压缩包（Git LFS）
        └── *.tar.gz
```

每个扩展仓库是一个 Git 仓库。仓库根目录下的每个子目录可以是一个插件（含 `plugin.json`）或一个工具链声明（含 `toolchain.json`）。一个仓库可以包含任意数量的插件和工具链，vmake 启动时自动发现并注册工具链、加载所有插件；只含 `toolchain.json` 的仓库不需要任何插件代码。

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

对应的 CLI 命令为 `vmake flash ...` 和 `vmake monitor ...`。`arm-gcc-12.2/toolchain.json` 由 vmake 直接扫描注册。

## plugin.json

插件元信息文件，必须放在插件目录根下。

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 插件名，同时也是 CLI 命令名（`vmake <name>`） |
| `version` | string | 否 | 语义版本号，显示在 `vmake ext list` |
| `description` | string | 否 | 一句话描述，显示在 `vmake ext list` |
| `entry` | string | 是 | 入口 Go 源文件的相对路径（如 `src/main.go`） |
| `enabled` | bool | 否 | 是否启用，默认 `false`。设为 `true` 时启用该插件 |

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
RegisterToolchain func(name string, tc *toolchain.Toolchain) error
```

注册一个自定义工具链。注册后用户可以通过 `--toolchain <name>` 或全局 `toolchain` 选项选择该工具链。名称必须与 `tc.Name` 一致，不能是 `host`，且不能与已注册的工具链重名，否则返回错误。

工具链只描述“用哪些程序编译”，不描述“为哪个 CPU 编译”。目标 CPU/ABI 选项由项目的 `build.go` 提供。

```go
err := ctx.RegisterToolchain("riscv32", &toolchain.Toolchain{
    Name:        "riscv32",
    DisplayName: "RISC-V 32-bit",
    Prefix:      "riscv32-unknown-elf-",
    Tools: toolchain.Tools{
        CC:  "riscv32-unknown-elf-gcc",
        CXX: "riscv32-unknown-elf-g++",
        AR:  "riscv32-unknown-elf-ar",
        LD:  "riscv32-unknown-elf-gcc",
    },
    InstallPath: filepath.Join(ctx.VMakeDir, "toolchains", "riscv32"),
})
```

通常不需要手动调用：仓库子目录中的 `toolchain.json` 由 vmake 自身扫描注册，见[工具链资源](#工具链资源)。`RegisterToolchain` 用于无法用 `toolchain.json` 描述的动态场景。

### GetToolchains

```go
GetToolchains func() map[string]*toolchain.Toolchain
```

获取所有已注册的工具链（内置 `host` + 声明或注册的工具链）。

```go
for name, tc := range ctx.GetToolchains() {
    fmt.Printf("%s: %s\n", name, tc.DisplayName)
}
```

### SetOnMissing

```go
SetOnMissing func(toolchainName string, onMissing func(name string) (*toolchain.Toolchain, error))
```

设置工具链缺失回调。第一个参数是工具链名称，用于区分不同工具链的缺失回调，支持每个工具链独立的下载逻辑。含 `installations` 的 `toolchain.json` 已由 vmake 自动注册该回调，此方法用于自定义安装流程。

```go
ctx.SetOnMissing("arm-gcc", func(name string) (*toolchain.Toolchain, error) {
    // 下载 arm-gcc 工具链...
})
```

### AddGlobalCFlags / AddGlobalCxxFlags

```go
AddGlobalCFlags   func(flags ...string)
AddGlobalCxxFlags func(flags ...string)
```

为所有构建目标注入全局 C 或 C++ 编译选项。影响所有使用 vmake 构建的项目，因此只应放置与项目无关的选项；目标 CPU、优化级别等属于项目，应写在 `build.go` 里。

```go
ctx.AddGlobalCFlags("-ffunction-sections", "-fdata-sections")
ctx.AddGlobalCxxFlags("-ffunction-sections", "-fdata-sections")
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

从 URL 下载文件到本地路径。使用 `curl -fL -o` 实现，自动创建目标目录的父目录。

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

在指定目录执行 `git lfs` 命令。声明式工具链的资源由 vmake 按需下载；插件自有的其他 LFS 资源需要在实际使用时调用此接口显式拉取。插件源码、`plugin.json` 和 `toolchain.json` 应作为普通 Git 文件保存。

```go
err := ctx.RunGitLFS(ctx.RepoDir, "pull", "--include=assets/toolchains/aarch64-gcc.tar.gz", "--exclude=")
```

`--include` 限定需要的文件，`--exclude=` 清除本次调用继承的 LFS 排除规则。该接口按插件提供的参数执行，下载范围和调用时机由插件负责。

## 工具链类型

工具链只回答“用哪些程序编译”。目标系统、目标三元组和项目编译选项都不属于工具链，由项目的 `build.go` 提供，因此同一个编译器可以服务不同 CPU 的项目。

### toolchain.Toolchain

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 工具链标识符（如 `"aarch64-linux-gnu"`） |
| `DisplayName` | `string` | 可读名称（如 `"ARM GCC 12.2.0"`），`vmake toolchain list` 显示 |
| `Prefix` | `string` | 包含末尾 `-` 的交叉编译前缀（如 `"aarch64-linux-gnu-"`），设为 `""` 表示无前缀；拼接工具名时不再添加 `-` |
| `Tools` | `Tools` | 各工具的可执行文件名 |
| `InstallPath` | `string` | 工具链安装目录的绝对路径，为空表示尚未安装 |

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
| `MAKE` | `string` | make 程序，为空时按需解析为 `make` | `"make"` |

`CC`、`CXX`、`AR` 和 `LD` 是必填项，其余可选。

### Toolchain.Env()

`Toolchain` 提供一个 `Env()` 方法，返回传给 `p.Run`/`p.Make` 等外部命令的环境变量映射：

| 变量 | 来源 |
|------|------|
| `CC` | `Tools.CC` |
| `CXX` | `Tools.CXX` |
| `LD` | `Tools.LD` |
| `AR` | `Tools.AR` |
| `MAKE` | `MakeTool()` |
| `CROSS_COMPILE` | `Prefix`（仅当非空时） |
| `OBJCOPY` | `Tools.OBJCOPY`（仅当非空时） |
| `SIZE` | `Tools.SIZE`（仅当非空时） |
| `OBJDUMP` | `Tools.OBJDUMP`（仅当非空时） |
| `NM` | `Tools.NM`（仅当非空时） |

`CFLAGS`/`CXXFLAGS`/`LDFLAGS` 来自包自身的编译选项，由 `pkg/api` 在调用外部构建系统时拼接，不再由工具链提供。

### Toolchain.CommandEnv()

`CommandEnv()` 返回单条命令使用的环境变量映射：依次把 `InstallPath/bin`（如有）、绝对路径 `CC` 和 `CXX` 所在目录前置到继承的 `PATH`，重复的工具目录只添加一次。这样直接配置编译器绝对路径的工具链也能让 CMake、Make 及其子进程找到同目录的辅助程序。vmake 不修改进程级 `PATH`，各工具链互不干扰。

## 工具链资源

扩展可随仓库提供预编译工具链，通过 `toolchain.json` 声明。vmake 启动时扫描每个扩展仓库的子目录，自动注册其中的 `toolchain.json`，不需要插件参与。

### toolchain.json 格式

```json
{
  "name": "arm-gcc",
  "version": "12.2.0",
  "display_name": "ARM GCC 12.2.0",
  "prefix": "arm-linux-gnueabihf-",
  "tools": {
    "cc": "arm-linux-gnueabihf-gcc",
    "cxx": "arm-linux-gnueabihf-g++",
    "ar": "arm-linux-gnueabihf-ar",
    "ld": "arm-linux-gnueabihf-gcc",
    "strip": "arm-linux-gnueabihf-strip"
  },
  "installations": {
    "linux/amd64": {
      "method": "lfs",
      "file": "arm-gcc-12.2.0.tar.gz",
      "format": "tar.gz",
      "root_dir": "arm-gcc-12.2.0"
    }
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 工具链标识符，用于 `--toolchain <name>` |
| `version` | string | 安装时必填 | 安装目录使用 `<os>/<arch>/<name>/<version>` |
| `display_name` | string | 否 | 可读名称，默认同 `name` |
| `prefix` | string | 否 | 交叉编译前缀，非空时必须包含结尾的连字符 |
| `tools` | object | 是 | 各工具的可执行文件名（同 `toolchain.Tools`，`cc`、`cxx`、`ar` 和 `ld` 必填） |
| `installations` | object | 否 | 以宿主 `OS/architecture` 为键的安装配置；不配置则使用明确配置的工具路径或 PATH |

未列出的字段一律拒绝。`target_os`、`target_triple`、`default_flags` 属于项目配置，写在 `toolchain.json` 里会让该定义报错；旧的 `host`、`install` 字段同样不再接受。

**每个 installations 条目的字段**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `method` | string | 是 | 下载方法：`"lfs"`（Git LFS）或 `"http"` |
| `file` | string | 是 | 压缩包文件名：`lfs` 方法时位于扩展仓库 `assets/toolchains/` 下，不能包含逗号、通配符 `*?[]` 或首尾空白，以保证只匹配一个文件；`http` 方法的文件名规则不变 |
| `url` | string | 否 | HTTP 下载URL（method 为 http 时必填） |
| `format` | string | 否 | 压缩格式（`tar.gz`/`tar.xz`/`tar.bz2`/`zip`），为空时自动检测 |
| `sha256` | string | 否 | SHA256 校验和，可选 |
| `root_dir` | string | 是 | 归档中的工具链根目录；`.` 表示归档根目录 |

### 自动下载机制

工具链的发现与安装由 vmake 自身完成，插件不参与：

1. vmake 启动时扫描每个扩展仓库的子目录，逐个加载 `toolchain.json`，独立记录每份定义的成功或错误
2. 对每个包含当前宿主 `installations` 条目的工具链，注册按需安装回调
3. 构建通过 `--toolchain <name>`、全局 `toolchain` 选项或实际构建的子图选中未安装的工具链时：
   - `method: "lfs"` → 先检查选中的压缩包；完整文件直接复用，指针或缺失文件才执行 `git -c lfs.fetchrecentalways=false lfs pull --include=assets/toolchains/<file> --exclude=`。只拉取当前检出版本的单个文件，已有 LFS 对象可离线复用；拉取后仍为指针或缺失则报错，不扩大下载范围
   - `method: "http"` → 从 `url` 下载压缩包到临时目录
4. 校验 SHA256、按 `root_dir` 解压到暂存目录，再整体重命名发布到 `~/.vmake/toolchains/<os>/<arch>/<name>/<version>/`，作为工具链的 `InstallPath`
5. 安装过程持有文件锁，多个项目并发构建时只会安装一次

工具链的程序通过 `InstallPath/bin` 逐条命令解析，vmake 不会修改进程级 `PATH`。

列表、查询、诊断、`clean` 和 `lock update` 不会自动安装工具链。操作确实依赖编译器信息而工具不可用时，会提示先执行 `vmake build --toolchain <name>`。`clean --all` 可在工具链缺失时清理构建目录；此时无法执行依赖工具链的 `OnClean` 回调会明确提示。第三方插件显式调用下载接口的行为不受此限制。

错误仅阻止对应工具链；`vmake toolchain list` 显示定义名称、路径及原因，损坏 JSON 无法识别名称时按路径显示。选择错误定义时不会回退到宿主工具链。`vmake ext update/remove` 跳过插件执行，可用于修复或移除扩展。

### 项目一侧的目标平台

工具链不携带目标信息，项目在 `build.go` 中声明：

```go
func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("none")
        ctx.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString).SetDefault("arm-none-eabi")
        ctx.AddGlobalCFlags("-mcpu=cortex-m4", "-mthumb")
        ctx.AddGlobalCxxFlags("-mcpu=cortex-m4", "-mthumb")
        ctx.AddGlobalLdFlags("-mcpu=cortex-m4", "-mthumb", "--specs=nosys.specs")
    })
}
```

`target_os` 决定产物命名、链接策略和 CMake 的 `CMAKE_SYSTEM_NAME`（裸机为 `none`），`target_triple` 提供 `--host=` 与 `CMAKE_*_COMPILER_TARGET`。两者都是普通全局选项，可由 `.vmake/config.json` 的 `global.options` 覆盖。

## 实战示例

### 示例 1：简单的 CLI 扩展

创建一个提供 `vmake flash` 子命令的插件：

```go
package main

import (
    "fmt"

    "github.com/spock2300/vmake/pkg/plugin"
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

提供 `vmake xcompile list` 命令，并为动态发现的工具链注册安装回调：

```go
package main

import (
    "fmt"
    "path/filepath"

    "github.com/spock2300/vmake/pkg/plugin"
    "github.com/spock2300/vmake/pkg/toolchain"
    "github.com/spf13/cobra"
)

func Main(ctx *plugin.Context) {
    registerToolchains(ctx)

    ctx.SetOnMissing("arm-none-eabi", func(name string) (*toolchain.Toolchain, error) {
        return downloadToolchain(ctx, name)
    })

    // 与项目无关的全局选项
    ctx.AddGlobalCFlags("-ffunction-sections", "-fdata-sections")
    ctx.AddGlobalCxxFlags("-ffunction-sections", "-fdata-sections")

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

    if err := ctx.RegisterToolchain("arm-none-eabi", &toolchain.Toolchain{
        Name:        "arm-none-eabi",
        DisplayName: "ARM GCC 12.2.1",
        Prefix:      "arm-none-eabi-",
        Tools: toolchain.Tools{
            CC:      "arm-none-eabi-gcc",
            CXX:     "arm-none-eabi-g++",
            AR:      "arm-none-eabi-ar",
            LD:      "arm-none-eabi-gcc",
            OBJCOPY: "arm-none-eabi-objcopy",
            SIZE:    "arm-none-eabi-size",
            OBJDUMP: "arm-none-eabi-objdump",
            NM:      "arm-none-eabi-nm",
        },
        InstallPath: filepath.Join(toolchainsDir, "arm-none-eabi-12.2.1"),
    }); err != nil {
        fmt.Printf("register arm-none-eabi: %v\n", err)
    }
}

func downloadToolchain(ctx *plugin.Context, name string) (*toolchain.Toolchain, error) {
    archive := filepath.Join(ctx.RepoDir, "assets", "toolchains", name+".tar.gz")
    toolchainsDir := filepath.Join(ctx.VMakeDir, "toolchains")

    if err := ctx.RunGitLFS(ctx.RepoDir, "pull", "--include", "assets/toolchains/"+name+".tar.gz", "--exclude="); err != nil {
        return nil, fmt.Errorf("download failed: %w", err)
    }
    if err := ctx.ExtractToDir(archive, toolchainsDir, ""); err != nil {
        return nil, fmt.Errorf("extract failed: %w", err)
    }
    return ctx.GetToolchains()[name], nil
}
```

注意：`-mcpu`、`-mthumb`、`-Os` 这类描述“为哪个 CPU 编译”的选项不属于插件，应写在项目的 `build.go` 里。

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

通过 `vmake ext add` 添加该仓库后，vmake 自动发现并注册其中的工具链，`vmake toolchain list` 立即可见，无需任何插件代码。
