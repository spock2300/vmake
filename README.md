# VMake

VMake 是一个现代化的 C/C++ 项目构建工具，采用 Go 语言开发。它提供了一个简洁而强大的 API，用于配置和构建多模块 C/C++ 项目。

## 功能特性

- **简洁的 API 设计**：通过方法链 (Fluent API) 实现声明式构建配置
- **灵活的选项系统**：支持布尔、字符串、整数和枚举类型的配置选项
- **条件构建支持**：通过 `If`、`When` 等方法实现条件编译
- **多模块支持**：原生支持多模块项目的构建管理
- **第三方包管理**：通过 Git 仓库管理第三方依赖，自动下载和构建
- **扩展插件系统**：支持 CLI 命令扩展和交叉编译工具链管理
- **增量编译**：基于依赖分析的智能增量编译，大幅提升构建效率
- **TUI 配置界面**：提供交互式终端用户界面，方便配置项目选项
- **工具链管理**：支持多种编译工具链的灵活切换，支持交叉编译
- **语义版本约束**：内置语义版本解析和约束匹配

## 快速开始

### 安装

```bash
go install gitee.com/spock2300/vmake/cmd/vmake@latest
```

安装后 vmake 位于 `~/go/bin/vmake`。

### 调试模式

开发 vmake 或调试 `build.go`/`package.go` 时，设置 `VMAKE_DIR` 指向本地源码：

```bash
export VMAKE_DIR=/path/to/vmake

cd /path/to/vmake
go build -o vmake ./cmd/vmake
./vmake build
```

调试模式下，插件会使用本地 vmake 源码编译，避免版本不匹配问题。

### 基本用法

创建 `build.go` 文件：

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

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
│   ├── buildscript/     # 构建脚本扫描、编译、加载
│   ├── config/          # 配置存储
│   ├── resolver/        # 依赖解析
│   ├── repo/            # 包仓库管理
│   ├── toolchain/       # 工具链管理
│   ├── log/             # 日志输出
│   ├── tui/             # 终端用户界面
│   └── version/         # 版本信息
├── internal/
│   ├── exec/            # 命令执行
│   ├── fs/              # 文件系统工具
│   ├── gitstore/        # Git 仓库管理（共享基础设施）
│   ├── glob/            # 文件匹配
│   ├── gocompile/       # Go 插件编译
│   └── jsonio/          # JSON 序列化
└── docs/                # 设计文档
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

## 命令行用法

### 构建命令

```bash
vmake build [-f|--force] [--toolchain <name>] [--mode <mode>] [-i|--install] [-p|--prefix <dir>]
vmake clean [--all]
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
vmake toolchain init <name>
```

### 包仓库管理

```bash
vmake repo add <name> <url>
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
vmake git tag [version] [--major|--minor|--patch]    # 版本标签
vmake update [version]                                # 自我更新
vmake version                                         # 版本信息
vmake doc                                             # AI 文档
```

全局选项：`-v` (verbose), `-V` (very verbose), `-q` (quiet)

## 开发文档

详细的设计文档请参考 [docs](docs/) 目录：

- [插件 API](docs/PLUGIN_API.md) - 构建脚本和扩展插件 API
- [架构设计](docs/ARCHITECTURE.md) - 系统架构和执行流程
- [目录结构](docs/VMAKE_HOME.md) - ~/.vmake 目录结构

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

## 许可证

本项目采用 MIT 许可证，详情请参见 [LICENSE](LICENSE) 文件。

## 联系方式

- 项目地址：https://gitee.com/spock2300/vmake
