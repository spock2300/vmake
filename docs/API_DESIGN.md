# VMake API 设计文档

> 基于 Go 的 C/C++ 构建与配置管理系统

## 目录

- [概述](#概述)
- [整体架构](#整体架构)
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

## 执行流程

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
│ 5. TUI 渲染 (Render TUI)                                  │
│    根据收集的 Option 定义渲染配置界面                       │
│    用户交互修改配置值                                       │
│    保存配置到 .vmake/config.json                          │
└──────────────────────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────┐
│ 6. 构建阶段 (Build)                                       │
│    加载 .vmake/config.json                                │
│    执行所有 OnBuild 回调                                   │
│    生成 Target 集合                                        │
│    解析依赖关系，执行编译/链接                              │
└──────────────────────────────────────────────────────────┘
```

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

import "github.com/vmake/api"

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
```

### ConfigContext

```go
type ConfigContext struct{}

func (ctx *ConfigContext) Option(name string) *Option

func (ctx *ConfigContext) Bool(name string) bool
func (ctx *ConfigContext) String(name string) string
func (ctx *ConfigContext) Int(name string) int
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

// 源码与头文件
func (t *Target) AddFiles(files ...string) *Target
func (t *Target) AddIncludes(dirs ...string) *Target

// 编译配置
func (t *Target) AddDefines(defines ...string) *Target
func (t *Target) SetLanguages(langs ...string) *Target

// 链接配置
func (t *Target) AddLinks(libs ...string) *Target
func (t *Target) AddDeps(targets ...string) *Target

// 编译/链接选项
func (t *Target) AddCFlags(flags ...string) *Target
func (t *Target) AddCxxFlags(flags ...string) *Target
func (t *Target) AddLdFlags(flags ...string) *Target
```

### API 与 xmake 对照

| xmake | vmake | 说明 |
|-------|-------|------|
| `set_kind` | `SetKind` | 目标类型 |
| `set_default` | `SetDefault` | 是否默认构建 |
| `add_files` | `AddFiles` | 源文件 |
| `add_includedirs` | `AddIncludes` | 头文件目录 |
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

// 条件表达式
func (ctx *BuildContext) If(option string, then ...string) []string
func (ctx *BuildContext) IfNot(option string, then ...string) []string
func (ctx *BuildContext) Select(option string, mapping map[string]string) string
func (ctx *BuildContext) When(option string, value any) bool
```

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
        )
})
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

import "github.com/vmake/api"

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

import "github.com/vmake/api"

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

import "github.com/vmake/api"

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

import "github.com/vmake/api"

func Main(b *api.Builder) {
    b.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("mylib").
            SetKind(api.TargetStatic).
            AddFiles("*.c").
            AddIncludes("../include")
    })
}
```

**app/build.go**:
```go
package main

import "github.com/vmake/api"

func Main(b *api.Builder) {
    b.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("myapp").
            SetKind(api.TargetBinary).
            AddFiles("*.c").
            AddDeps("mylib").
            AddIncludes("../include")
    })
}
```

---

## 配置存储格式

**.vmake/config.json**:
```json
{
  "version": "1",
  "options": {
    "debug": false,
    "optimization": "O2",
    "ssl": true,
    "ssl_version": "1.1.1"
  },
  "targets": {
    "mylib": { "enabled": true },
    "myapp": { "enabled": true },
    "tests": { "enabled": false }
  }
}
```

---

## CLI 命令设计

```bash
vmake config                    # 打开 TUI 配置界面
vmake config --list             # 列出所有配置选项
vmake config --set <key>=<value># 命令行设置配置

vmake build                     # 构建所有默认目标
vmake build <target>            # 构建指定目标
vmake build -j4                 # 并行构建（4 jobs）
vmake build -v                  # 详细输出

vmake clean                     # 清理构建产物
vmake rebuild                   # 完全重新构建

vmake targets                   # 列出所有目标
vmake show <target>             # 显示目标详细信息
```

---

## 开发计划

1. [ ] 实现核心 API 框架
2. [ ] 实现插件扫描与加载
3. [ ] 实现 TUI 配置界面
4. [ ] 实现 GCC/Clang 工具链支持
5. [ ] 实现增量构建
6. [ ] 添加 MSVC 工具链支持
7. [ ] 添加跨平台编译支持
