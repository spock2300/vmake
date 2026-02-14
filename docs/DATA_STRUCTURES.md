# VMake 数据结构设计

> 核心数据模型与存储格式定义

## 目录

- [概述](#概述)
- [核心概念映射](#核心概念映射)
- [基础类型](#基础类型)
- [Option 数据结构](#option-数据结构)
- [Target 数据结构](#target-数据结构)
- [Package 数据结构](#package-数据结构)
- [配置存储结构](#配置存储结构)
- [运行时注册表](#运行时注册表)
- [构建依赖图](#构建依赖图)
- [构建缓存](#构建缓存)
- [存储目录结构](#存储目录结构)
- [设计决策](#设计决策)

---

## 概述

本文档定义 VMake 的核心数据结构，包括：

1. **API 层数据**：插件直接操作的 `Option`、`Target` 等
2. **运行时数据**：`Package`、`Registry` 等内部状态
3. **持久化数据**：配置文件、构建缓存等

---

## 核心概念映射

```
build.go ──────────► Package ──────────► Option(s)
     │                  │
     │                  └──────────────► Target(s)
     │
     ▼
.vmake/config.json ──► ConfigFile ──► PackageConfig ──► 选项值
```

| 概念 | 来源 | 存储位置 | 生命周期 |
|------|------|----------|----------|
| Option | OnConfig 回调定义 | 内存 + config.json | 配置阶段 |
| Target | OnBuild 回调定义 | 内存 | 构建阶段 |
| Package | 目录名隐式确定 | 内存 | 全局 |
| 配置值 | 用户交互 | config.json | 持久化 |
| 构建缓存 | 构建过程 | build.json | 持久化 |

---

## 基础类型

```go
// pkg/api/types.go

// TargetKind 目标类型
type TargetKind string

const (
    TargetBinary TargetKind = "binary"   // 可执行文件
    TargetStatic TargetKind = "static"   // 静态库
    TargetShared TargetKind = "shared"   // 动态库
    TargetObject TargetKind = "object"   // 目标文件
)

// OptionType 选项类型
type OptionType int

const (
    OptionBool   OptionType = iota  // 布尔型
    OptionString                    // 字符串型
    OptionInt                       // 整数型
    OptionChoice                    // 选择型
)
```

---

## Option 数据结构

### Option（运行时）

```go
// pkg/api/option.go

// Option 选项定义（由 OnConfig 回调创建）
type Option struct {
    // 私有字段（通过方法设置和访问）
    name        string                        // 选项名称（Package 内唯一）
    optType     OptionType                    // 选项类型
    defaultVal  any                           // 默认值
    description string                        // 描述文本
    values      []string                      // 可选值（仅 OptionChoice 使用）
    group       string                        // 分组名称（TUI 显示用）
    showIf      func(ctx *ConfigContext) bool // 条件显示函数
    isGlobal    bool                          // 是否全局选项
}

// 方法
func (o *Option) SetType(t OptionType) *Option
func (o *Option) SetDefault(v any) *Option
func (o *Option) SetDescription(desc string) *Option
func (o *Option) SetValues(vals ...string) *Option
func (o *Option) SetShowIf(fn func(ctx *ConfigContext) bool) *Option
func (o *Option) SetGroup(group string) *Option
func (o *Option) SetGlobal() *Option

func (o *Option) Name() string
func (o *Option) Type() OptionType
func (o *Option) Default() any
func (o *Option) Description() string
func (o *Option) Values() []string
func (o *Option) Group() string
func (o *Option) ShowIf() func(ctx *ConfigContext) bool
func (o *Option) IsGlobal() bool
```

### 字段说明

| 字段 | 类型 | 说明 | 示例 |
|------|------|------|------|
| `name` | string | 选项标识符 | `"debug"`, `"optimization"` |
| `optType` | OptionType | 数据类型 | `OptionBool`, `OptionChoice` |
| `defaultVal` | any | 默认值 | `false`, `"O2"`, `42` |
| `description` | string | TUI 显示的描述 | `"Enable debug mode"` |
| `values` | []string | Choice 类型的可选值 | `["O0", "O1", "O2", "O3"]` |
| `group` | string | TUI 分组名称 | `"General"`, `"SSL"`, `"Global"` |
| `showIf` | func | 条件显示（无法序列化） | `func(ctx) bool { return ctx.Bool("ssl") }` |
| `isGlobal` | bool | 是否全局选项 | `true`, `false` |

---

## Target 数据结构

### Target（运行时）

```go
// pkg/api/target.go

// Target 构建目标（由 OnBuild 回调创建）
type Target struct {
    // 私有字段（通过方法设置和访问）
    name           string      // 目标名称（Package 内唯一）
    kind           TargetKind  // 目标类型
    isDefault      bool        // 是否默认构建
    files          []string    // 源文件（支持 glob 模式）
    includes       []string    // 头文件搜索目录（私有）
    publicIncludes []string    // 头文件搜索目录（公开，依赖方继承）
    defines        []string    // 预处理器宏定义
    languages      []string    // 语言标准
    links          []string    // 外部链接库
    deps           []string    // 依赖目标（支持 "pkg:target" 格式）
    cflags         []string    // C 编译选项
    cxxflags       []string    // C++ 编译选项
    ldflags        []string    // 链接选项
}

// 方法（链式调用）
func (t *Target) SetKind(kind TargetKind) *Target
func (t *Target) SetDefault(isDefault bool) *Target
func (t *Target) AddFiles(files ...any) *Target
func (t *Target) AddIncludes(dirs ...any) *Target
func (t *Target) AddPublicIncludes(dirs ...any) *Target
func (t *Target) AddDefines(defines ...any) *Target
func (t *Target) SetLanguages(langs ...string) *Target
func (t *Target) AddLinks(libs ...any) *Target
func (t *Target) AddDeps(targets ...string) *Target
func (t *Target) AddCFlags(flags ...any) *Target
func (t *Target) AddCxxFlags(flags ...any) *Target
func (t *Target) AddLdFlags(flags ...any) *Target

// Getter 方法
func (t *Target) Name() string
func (t *Target) Kind() TargetKind
func (t *Target) IsDefault() bool
func (t *Target) Files() []string
func (t *Target) Includes() []string
func (t *Target) PublicIncludes() []string
func (t *Target) Defines() []string
func (t *Target) Languages() []string
func (t *Target) Links() []string
func (t *Target) Deps() []string
func (t *Target) CFlags() []string
func (t *Target) CxxFlags() []string
func (t *Target) LdFlags() []string
```

### 字段说明

| 字段 | 类型 | 说明 | 示例 |
|------|------|------|------|
| `name` | string | 目标标识符 | `"myapp"`, `"utils"` |
| `kind` | TargetKind | 目标类型 | `TargetBinary`, `TargetStatic` |
| `isDefault` | bool | 默认是否构建 | `true`（默认）, `false` |
| `files` | []string | 源文件 glob 模式 | `["src/*.c", "lib/*.c"]` |
| `includes` | []string | 头文件目录（私有） | `["include", "../common"]` |
| `publicIncludes` | []string | 头文件目录（公开） | `["../include"]` |
| `defines` | []string | 宏定义 | `["DEBUG=1", "USE_SSL"]` |
| `languages` | []string | 语言标准 | `["c11", "cxx17"]` |
| `links` | []string | 外部库 | `["ssl", "crypto", "z"]` |
| `deps` | []string | 依赖目标 | `["utils", "lib:common"]` |
| `cflags` | []string | C 编译选项 | `["-Wall", "-Wextra"]` |
| `cxxflags` | []string | C++ 编译选项 | `["-std=c++17"]` |
| `ldflags` | []string | 链接选项 | `["-Wl,--as-needed"]` |

### 依赖引用格式

```
同包依赖: "target_name"
跨包依赖: "package_name:target_name"
```

---

## Package 数据结构

### Package（运行时）

```go
// pkg/plugin/package.go

// Package 运行时的 Package 结构
type Package struct {
    // 元信息
    Name string  // Package 名称（目录名）
    Path string  // build.go 文件的绝对路径
    Dir  string  // build.go 所在目录的绝对路径
}
```

### 命名规则

| 目录结构 | Package.Name |
|----------|--------------|
| `/project/build.go` | `"project"` |
| `/project/lib/build.go` | `"lib"` |
| `/project/core/utils/build.go` | `"utils"` |

**冲突检测**：同名 Package 报错

---

## 配置存储结构

### ConfigFile（.vmake/config.json）

```go
// pkg/config/store.go

// ConfigFile 配置文件结构
type ConfigFile struct {
    Version  string                    `json:"version"`            // 配置格式版本
    Global   *GlobalConfig             `json:"global,omitempty"`   // 全局配置
    Packages map[string]*PackageConfig `json:"packages"`           // Package 配置
}

// GlobalConfig 全局配置
type GlobalConfig struct {
    Toolchain string         `json:"toolchain,omitempty"` // 工具链名称
    Mode      string         `json:"mode,omitempty"`      // 构建模式（debug/release）
    Options   map[string]any `json:"options,omitempty"`   // 全局选项值
}

// PackageConfig 单个 Package 的配置值
type PackageConfig struct {
    Options map[string]any `json:"options"`  // 选项名 -> 值
}
```

### 示例

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
        "shared": true,
        "api_version": "2.0"
      }
    },
    "app": {
      "options": {
        "enable_ssl": true,
        "ssl_version": "1.1.1"
      }
    }
  }
}
```

---

## 构建依赖图

### BuildGraph

```go
// pkg/build/graph.go

// BuildGraph 构建依赖图
type BuildGraph struct {
    Nodes map[string]*BuildNode  // 全限定名 -> 节点
    Order []string               // 拓扑排序结果
}

// BuildNode 构建节点
type BuildNode struct {
    FullName string     // 全限定名（package:target）
    PkgName  string     // Package 名称
    Target   *Target    // 目标定义
    Deps     []string   // 直接依赖（全限定名）
}
```

### 构建流程

```
1. 遍历所有 Package.Targets
2. 解析 Deps，生成 BuildNode
3. 拓扑排序，生成 Order
4. 按 Order 顺序执行构建
```

### 示例

```go
// Targets:
// - lib:utils (无依赖)
// - lib:core (依赖 lib:utils)
// - app:main (依赖 lib:core, lib:utils)

BuildGraph{
    Nodes: {
        "lib:utils": {Deps: []},
        "lib:core":  {Deps: ["lib:utils"]},
        "app:main":  {Deps: ["lib:core", "lib:utils"]},
    },
    Order: ["lib:utils", "lib:core", "app:main"],
}
```

---

## 构建缓存

### BuildCache（build/{tcName}-{mode}/cache.json）

每个 Package 在其 `build.go` 所在目录下维护独立的构建缓存，按工具链和模式分目录存储。

```go
// pkg/build/cache.go

const CacheVersion = 3

// BuildCache 构建缓存
type BuildCache struct {
    Version   int                `json:"version"`
    Toolchain ToolchainMeta      `json:"toolchain"`
    Mode      string             `json:"mode,omitempty"`      // 构建模式
    Sources   map[string]*Source `json:"sources"`             // 源文件 -> 编译信息
    mu        sync.RWMutex       `json:"-"`                   // 并发锁
}

// ToolchainMeta 工具链元信息
type ToolchainMeta struct {
    Name    string `json:"name"`     // 工具链名称
    CCPath  string `json:"cc_path"`  // C 编译器绝对路径
    CXXPath string `json:"cxx_path"` // C++ 编译器绝对路径
    Host    string `json:"host"`     // 目标三元组
}

// Source 源文件编译信息
type Source struct {
    ModTime int64    `json:"mod_time"` // 源文件和依赖的最大修改时间
    ObjPath string   `json:"obj_path"` // 目标文件路径
    Deps    []string `json:"deps"`     // 依赖的头文件绝对路径
}
```

### 缓存文件位置

```
lib/build/gcc-debug/cache.json       # lib Package 的构建缓存 (gcc, debug 模式)
lib/build/gcc-release/cache.json     # lib Package 的构建缓存 (gcc, release 模式)
app/build/gcc-debug/cache.json       # app Package 的构建缓存
```

### 增量构建判断

```go
func (c *BuildCache) GetIfValid(sourcePath string) *Source {
    src, ok := c.Sources[sourcePath]
    if !ok {
        return nil  // 无缓存
    }

    // 目标文件不存在
    if _, err := os.Stat(src.ObjPath); os.IsNotExist(err) {
        return nil
    }

    // 源文件变化
    info, err := os.Stat(sourcePath)
    if err != nil || info.ModTime().Unix() > src.ModTime {
        return nil
    }

    // 头文件变化
    for _, dep := range src.Deps {
        depInfo, err := os.Stat(dep)
        if err != nil || depInfo.ModTime().Unix() > src.ModTime {
            return nil
        }
    }

    return src  // 缓存有效
}
```

---

## 存储目录结构

每个 Package 在其 `build.go` 所在目录下拥有独立的 `build/` 目录，包含插件、缓存和构建产物。按工具链和模式分子目录。

```
project/
├── .vmake/
│   └── config.json              # 全局配置（唯一集中存储）
│
├── build/                       # 根 package (project) 的构建目录
│   ├── plugin.so               # 编译后的 Go 插件
│   ├── compile_commands.json   # LSP 编译数据库
│   ├── gcc-debug/              # gcc 工具链 debug 模式
│   │   ├── cache.json          # 增量构建缓存
│   │   ├── objects/            # 中间目标文件
│   │   │   └── main.o
│   │   └── myapp               # 输出产物
│   └── gcc-release/            # gcc 工具链 release 模式
│       ├── cache.json
│       ├── objects/
│       └── myapp
│
├── lib/
│   ├── build.go
│   └── build/                  # lib package 的构建目录
│       ├── plugin.so
│       ├── gcc-debug/
│       │   ├── cache.json
│       │   ├── objects/
│       │   │   ├── utils.o
│       │   │   └── core.o
│       │   └── libutils.a      # 输出产物
│       └── gcc-release/
│
└── app/
    ├── build.go
    └── build/                  # app package 的构建目录
        ├── plugin.so
        ├── gcc-debug/
        │   ├── cache.json
        │   ├── objects/
        │   │   └── main.o
        │   └── main            # 输出产物
        └── gcc-release/
```

### 文件用途

| 文件/目录 | 用途 | 读写时机 |
|-----------|------|----------|
| `.vmake/config.json` | 存储用户配置值（全局） | config 命令保存，build 命令读取 |
| `build/plugin.so` | 编译后的 Go 插件 | build.go 变化时重新编译 |
| `build/compile_commands.json` | LSP 编译数据库 | 每次构建后生成 |
| `build/{tc}-{mode}/cache.json` | 增量构建缓存 | 每次构建后更新 |
| `build/{tc}-{mode}/objects/` | 中间目标文件 | 编译过程生成 |
| `build/{tc}-{mode}/<target>` | 最终输出产物 | 链接过程生成 |

### 设计优势

1. **隔离性**：每个 Package 的构建产物完全隔离，互不干扰
2. **多配置支持**：不同工具链和模式的构建产物独立存储
3. **可清理**：删除 `build/` 目录即可清理所有构建缓存
4. **LSP 集成**：自动生成 compile_commands.json 支持 IDE

---

## 设计决策

### 为什么使用私有字段和 Getter 方法？

**原因**：提供更好的封装性和 API 稳定性。

| 设计 | 优点 |
|------|------|
| 私有字段 + Getter | 可以后续添加验证逻辑，保持 API 兼容 |
| 链式调用 | 支持声明式配置风格 |

### 为什么需要 BuildGraph？

**原因**：
1. 拓扑排序确定构建顺序
2. 检测循环依赖
3. 支持并行构建（无依赖的节点可并行）

### 为什么使用全限定名索引？

**原因**：全局唯一，避免命名冲突。

```
"lib:utils"    // 明确指向 lib Package 的 utils Target
"app:utils"    // 明确指向 app Package 的 utils Target（可以共存）
```

### 为什么配置是 Package 级别而非 Target 级别？

**原因**：
1. 简化配置模型
2. Target 的启用/禁用由代码逻辑控制（SetDefault + 条件表达式）
3. 减少用户配置负担

### 为什么每个 Package 使用独立的 build 目录？

**原因**：
1. **隔离性**：构建产物互不干扰，避免命名冲突
2. **可清理**：删除单个 `build/` 目录不影响其他 Package
3. **可移植**：`build/` 加入 `.gitignore`，不污染版本控制
4. **多配置**：支持不同工具链和模式的构建产物独立存储

### 为什么需要 Global 配置？

**原因**：
1. 工具链选择应该全局一致
2. 构建模式（debug/release）应该全局一致
3. 某些选项（如 SSL 支持开关）可能需要在多个 Package 间保持一致

---

## 工具链配置

### 全局配置文件 (`~/.vmake/config.json`)

工具链配置存储在用户主目录下，所有项目共享。

```go
// pkg/toolchain/config.go

type GlobalConfig struct {
    Version          string                `json:"version"`
    DefaultToolchain string                `json:"default_toolchain"`
    Toolchains       map[string]*Toolchain `json:"toolchains"`
}

type Toolchain struct {
    Name         string       `json:"name"`
    DisplayName  string       `json:"display_name"`
    Host         string       `json:"host"`
    Tools        Tools        `json:"tools"`
    DefaultFlags DefaultFlags `json:"default_flags"`
    DownloadURL  string       `json:"download_url"`
    InstallPath  string       `json:"install_path"`
}

type Tools struct {
    CC     string `json:"cc"`
    CXX    string `json:"cxx"`
    AR     string `json:"ar"`
    LD     string `json:"ld"`
    STRIP  string `json:"strip"`
    RANLIB string `json:"ranlib"`
}

type DefaultFlags struct {
    CFlags   []string `json:"cflags"`
    CxxFlags []string `json:"cxxflags"`
    LdFlags  []string `json:"ldflags"`
}
```

### 配置示例

```json
{
  "version": "1",
  "default_toolchain": "gcc",
  "toolchains": {
    "gcc": {
      "name": "gcc",
      "display_name": "System GCC",
      "host": "x86_64-linux-gnu",
      "tools": {
        "cc": "gcc",
        "cxx": "g++",
        "ar": "ar",
        "ld": "ld",
        "strip": "strip",
        "ranlib": "ranlib"
      },
      "default_flags": {
        "cflags": ["-O2", "-Wall", "-Wstrict-prototypes", "-fno-strict-aliasing", "-fno-common", "-fno-pic"],
        "cxxflags": ["-O2", "-Wall", "-Wextra", "-fno-strict-aliasing", "-fno-common", "-fno-pic"],
        "ldflags": ["-Wl,--as-needed"]
      },
      "download_url": "",
      "install_path": ""
    },
    "arm-gcc": {
      "name": "arm-gcc",
      "display_name": "ARM Cross GCC",
      "host": "arm-linux-gnueabihf",
      "tools": {
        "cc": "arm-linux-gnueabihf-gcc",
        "cxx": "arm-linux-gnueabihf-g++",
        "ar": "arm-linux-gnueabihf-ar",
        "ld": "arm-linux-gnueabihf-ld",
        "strip": "arm-linux-gnueabihf-strip",
        "ranlib": "arm-linux-gnueabihf-ranlib"
      },
      "default_flags": {
        "cflags": ["-O2", "-Wall", "-Wstrict-prototypes", "-fno-strict-aliasing", "-fno-common", "-fno-pic", "-march=armv7-a", "-mfpu=neon"],
        "cxxflags": ["-O2", "-Wall", "-Wextra", "-fno-strict-aliasing", "-fno-common", "-fno-pic", "-march=armv7-a", "-mfpu=neon"],
        "ldflags": ["-Wl,--as-needed"]
      },
      "download_url": "https://example.com/arm-toolchain.tar.gz",
      "install_path": "/opt/arm-toolchain"
    }
  }
}
```

### 字段说明

| 字段 | 说明 |
|------|------|
| `name` | 工具链标识符 |
| `display_name` | 显示名称 |
| `host` | 目标三元组（如 x86_64-linux-gnu） |
| `tools.cc` | C 编译器 |
| `tools.cxx` | C++ 编译器 |
| `tools.ar` | 静态库工具 |
| `tools.ld` | 链接器 |
| `tools.strip` | 符号剥离工具 |
| `tools.ranlib` | 静态库索引工具 |
| `default_flags` | 默认编译/链接选项 |
| `download_url` | 下载地址（预留） |
| `install_path` | 安装路径（用于交叉编译工具链） |

### 工具链选择流程

```
1. 项目配置 .vmake/config.json 中的 toolchain 字段
2. 全局配置 ~/.vmake/config.json 中的 default_toolchain
3. 内置默认值 (gcc)
```

### 项目配置扩展

项目的 `.vmake/config.json` 可指定使用的工具链：

```json
{
  "version": "1",
  "toolchain": "arm-gcc",
  "packages": {
    "myproject": { "options": {...} }
  }
}
```

### CLI 命令

```bash
vmake toolchain init          # 生成全局配置模板
vmake toolchain list          # 列出所有工具链
vmake toolchain show [name]   # 显示工具链详情（默认显示当前选中的）
```

### 内置默认值

当 `~/.vmake/config.json` 不存在时，使用内置默认值：
- 工具链名称: `gcc`
- 工具: `{cc: "gcc", cxx: "g++", ar: "ar", ld: "ld", ...}`

---

## 附录：类型关系图

```
┌─────────────────────────────────────────────────────────────┐
│                     构建流程数据流                           │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  build.go ──────► Plugin.Load() ──────► Builder             │
│                                                │            │
│                    ┌───────────────────────────┼───────────┐│
│                    │                           │           ││
│                    ▼                           ▼           ││
│            OnConfig(ctx)               OnBuild(ctx)        ││
│                    │                           │           ││
│                    ▼                           ▼           ││
│            ConfigContext              BuildContext          ││
│            ├── Option(s)              ├── Target(s)         ││
│            └── cfgVals                └── globalVals        ││
│                                                             │
├─────────────────────────────────────────────────────────────┤
│                     配置文件结构                             │
│                                                             │
│  .vmake/config.json                                         │
│  {                                                          │
│    "version": "1",                                          │
│    "global": {                                              │
│      "toolchain": "gcc",                                    │
│      "mode": "debug",                                       │
│      "options": {...}                                       │
│    },                                                       │
│    "packages": {                                            │
│      "myproject": { "options": {...} },                     │
│      "lib": { "options": {...} }                            │
│    }                                                        │
│  }                                                          │
│                                                             │
├─────────────────────────────────────────────────────────────┤
│                     构建缓存结构                             │
│                                                             │
│  build/gcc-debug/cache.json                                 │
│  {                                                          │
│    "version": 3,                                            │
│    "toolchain": {                                           │
│      "name": "gcc",                                         │
│      "cc_path": "/usr/bin/gcc",                             │
│      "cxx_path": "/usr/bin/g++",                            │
│      "host": "x86_64-linux-gnu"                             │
│    },                                                       │
│    "mode": "debug",                                         │
│    "sources": {                                             │
│      "main.c": {                                            │
│        "mod_time": 1234567890,                              │
│        "obj_path": "build/gcc-debug/objects/main.c.o",      │
│        "deps": ["header.h", "/usr/include/stdio.h"]         │
│      }                                                      │
│    }                                                        │
│  }                                                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```
