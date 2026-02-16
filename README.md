

# VMake

VMake 是一个现代化的 C/C++ 项目构建工具，采用 Go 语言开发。它提供了一个简洁而强大的 API，用于配置和构建多模块 C/C++ 项目。

## 功能特性

- **简洁的 API 设计**：仅需 11 个核心方法，通过方法链 (Fluent API) 实现声明式构建配置
- **灵活的选项系统**：支持布尔、字符串、整数和枚举类型的配置选项
- **条件构建支持**：通过 `If`、`When` 等方法实现条件编译
- **多模块支持**：原生支持多模块项目的构建管理
- **增量编译**：基于依赖分析的智能增量编译，大幅提升构建效率
- **TUI 配置界面**：提供交互式终端用户界面，方便配置项目选项
- **工具链管理**：支持多种编译工具链的灵活切换
- **缓存管理**：自动管理构建缓存，支持增量编译判断

## 快速开始

### 安装

```bash
go install gitee.com/spock2300/vmake/cmd/vmake@latest
```

安装后 vmake 位于 `~/go/bin/vmake`。

### 调试模式

开发 vmake 或调试 `build.go`/`package.go` 时，设置 `VMAKE_DIR` 指向本地源码：

```bash
# 设置 vmake 源码路径
export VMAKE_DIR=/path/to/vmake

# 编译并运行
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

func Main(b *api.Builder) {
    b.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("debug").
            SetType(api.OptionBool).
            SetDefault(true).
            SetDescription("Enable debug mode")
    })

    b.OnBuild(func(ctx *api.BuildContext) {
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
│   ├── api/             # 核心构建 API
│   ├── build/           # 编译、链接、缓存管理
│   ├── config/          # 配置存储
│   ├── plugin/          # 插件加载器
│   ├── toolchain/       # 工具链管理
│   └── tui/             # 终端用户界面
├── internal/glob/       # 文件匹配
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

// 目标配置
ctx.Target(name string) *Target
```

## 配置选项

### 项目级选项

在 `build.go` 中通过 `OnConfig` 函数定义：

```go
b.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Option("optimization").
        SetType(api.OptionChoice).
        SetDefault("O2").
        SetValues("O0", "O1", "O2", "O3").
        SetDescription("Compiler optimization level")
})
```

### 全局选项

VMake 内置以下全局选项：

- `mode`：构建模式 (`debug` / `release`)
- `toolchain`：编译器工具链

### 条件表达式

```go
b.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").
        AddDefines(ctx.If("debug", "DEBUG")...).
        AddCFlags(ctx.Select("optimization", map[string]string{
            "O3": "-DNDEBUG",
        }))
})
```

## 多模块项目

```
myproject/
├── build.go              # 根模块配置
├── app/
│   ├── build.go          # app 模块配置
│   └── src/
├── lib/
│   ├── build.go          # lib 模块配置
│   └── src/
└── include/
```

### 根模块 build.go

```go
func Main(b *api.Builder) {
    b.OnConfig(func(ctx *api.ConfigContext) {
        // 全局选项
    })

    b.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").
            SetKind(api.TargetBinary).
            AddFiles("app/src/main.c").
            AddDeps("lib")
    })
}
```

### 子模块 build.go

```go
func Main(b *api.Builder) {
    b.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("mylib").
            SetKind(api.TargetStatic).
            AddFiles("src/*.c").
            AddIncludes("include")
    })
}
```

## 命令行用法

### 构建命令

```bash
# 构建当前模块
vmake build

# 详细输出
vmake build -v

# 非常详细输出
vmake build -vv
```

### 配置命令

```bash
# 进入交互式配置界面
vmake config
```

### 工具链管理

```bash
# 列出可用工具链
vmake toolchain list

# 显示当前工具链信息
vmake toolchain show

# 初始化新工具链
vmake toolchain init <name>
```

### 清理命令

```bash
# 清理当前模块的构建产物
vmake clean

# 清理所有模块的构建产物
vmake clean --all

# 完全重建
vmake rebuild
```

## 配置文件

### 项目配置 (`.vmake/config.json`)

```json
{
  "version": "1.0",
  "packages": {
    "app": {
      "options": {
        "debug": true
      }
    }
  }
}
```

### 全局配置 (`~/.vmake/config.json`)

```json
{
  "version": "1.0",
  "defaultToolchain": "gcc",
  "toolchains": {
    "gcc": {
      "path": "/usr/bin",
      "c compiler": "gcc",
      "c++ compiler": "g++"
    }
  }
}
```

## 高级特性

### 条件编译

```go
b.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Option("platform").
        SetType(api.OptionChoice).
        SetValues("windows", "linux", "macos")
    
    ctx.Option("features.encryption").
        SetType(api.OptionBool).
        SetDefault(false)
})

b.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").
        AddFiles("src/main.c").
        AddFiles(ctx.If("platform", "windows", "src/win32.c")...).
        AddDefines(ctx.If("features.encryption", "ENABLE_ENCRYPTION")...)
})
```

### 自定义编译选项

```go
ctx.Target("app").
    AddFiles("src/*.c").
    AddIncludes("include", "thirdparty/include").
    AddPublicIncludes("include").
    AddDefines("VERSION=1.0").
    AddLinks("pthread", "dl").
    AddCFlags("-Wall", "-Wextra").
    AddCxxFlags("-std=c++17").
    AddLdFlags("-Wl,--gc-sections")
```

## 开发文档

详细的设计文档请参考 [docs](docs/) 目录：

- [API 设计](docs/API_DESIGN.md)
- [编译设计](docs/BUILD_DESIGN.md)
- [数据结构](docs/DATA_STRUCTURES.md)

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
| `test_data/07_parallel_test` | 并行编译测试 |

运行测试：

```bash
vmake build
```

## 架构设计

### 核心组件

1. **Builder**：构建器入口，负责注册配置和构建回调
2. **ConfigContext**：配置上下文，管理选项定义和值
3. **BuildContext**：构建上下文，管理目标和构建逻辑
4. **BuildGraph**：构建依赖图，分析目标间依赖关系
5. **Scheduler**：调度器，协调编译和链接任务
6. **Compiler**：编译器，处理源文件编译
7. **Linker**：链接器，处理目标文件链接

### 执行流程

```
1. 扫描项目结构，收集 Package
2. 加载并编译插件
3. 执行 OnConfig 回调，收集选项定义
4. 加载保存的配置值
5. (可选) 启动 TUI 进行交互式配置
6. 执行 OnBuild 回调，构建 Target 依赖图
7. 调度编译任务
8. 执行链接任务
9. 保存配置和缓存
```

## 许可证

本项目采用 MIT 许可证，详情请参见 [LICENSE](LICENSE) 文件。

## 贡献指南

欢迎提交 Issue 和 Pull Request！

## 联系方式

- 项目地址：https://gitee.com/spock2300/vmake