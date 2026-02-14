# VMake API 设计文档

> 基于 Go 的 C/C++ 构建与配置管理系统

## 目录

- [概述](#概述)
- [整体架构](#整体架构)
- [核心概念](#核心概念)
- [执行流程](#执行流程)
- [API 设计](#api-设计)
- [条件表达式](#条件表达式)
- [TUI 配置界面](#tui-配置界面)
- [项目结构](#项目结构)
- [使用示例](#使用示例)

---

## 概述

VMake 是一个基于 Go 语言实现的 C/C++ 构建系统，灵感来源于 xmake。每个工程目录下的 `build.go` 文件会被编译为 Go 插件（.so），然后由 vmake 加载执行。

核心特性：

- 使用 Go 作为配置语言，类型安全
- 支持 TUI 交互式配置界面
- 支持多目标、多配置
- 基于 Go plugin 实现热加载
- **少即是多** - API 精简，仅保留核心功能

---

## 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                         VMake CLI                           │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────────────┐│
│  │ Scanner │→ │ Compiler│→ │ Loader  │→ │    Executor     ││
│  └─────────┘  └─────────┘  └─────────┘  └─────────────────┘│
│                                              │              │
│                                              ▼              │
│                                    ┌─────────────────┐     │
│                                    │   TUI / Build   │     │
│                                    └─────────────────┘     │
└─────────────────────────────────────────────────────────────┘
```

---

## 核心概念

### Package

每个 `build.go` 文件对应一个 Package，是配置和构建的基本单元。

**命名规则**：
- Package 名称 = `build.go` 所在目录的最后一层目录名
- 隐式声明，无需 API 设置
- 名称必须全局唯一，冲突时报错

| 目录结构 | Package 名称 |
|---------|-------------|
| `/myproject/build.go` | `myproject` |
| `/myproject/lib/build.go` | `lib` |
| `/myproject/core/utils/build.go` | `utils` |
| `/lib/build.go` + `/sub/lib/build.go` | 报错（冲突） |

**Package 的作用**：
- 配置隔离：每个 Package 的 Options 完全独立
- 配置存储：按 Package 名称分组保存
- Target 归属：Target 属于定义它的 Package

### Target

Target 是构建目标，定义在 `OnBuild` 中，归属于当前 Package。

| 属性 | 说明 |
|-----|------|
| 名称 | 在 Package 内唯一 |
| 类型 | binary / static / shared / object |
| 依赖 | 可依赖同包或跨包 Target |

**依赖引用规则**：
- 同包依赖：直接用 Target 名称，如 `AddDeps("utils")`
- 跨包依赖：使用全限定名 `package:target`，如 `AddDeps("lib:utils")`

### 配置存储

配置按 Package 分组存储，全局选项存储在 `global` 字段：

```
.vmake/
└── config.json
```

```json
{
  "version": "1",
  "global": {
    "toolchain": "gcc",
    "mode": "debug",
    "options": {
      "my_global": true
    }
  },
  "packages": {
    "myproject": {
      "options": {
        "debug": false,
        "optimization": "O2"
      }
    },
    "lib": {
      "options": {
        "shared": true
      }
    },
    "app": {
      "options": {
        "enable_ssl": true
      }
    }
  }
}
```

**设计要点**：
- 单一配置文件：所有 Package 的配置集中在 `.vmake/config.json`
- `global` 字段：存储 toolchain、mode 和用户定义的全局选项
- Package 独立：每个 Package 的 Options 互不影响
- 无 Target 配置：Target 的启用/禁用由代码逻辑控制（如 `SetDefault(false)`）

---

## 执行流程

### 统一前置流程

config 和 build 命令共享相同的前置流程：

```
┌──────────────────────────────────────────────────────────┐
│ 1. 扫描阶段 (Scan)                                        │
│    递归扫描工程目录下的 build.go 文件                      │
└──────────────────────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────┐
│ 2. 编译阶段 (Compile)                                     │
│    go build -buildmode=plugin build.go → build.so        │
└──────────────────────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────┐
│ 3. 加载阶段 (Load)                                        │
│    plugin.Open("build.so")                               │
│    查找并调用 Main(*api.Builder) 函数                      │
└──────────────────────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────┐
│ 4. 配置收集 (Collect Options)                             │
│    执行所有 OnConfig 回调                                  │
│    收集所有 Option 定义到全局注册表                         │
└──────────────────────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────┐
│ 5. 加载已有配置 (Load Config)                             │
│    从 .vmake/config.json 加载用户配置值                    │
│    将值填充到对应的 Option                                 │
└──────────────────────────────────────────────────────────┘
                           │
                           ▼
                    ┌──────┴──────┐
                    │   命令类型   │
                    └──────┬──────┘
              ┌────────────┴────────────┐
              │                         │
              ▼                         ▼
┌─────────────────────────┐  ┌─────────────────────────────┐
│ vmake config            │  │ vmake / vmake build         │
│                         │  │ (默认构建模式)               │
├─────────────────────────┤  ├─────────────────────────────┤
│ 6a. TUI 渲染            │  │ 6b. 构建执行                 │
│     渲染配置界面         │  │     执行所有 OnBuild 回调    │
│     用户交互修改         │  │     生成 Target 集合         │
│     保存配置到文件       │  │     解析依赖，编译/链接      │
└─────────────────────────┘  └─────────────────────────────┘
```

### 流程说明

1. **OnConfig 始终执行**：无论是 config 还是 build，都需要先收集选项定义并加载已有值
2. **config 命令**：显示 TUI 界面，允许用户修改配置，然后保存
3. **build 命令**（或无参数）：使用已有配置值，执行构建
4. **默认构建**：直接运行 `vmake` 等同于 `vmake build`

---

## API 设计

### 核心 Builder 接口

```go
package api

type Builder struct{}

type ConfigFunc func(ctx *ConfigContext)
type BuildFunc func(ctx *BuildContext)

func (b *Builder) OnConfig(fn ConfigFunc)
func (b *Builder) OnBuild(fn BuildFunc)
```

### 入口函数签名

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(b *api.Builder) {
    b.OnConfig(func(ctx *api.ConfigContext) { ... })
    b.OnBuild(func(ctx *api.BuildContext) { ... })
}
```

### Option 类型

```go
type OptionType int

const (
    OptionBool   OptionType = iota
    OptionString
    OptionInt
    OptionChoice
)

type Option struct{}

func (o *Option) SetType(t OptionType) *Option
func (o *Option) SetDefault(v any) *Option
func (o *Option) SetDescription(desc string) *Option
func (o *Option) SetValues(vals ...string) *Option
func (o *Option) SetShowIf(fn func(ctx *ConfigContext) bool) *Option
func (o *Option) SetGroup(group string) *Option
func (o *Option) SetGlobal() *Option          // 标记为全局选项（跨 Package 共享）
```

### ConfigContext

```go
type ConfigContext struct{}

func (ctx *ConfigContext) Option(name string) *Option

// 获取选项值（优先配置值，其次默认值）
func (ctx *ConfigContext) Bool(name string) bool
func (ctx *ConfigContext) String(name string) string
func (ctx *ConfigContext) Int(name string) int

// 其他方法
func (ctx *ConfigContext) PackageName() string
func (ctx *ConfigContext) SetConfigValue(name string, val any)
func (ctx *ConfigContext) GetOptions() map[string]*Option

// 全局选项快捷方法
func (ctx *ConfigContext) GlobalOption(name string) *Option  // 创建并标记为全局选项
func (ctx *ConfigContext) GlobalMode() *Option               // 内置 mode 选项快捷方法
```

### Target 类型

> 设计原则：少即是多，仅保留核心 API，共 11 个方法

```go
type TargetKind string

const (
    TargetBinary TargetKind = "binary"
    TargetStatic TargetKind = "static"
    TargetShared TargetKind = "shared"
    TargetObject TargetKind = "object"
)

type Target struct {
    Name string
}

// 基本设置
func (t *Target) SetKind(kind TargetKind) *Target
func (t *Target) SetDefault(isDefault bool) *Target

// 源码与头文件（接受 string 或 []string，支持条件表达式返回的 []string）
func (t *Target) AddFiles(files ...any) *Target
func (t *Target) AddIncludes(dirs ...any) *Target
func (t *Target) AddPublicIncludes(dirs ...any) *Target

// 编译配置
func (t *Target) AddDefines(defines ...any) *Target
func (t *Target) SetLanguages(langs ...string) *Target

// 链接配置
func (t *Target) AddLinks(libs ...any) *Target
func (t *Target) AddDeps(targets ...string) *Target

// 编译/链接选项
func (t *Target) AddCFlags(flags ...any) *Target
func (t *Target) AddCxxFlags(flags ...any) *Target
func (t *Target) AddLdFlags(flags ...any) *Target
```

### API 与 xmake 对照

| xmake | vmake | 说明 |
|-------|-------|------|
| `set_kind` | `SetKind` | 目标类型 |
| `set_default` | `SetDefault` | 是否默认构建 |
| `add_files` | `AddFiles` | 源文件 |
| `add_includedirs` | `AddIncludes` | 头文件目录（私有） |
| `add_sysincludedirs` | `AddPublicIncludes` | 头文件目录（公开，依赖方继承） |
| `add_defines` | `AddDefines` | 宏定义 |
| `add_links` | `AddLinks` | 链接库 |
| `add_deps` | `AddDeps` | 依赖目标 |
| `add_cflags` | `AddCFlags` | C 编译选项 |
| `add_cxxflags` | `AddCxxFlags` | C++ 编译选项 |
| `add_ldflags` | `AddLdFlags` | 链接选项 |
| `set_languages` | `SetLanguages` | 语言标准 |

### BuildContext

```go
type BuildContext struct{}

func (ctx *BuildContext) Target(name string) *Target

// 条件表达式（Package 内选项）
func (ctx *BuildContext) If(option string, then ...string) []string
func (ctx *BuildContext) IfNot(option string, then ...string) []string
func (ctx *BuildContext) Select(option string, mapping map[string]string) string
func (ctx *BuildContext) When(option string, value any) bool

// 获取选项值（Package 内选项）
func (ctx *BuildContext) Bool(name string) bool
func (ctx *BuildContext) String(name string) string
func (ctx *BuildContext) Int(name string) int

// 全局选项方法
func (ctx *BuildContext) GlobalBool(name string) bool
func (ctx *BuildContext) GlobalString(name string) string
func (ctx *BuildContext) IfGlobal(option string, then ...string) []string
func (ctx *BuildContext) SelectGlobal(option string, mapping map[string]string) string
func (ctx *BuildContext) Mode() string  // 获取当前构建模式（debug/release）

// 其他方法
func (ctx *BuildContext) PackageName() string
func (ctx *BuildContext) GetTargets() map[string]*Target
```

### 全局选项

全局选项是跨 Package 共享的配置项，所有 Package 使用相同的值。

```go
// 内置常量
const (
    ModeOptionName      = "mode"
    ToolchainOptionName = "toolchain"
    ModeDebug           = "debug"
    ModeRelease         = "release"
)

// 在 OnConfig 中创建全局选项
b.OnConfig(func(ctx *api.ConfigContext) {
    ctx.GlobalOption("my_global").
        SetType(api.OptionBool).
        SetDefault(true).
        SetDescription("A global option shared across all packages")
})

// 在 OnBuild 中使用全局选项
b.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").
        SetKind(api.TargetBinary).
        AddDefines(ctx.IfGlobal("my_global", "ENABLE_FEATURE")).
        AddCFlags(ctx.SelectGlobal("mode", map[string]string{
            api.ModeDebug:   "-O0 -g",
            api.ModeRelease: "-O2",
        }))
})
```

**全局选项特性**：
- 使用 `GlobalOption()` 创建，自动设置 `group: "Global"`
- 在所有 Package 间共享，值统一
- 如果多个 Package 定义同名全局选项，类型和默认值必须一致
- 内置 `mode` 和 `toolchain` 选项

---

## 条件表达式

### 使用示例

```go
b.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("myapp").
        SetKind(api.TargetBinary).
        AddFiles("src/*.c").
        AddDefines(
            ctx.If("ssl", "USE_SSL"),
            ctx.If("debug", "DEBUG=1"),
        ).
        AddLinks(
            ctx.If("ssl", "ssl", "crypto"),
            ctx.IfNot("minimal", "z"),
        ).
        AddCFlags(
            ctx.Select("optimization", map[string]string{
                "O0": "-O0",
                "O1": "-O1", 
                "O2": "-O2",
                "O3": "-O3",
                "Os": "-Os",
            }),
        ).
        AddCFlags(ctx.SelectGlobal("mode", map[string]string{
            api.ModeDebug:   "-O0 -g",
            api.ModeRelease: "-O2",
        }))
})
```

### 内置模式标志

`GetModeFlags()` 函数根据模式返回默认编译标志：

```go
cflags, defines := api.GetModeFlags(ctx.Mode())
// debug:    cflags=["-O0", "-g"], defines=nil
// release:  cflags=["-O2"], defines=["NDEBUG"]
```

---

## TUI 配置界面

### 界面布局

```
┌─────────────────────────────────────────────────────────────┐
│ VMake Configuration - /path/to/project                      │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│ ┌─ General ─────────────────────────────────────────────┐  │
│ │                                                       │  │
│ │ [x] debug          Enable debug mode                  │  │
│ │ [ ] verbose        Enable verbose output              │  │
│ │                                                       │  │
│ │ optimization: [O2 ▼] Optimization level               │  │
│ │               ┌─────┐                                 │  │
│ │               │ O0  │                                 │  │
│ │               │ O1  │                                 │  │
│ │               │ O2  │                                 │  │
│ │               │ O3  │                                 │  │
│ │               │ Os  │                                 │  │
│ │               └─────┘                                 │  │
│ └───────────────────────────────────────────────────────┘  │
│                                                             │
│ ┌─ SSL ─────────────────────────────────────────────────┐  │
│ │                                                       │  │
│ │ [x] ssl            Enable SSL support                 │  │
│ │     ssl_version: [1.1.1 ▼] SSL library version        │  │
│ │                                                       │  │
│ └───────────────────────────────────────────────────────┘  │
│                                                             │
├─────────────────────────────────────────────────────────────┤
│ <Save (S)>  <Build (B)>  <Cancel (Esc)>  <Help (?)>        │
└─────────────────────────────────────────────────────────────┘
```

### TUI 库选型

推荐使用 [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Lip Gloss](https://github.com/charmbracelet/lipgloss)：

- 纯 Go 实现，无 CGO 依赖
- 声明式 UI 模型
- 丰富的组件生态（Bubbles）
- 跨平台支持

### TUI 组件映射

| Option 类型 | TUI 组件 |
|------------|---------|
| OptionBool | Checkbox |
| OptionString | TextInput |
| OptionInt | NumberInput |
| OptionChoice | Select/Dropdown |

---

## 项目结构

```
vmake/
├── cmd/
│   └── vmake/
│       └── main.go              # CLI 入口
│
├── pkg/
│   ├── api/
│   │   ├── builder.go           # Builder 核心结构
│   │   ├── target.go            # Target 定义
│   │   ├── option.go            # Option 定义
│   │   └── context.go           # 上下文
│   │
│   ├── plugin/
│   │   ├── scanner.go           # build.go 扫描
│   │   ├── compiler.go          # 插件编译
│   │   └── loader.go            # 插件加载
│   │
│   ├── config/
│   │   └── store.go             # 配置存储
│   │
│   ├── tui/
│   │   ├── app.go               # TUI 主应用
│   │   ├── components/
│   │   │   ├── checkbox.go
│   │   │   ├── select.go
│   │   │   └── textinput.go
│   │   └── styles.go            # 样式定义
│   │
│   ├── build/
│   │   ├── compiler.go          # 编译器抽象
│   │   ├── linker.go            # 链接器抽象
│   │   ├── scheduler.go         # 构建调度
│   │   └── cache.go             # 增量构建缓存
│   │
│   └── toolchain/
│       ├── gcc.go               # GCC 工具链
│       ├── clang.go             # Clang 工具链
│       ├── msvc.go              # MSVC 工具链
│       └── detector.go          # 工具链检测
│
├── internal/
│   └── fsutil/
│       └── glob.go              # 文件通配符
│
├── docs/
│   └── API_DESIGN.md            # 本文档
│
├── go.mod
└── go.sum
```

---

## 使用示例

### 简单项目

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(b *api.Builder) {
    b.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("hello").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c").
            AddIncludes("include")
    })
}
```

### 完整项目

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(b *api.Builder) {
    b.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("debug").
            SetType(api.OptionBool).
            SetDefault(false).
            SetDescription("Enable debug mode").
            SetGroup("General")
        
        ctx.Option("optimization").
            SetType(api.OptionChoice).
            SetDefault("O2").
            SetValues("O0", "O1", "O2", "O3", "Os").
            SetDescription("Optimization level").
            SetGroup("General")
        
        ctx.Option("ssl").
            SetType(api.OptionBool).
            SetDefault(true).
            SetDescription("Enable SSL support").
            SetGroup("SSL")
        
        ctx.Option("ssl_version").
            SetType(api.OptionChoice).
            SetDefault("1.1.1").
            SetValues("1.1.1", "3.0").
            SetDescription("SSL library version").
            SetGroup("SSL").
            SetShowIf(func(ctx *api.ConfigContext) bool {
                return ctx.Bool("ssl")
            })
    })
    
    b.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("mylib").
            SetKind(api.TargetStatic).
            AddFiles("src/lib/*.c").
            AddIncludes("include").
            SetLanguages("c11").
            AddCFlags("-Wall", "-Wextra").
            AddCFlags(ctx.Select("optimization", map[string]string{
                "O0": "-O0", "O1": "-O1", "O2": "-O2", "O3": "-O3", "Os": "-Os",
            }))
        
        ctx.Target("myapp").
            SetKind(api.TargetBinary).
            AddFiles("src/app/*.c").
            AddIncludes("include").
            AddDeps("mylib").
            AddDefines(ctx.If("ssl", "USE_SSL")).
            AddDefines(ctx.If("debug", "DEBUG=1")).
            AddLinks(ctx.If("ssl", "ssl", "crypto"))
        
        ctx.Target("tests").
            SetKind(api.TargetBinary).
            AddFiles("tests/*.c").
            AddDeps("mylib").
            SetDefault(false)
    })
}
```

### 多模块项目

```
project/
├── build.go              # 根配置
├── lib/
│   └── build.go          # 库配置
├── app/
│   └── build.go          # 应用配置
└── tests/
    └── build.go          # 测试配置
```

**build.go (根配置)**:
```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(b *api.Builder) {
    b.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("debug").
            SetType(api.OptionBool).
            SetDefault(false).
            SetDescription("Enable debug mode globally")
    })
}
```

**lib/build.go**:
```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(b *api.Builder) {
	b.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("mylib").
			SetKind(api.TargetStatic).
			AddFiles("*.c").
			AddIncludes("internal").      // 私有头文件，仅本目标使用
			AddPublicIncludes("../include") // 公开头文件，依赖方自动继承
	})
}
```

**app/build.go**:
```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(b *api.Builder) {
	b.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("myapp").
			SetKind(api.TargetBinary).
			AddFiles("*.c").
			AddDeps("lib:mylib")  // 自动继承 mylib 的 PublicIncludes
	})
}
```

---

## CLI 命令设计

```bash
vmake                           # 构建所有默认目标（默认模式）
vmake build                     # 构建所有默认目标
vmake build -v                  # 详细输出
vmake build -V                  # 非常详细输出
vmake build -q                  # 安静模式

vmake config                    # 打开 TUI 配置界面

vmake clean                     # 清理构建产物
vmake rebuild                   # 完全重新构建

vmake toolchain init            # 生成全局配置模板
vmake toolchain list            # 列出所有工具链
vmake toolchain show [name]     # 显示工具链详情

vmake version                   # 显示版本信息
```

### 命令行选项

| 选项 | 说明 |
|------|------|
| `-v, --verbose` | 详细输出 |
| `-V, --very-verbose` | 非常详细输出 |
| `-q, --quiet` | 安静模式 |

---

## 开发计划

1. [x] 实现核心 API 框架
2. [x] 实现插件扫描与加载
3. [x] 实现 TUI 配置界面
4. [x] 实现 GCC/Clang 工具链支持
5. [x] 实现增量构建
6. [x] 添加全局选项支持
7. [ ] 添加 MSVC 工具链支持
8. [ ] 添加跨平台编译支持
