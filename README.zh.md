# VMake

VMake 是一个现代化的 C/C++ 项目构建工具，采用 Go 语言开发。它提供了一个简洁而强大的 API，用于配置和构建多模块 C/C++ 项目。

## 功能特性

- **简洁的 API 设计**：通过方法链 (Fluent API) 实现声明式构建配置
- **灵活的选项系统**：支持布尔、字符串、整数和枚举类型的配置选项
- **条件构建支持**：通过 `If`、`When` 等方法实现条件编译
- **多模块支持**：原生支持多模块项目的构建管理
- **第三方包管理**：支持 Registry（包装 CMake/Autotools）和 Native（vmake 原生包）两种仓库类型，通过 OnRequire 声明依赖，自动下载、版本匹配和构建
- **扩展插件系统**：通过 Go 插件扩展 CLI 命令和工具链，支持自定义子命令、交叉编译工具链自动下载与管理
- **增量编译**：基于依赖分析的智能增量编译，大幅提升构建效率
- **TUI 配置界面**：提供交互式终端用户界面，方便配置项目选项
- **工具链管理**：支持多种编译工具链的灵活切换，支持交叉编译
- **语义版本约束**：内置语义版本解析和约束匹配
- **符号管理**：通过 `SetDefaultVisibilityHidden` + `SetVersionScript` + `SetExcludeLibs` + `SetSymbolBinding` + `vmake check-symbols` 五层防御，控制库的导出符号，避免复杂依赖图中的符号冲突和泄漏

## 快速开始

### 安装

```bash
go install github.com/spock2300/vmake/cmd/vmake@latest
```

安装后 vmake 位于 `~/go/bin/vmake`。

### 调试模式

build.go 由 yaegi 解释器直接执行，无需编译为插件：

```bash
cd /path/to/vmake
go build -o vmake ./cmd/vmake
./vmake build
```

调试模式下，插件会使用本地 vmake 源码编译，避免版本不匹配问题。

### 基本用法

创建 `build.go` 文件：

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("debug").
            SetType(api.OptionBool).
            SetDefault(true).
            SetDescription("Enable debug mode")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").
            SetKind(api.TargetBinary).
            AddFiles("src/main.c").
            AddDefines(ctx.If("debug", "DEBUG")...)
    })
}
```

运行构建：

```bash
vmake build
```

## 项目结构

```
vmake/
├── cmd/vmake/           # CLI 命令入口
├── pkg/
│   ├── api/             # 核心构建 API（构建脚本可导入）
│   ├── plugin/          # 扩展插件系统（插件可导入）
│   ├── build/           # 编译、链接、缓存管理
│   ├── buildscript/     # 构建脚本扫描、解释、加载
│   ├── config/          # 配置存储
│   ├── resolver/        # 依赖解析
│   ├── repo/            # 包仓库管理
│   ├── toolchain/       # 工具链管理
│   ├── log/             # 日志输出
│   ├── tui/             # 终端用户界面
│   └── version/         # 版本信息
├── internal/
│   ├── exec/            # 命令执行
│   ├── flock/           # 文件锁（跨项目同步）
│   ├── fs/              # 文件系统工具
│   ├── gitstore/        # Git 仓库管理（共享基础设施）
│   ├── glob/            # 文件匹配
│   ├── gosrc/           # Go 源码合并（buildscript + plugin 共用）
│   ├── jsonio/          # JSON 序列化
│   ├── toposort/        # 拓扑排序
│   ├── yaegibase/       # yaegi 解释器初始化 helper
│   └── yaegisym/        # cobra/pflag 的 yaegi 符号表（go generate）
└── docs/                # 设计文档
```

## 包仓库

VMake 支持两种包仓库类型：

**Registry 仓库**：包装第三方 C/C++ 库（如 zlib、curl）。`build.go` 作为包装器调用 CMake/Autotools 构建源码，版本通过 `AddVersion()` 手动映射。

**Native 仓库**：VMake 原生包，用于跨项目共享。每个包是一个独立的 Git 仓库，`build.go` 位于仓库根目录，版本通过 git tag 自动识别。

| | Registry 仓库 | Native 仓库 |
|--|--|--|
| **用途** | 包装第三方 C/C++ 库 | VMake 原生包，跨项目共享 |
| **build.go** | 包装器（调用 CMake 等） | 真正的构建描述 |
| **版本来源** | `AddVersion()` 手动映射 | git tag（自动识别 semver） |
| **添加命令** | `vmake repo add name url` | `vmake repo add --native name "https://..../{name}.git"` |

### 使用流程

1. 添加仓库：

```bash
vmake repo add official https://github.com/user/vmake-packages    # Registry
vmake repo add --native myorg https://git.example.com/{name}.git   # Native
```

2. 在 `build.go` 中声明依赖：

```go
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("official/zlib >=1.2")
})
```

3. 在 Target 中使用：

```go
ctx.Target("app").
    SetKind(api.TargetBinary).
    AddFiles("src/*.c").
    AddDeps("official/zlib")
```

## API 概览

### Option 类型

| 类型 | 说明 |
|------|------|
| `OptionBool` | 布尔类型 |
| `OptionString` | 字符串类型 |
| `OptionInt` | 整数类型 |
| `OptionChoice` | 枚举类型 |

### Target 类型

| 类型 | 说明 |
|------|------|
| `TargetBinary` | 可执行文件 |
| `TargetStatic` | 静态库 |
| `TargetShared` | 动态库 |
| `TargetObject` | 目标文件 |
| `TargetVoid` | 第三方包构建（配合 `SetBuildFunc`） |

### 核心方法

```go
// 配置选项
ctx.Option(name string) *Option
ctx.Bool(name string) bool
ctx.String(name string) string
ctx.Int(name string) int

// 条件判断
ctx.If(option string, then ...string) []string
ctx.IfNot(option string, then ...string) []string
ctx.When(option string, value any) bool
ctx.Select(option string, mapping map[string]string) string

// 目标配置
ctx.Target(name string) *Target
```

## 扩展插件

扩展插件通过 yaegi 解释器动态加载 Go 源码，无需编译，不产生 `.so` 文件。每个扩展仓库是一个 Git 仓库，仓库根目录下的每个子目录（含 `plugin.json`）是一个独立的插件。

### 扩展能力

- **CLI 命令扩展**：通过 `AddSubCommand` 添加自定义子命令
- **工具链管理**：注册自定义工具链，支持通过 `toolchain.json` + `tc` 插件实现首次使用自动下载（Git LFS 或 HTTP）
- **全局编译/链接标志**：通过 `AddGlobalCFlags`、`AddGlobalCxxFlags` 和 `AddGlobalLdFlags` 向所有构建注入 C/CXX/链接选项，并通过 `CMakeGlobalFlagsArgs()` 或 `MergedCFlags()` 传递给 CMake 外部构建

### 使用流程

1. 添加扩展仓库：

```bash
vmake ext add <name> <git-url>
```

2. 插件在下次运行时自动发现并解释执行，重启 vmake 即可使用

详见 [扩展插件指南](docs/EXTENSION_PLUGIN.md) 获取完整的插件编写教程、所有接口参考和实战示例。

## 命令行用法

### 构建命令

```bash
vmake build [--toolchain <name>] [--mode <mode>] [-i|--install] [-p|--prefix <dir>] [--install-type <type>] [--manifest <file>] [--tests]
vmake test
vmake clean [--all]
vmake distclean
vmake rebuild
```

### 配置命令

```bash
vmake config    # 交互式 TUI 配置
```

### 工具链管理

```bash
vmake toolchain list
vmake toolchain show [name]
```

### 包仓库管理

```bash
vmake repo add <name> <url>                # Registry 仓库
vmake repo add --native <name> <url>       # Native 仓库（URL 模板含 {name}）
vmake repo remove <name>
vmake repo list
vmake repo update <name>
```

### 包管理

```bash
vmake pkg list
vmake pkg search <keyword>
vmake pkg clean <repo/name> [-a]
vmake pkg update <repo/name>
```

### 扩展管理

```bash
vmake ext add <name> <url>
vmake ext remove <name>
vmake ext list
vmake ext update [name]
```

### 其他命令

```bash
vmake git tag [version] [--minor|--major]             # 版本标签
vmake doctor                                          # 诊断 build.go 模式
vmake manifest show <path>                            # 显示清单内容
vmake manifest checkout <path> [name]                 # 按清单检出版本
vmake completion <shell>                              # 生成 shell 自动补全 (bash|zsh|fish|powershell)
vmake completion install                              # 自动安装 shell 补全
vmake update [version]                                # 自我更新
vmake version                                         # 版本信息
vmake skill install                                   # 安装 AI 技能
vmake skill uninstall                                 # 卸载 AI 技能
vmake skill path                                      # 显示安装路径
```

全局选项：`-v` (verbose), `-V` (very verbose), `-q` (quiet)

## 开发文档

详细的设计文档请参考 [docs](docs/) 目录：

- [构建脚本 API](docs/BUILD_SCRIPT_API.md) - 构建脚本和第三方包 API
- [扩展插件指南](docs/EXTENSION_PLUGIN.md) - CLI 扩展和工具链仓库编写
- [架构设计](docs/ARCHITECTURE.md) - 系统架构和执行流程
- [目录结构](docs/VMAKE_HOME.md) - ~/.vmake 目录结构
- [AI 安装指南](docs/AI_INSTALL_GUIDE.md) - AI 助手技能安装指南
- [固件构建设计](docs/FIRMWARE_BUILD_DESIGN.md) - 固件构建系统设计

## 测试用例

项目包含以下测试场景：

| 目录 | 描述 |
|------|------|
| `test_data/01_simple_c` | 简单 C 项目 |
| `test_data/02_with_config` | 带配置选项的项目 |
| `test_data/03_multi_target` | 多目标项目 |
| `test_data/04_multi_module` | 多模块项目 |
| `test_data/05_conditional` | 条件编译项目 |
| `test_data/06_complete_api` | 完整 API 测试 |
| `test_data/07_subbuild_codegen` | 子构建 / 代码生成 |
| `test_data/08_with_package` | 使用第三方包 |
| `test_data/09_with_curl` | 使用 libcurl |
| `test_data/10_local_repo` | 本地包仓库 |
| `test_data/11_with_tinyexpr` | 使用 tinyexpr 库 |
| `test_data/12_rtos_simulate` | RTOS 模拟项目 |
| `test_data/13_with_prefix_repo` | Native 仓库依赖 |
| `test_data/14_bin_header` | 二进制头文件嵌入 |
| `test_data/15_subgraph_siblings` | 子图兄弟目标构建（宿主机代码生成工具 + 库） |
| `test_data/16_subgraph_cross_tc` | 子图交叉编译工具链 |
| `test_data/18_config_header` | 配置头文件自动生成（GenerateConfigHeader） |
| `test_data/19_config_defines` | 配置宏定义自动生成（GenerateConfigDefines） |
| `test_data/20_config_propagate` | 配置跨包传播（ImportConfig） |
| `test_data/21_root_package` | 根包选择（SetRoot） |
| `test_data/22_version_script` | 版本脚本链接器集成 |
| `test_data/23_link_strategy` | 链接策略测试 |
| `test_data/24_symbol_prefix` | 符号前缀（objcopy --prefix-symbols） |
| `test_linux/17_firmware` | 完整固件构建（Linux, U-Boot, BusyBox, App, RootFS, Firmware） |

## 许可证

本项目采用 MIT 许可证，详情请参见 [LICENSE](LICENSE) 文件。

## 联系方式

- 项目地址：https://github.com/spock2300/vmake
