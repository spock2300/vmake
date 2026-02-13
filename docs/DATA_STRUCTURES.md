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
    // 公开字段
    Name        string                        // 选项名称（Package 内唯一）
    Type        OptionType                    // 选项类型
    Default     any                           // 默认值
    Description string                        // 描述文本
    Values      []string                      // 可选值（仅 OptionChoice 使用）
    Group       string                        // 分组名称（TUI 显示用）
    ShowIf      func(ctx *ConfigContext) bool // 条件显示函数

    // 私有字段（运行时状态）
    _value      any                           // 当前值（从配置加载或用户设置）
}
```

### OptionDef（序列化）

```go
// OptionDef 用于序列化的选项定义（不含函数）
type OptionDef struct {
    Name        string     `json:"name"`
    Type        OptionType `json:"type"`
    Default     any        `json:"default"`
    Description string     `json:"description"`
    Values      []string   `json:"values,omitempty"`
    Group       string     `json:"group,omitempty"`
}
```

### 字段说明

| 字段 | 类型 | 说明 | 示例 |
|------|------|------|------|
| `Name` | string | 选项标识符 | `"debug"`, `"optimization"` |
| `Type` | OptionType | 数据类型 | `OptionBool`, `OptionChoice` |
| `Default` | any | 默认值 | `false`, `"O2"`, `42` |
| `Description` | string | TUI 显示的描述 | `"Enable debug mode"` |
| `Values` | []string | Choice 类型的可选值 | `["O0", "O1", "O2", "O3"]` |
| `Group` | string | TUI 分组名称 | `"General"`, `"SSL"` |
| `ShowIf` | func | 条件显示（无法序列化） | `func(ctx) bool { return ctx.Bool("ssl") }` |

---

## Target 数据结构

### Target（运行时）

```go
// pkg/api/target.go

// Target 构建目标（由 OnBuild 回调创建）
type Target struct {
    // 公开字段（API 设置）
    Name       string      // 目标名称（Package 内唯一）
    Kind       TargetKind  // 目标类型
    IsDefault  bool        // 是否默认构建
    Files      []string    // 源文件（支持 glob 模式）
    Includes   []string    // 头文件搜索目录
    Defines    []string    // 预处理器宏定义
    Languages  []string    // 语言标准
    Links      []string    // 外部链接库
    Deps       []string    // 依赖目标（支持 "pkg:target" 格式）
    CFlags     []string    // C 编译选项
    CxxFlags   []string    // C++ 编译选项
    LdFlags    []string    // 链接选项

    // 私有字段（运行时状态）
    _package       string   // 所属 Package 名称
    _filesResolved []string // 解析后的源文件绝对路径
}
```

### TargetDef（序列化）

```go
// TargetDef 用于序列化的目标定义
type TargetDef struct {
    Name      string     `json:"name"`
    Kind      TargetKind `json:"kind"`
    IsDefault bool       `json:"is_default"`
    Files     []string   `json:"files"`
    Includes  []string   `json:"includes"`
    Defines   []string   `json:"defines"`
    Languages []string   `json:"languages"`
    Links     []string   `json:"links"`
    Deps      []string   `json:"deps"`
    CFlags    []string   `json:"cflags"`
    CxxFlags  []string   `json:"cxxflags"`
    LdFlags   []string   `json:"ldflags"`
}
```

### 字段说明

| 字段 | 类型 | 说明 | 示例 |
|------|------|------|------|
| `Name` | string | 目标标识符 | `"myapp"`, `"utils"` |
| `Kind` | TargetKind | 目标类型 | `TargetBinary`, `TargetStatic` |
| `IsDefault` | bool | 默认是否构建 | `true`（默认）, `false` |
| `Files` | []string | 源文件 glob 模式 | `["src/*.c", "lib/*.c"]` |
| `Includes` | []string | 头文件目录 | `["include", "../common"]` |
| `Defines` | []string | 宏定义 | `["DEBUG=1", "USE_SSL"]` |
| `Languages` | []string | 语言标准 | `["c11", "cxx17"]` |
| `Links` | []string | 外部库 | `["ssl", "crypto", "z"]` |
| `Deps` | []string | 依赖目标 | `["utils", "lib:common"]` |
| `CFlags` | []string | C 编译选项 | `["-Wall", "-Wextra"]` |
| `CxxFlags` | []string | C++ 编译选项 | `["-std=c++17"]` |
| `LdFlags` | []string | 链接选项 | `["-Wl,--as-needed"]` |

### 依赖引用格式

```
同包依赖: "target_name"
跨包依赖: "package_name:target_name"
```

---

## Package 数据结构

### Package（运行时）

```go
// pkg/config/package.go

// Package 运行时的 Package 结构
type Package struct {
    // 元信息
    Name       string  // Package 名称（目录名）
    Dir        string  // build.go 所在目录的绝对路径
    BuildDir   string  // 构建目录（Dir/build）
    PluginPath string  // 编译后的 .so 文件路径（BuildDir/plugin.so）

    // 收集的数据
    Options    map[string]*Option  // 选项定义（按名称索引）
    Targets    map[string]*Target  // 目标定义（按名称索引）

    // 回调函数
    OnConfig   ConfigFunc  // 配置回调
    OnBuild    BuildFunc   // 构建回调
}

type ConfigFunc func(ctx *ConfigContext)
type BuildFunc func(ctx *BuildContext)
```

### PackageDef（序列化）

```go
// PackageDef 用于序列化的 Package 定义
type PackageDef struct {
    Name    string                `json:"name"`
    Dir     string                `json:"dir"`
    Options map[string]*OptionDef `json:"options,omitempty"`
    Targets map[string]*TargetDef `json:"targets,omitempty"`
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
    Version  string                     `json:"version"`            // 配置格式版本
    Packages map[string]*PackageConfig  `json:"packages"`           // Package 配置
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

## 运行时注册表

### Registry

```go
// pkg/build/registry.go

// Registry 全局构建注册表
type Registry struct {
    // 基础信息
    RootDir  string              // 工程根目录
    Packages map[string]*Package // 所有 Package（按名称索引）
    Config   *ConfigFile         // 用户配置

    // 构建时生成的索引
    targetIndex map[string]*Target  // 全限定名 -> Target
}
```

### targetIndex 示例

```go
targetIndex = {
    "myproject:myapp":  *Target{Name: "myapp", ...},
    "lib:utils":        *Target{Name: "utils", ...},
    "lib:core":         *Target{Name: "core", ...},
    "app:main":         *Target{Name: "main", ...},
}
```

### 查询方法

```go
// 获取 Target
func (r *Registry) GetTarget(fullName string) *Target

// 获取 Package
func (r *Registry) GetPackage(name string) *Package

// 获取配置值
func (r *Registry) GetOptionValue(pkg, option string) any
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
    Target   *Target    // 目标定义
    Deps     []string   // 直接依赖（全限定名）
    Depended []string   // 被谁依赖（全限定名）
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
        "lib:utils": {Deps: [], Depended: ["lib:core", "app:main"]},
        "lib:core":  {Deps: ["lib:utils"], Depended: ["app:main"]},
        "app:main":  {Deps: ["lib:core", "lib:utils"], Depended: []},
    },
    Order: ["lib:utils", "lib:core", "app:main"],
}
```

---

## 构建缓存

### BuildCache（build/cache.json）

每个 Package 在其 `build/` 目录下维护独立的构建缓存。

```go
// pkg/build/cache.go

// BuildCache 构建缓存
type BuildCache struct {
    Version   string               `json:"version"`
    Artifacts map[string]*Artifact `json:"artifacts"`  // 全限定名 -> 产物
}

// Artifact 构建产物元数据
type Artifact struct {
    TargetName string       `json:"target_name"`  // 全限定名
    OutputPath string       `json:"output_path"`  // 输出文件路径（相对于 BuildDir）
    Sources    []SourceInfo `json:"sources"`      // 源文件信息
    BuildTime  int64        `json:"build_time"`   // 构建时间戳
}

// SourceInfo 源文件信息
type SourceInfo struct {
    Path    string `json:"path"`     // 源文件绝对路径
    ModTime int64  `json:"mod_time"` // 修改时间
    Hash    string `json:"hash"`     // 内容哈希（可选）
}
```

### 缓存文件位置

```
lib/build/cache.json       # lib Package 的构建缓存
app/build/cache.json       # app Package 的构建缓存
```

### 增量构建判断

```go
func NeedRebuild(artifact *Artifact, target *Target) bool {
    // 1. 产物不存在
    if !fileExists(artifact.OutputPath) {
        return true
    }

    // 2. 源文件变化
    for _, src := range artifact.Sources {
        if getModTime(src.Path) > src.ModTime {
            return true
        }
    }

    // 3. 依赖的 Target 变化
    for _, dep := range target.Deps {
        if depArtifact.RebuildTime > artifact.BuildTime {
            return true
        }
    }

    return false
}
```

---

## 存储目录结构

每个 Package 在其 `build.go` 所在目录下拥有独立的 `build/` 目录，包含插件、缓存和构建产物。

```
project/
├── .vmake/
│   └── config.json              # 全局配置（唯一集中存储）
│
├── build/                       # 根 package (project) 的构建目录
│   ├── plugin.so               # 编译后的 Go 插件
│   ├── cache.json              # 增量构建缓存
│   ├── objects/                # 中间目标文件
│   │   └── main.o
│   └── myapp                   # 输出产物
│
├── lib/
│   ├── build.go
│   └── build/                  # lib package 的构建目录
│       ├── plugin.so
│       ├── cache.json
│       ├── objects/
│       │   ├── utils.o
│       │   └── core.o
│       └── libutils.a          # 输出产物
│
└── app/
    ├── build.go
    └── build/                  # app package 的构建目录
        ├── plugin.so
        ├── cache.json
        ├── objects/
        │   └── main.o
        └── main                # 输出产物
```

### 文件用途

| 文件/目录 | 用途 | 读写时机 |
|-----------|------|----------|
| `.vmake/config.json` | 存储用户配置值（全局） | config 命令保存，build 命令读取 |
| `build/plugin.so` | 编译后的 Go 插件 | build.go 变化时重新编译 |
| `build/cache.json` | 增量构建缓存 | 每次构建后更新 |
| `build/objects/` | 中间目标文件 | 编译过程生成 |
| `build/<target>` | 最终输出产物 | 链接过程生成 |

### 设计优势

1. **隔离性**：每个 Package 的构建产物完全隔离，互不干扰
2. **可清理**：删除 `build/` 目录即可清理单个 Package 的构建缓存
3. **可移植**：`build/` 目录可加入 `.gitignore`，不污染源码仓库

---

## 设计决策

### 为什么分离运行时结构和序列化结构？

**原因**：Go 的函数类型无法序列化为 JSON。

| 结构 | 用途 | 包含函数 |
|------|------|----------|
| `Option` | 运行时 | 是（ShowIf） |
| `OptionDef` | 序列化 | 否 |
| `Target` | 运行时 | 否（但可能有私有字段） |
| `TargetDef` | 序列化 | 否 |

### 为什么 Target 需要 `_package` 字段？

**原因**：解析跨包依赖时需要知道 Target 属于哪个 Package。

```
Deps: ["lib:utils"]  // 跨包
Deps: ["core"]       // 同包，需要当前 Package 名称才能构建全限定名
```

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
4. **并行构建**：不同 Package 可并行构建，无锁竞争

---

## 附录：类型关系图

```
┌─────────────────────────────────────────────────────────────┐
│                        Registry                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ Packages: map[string]*Package                          │ │
│  │   ├── "myproject" → Package                            │ │
│  │   │   ├── Options: map[string]*Option                  │ │
│  │   │   │   └── "debug" → Option{Name, Type, ...}       │ │
│  │   │   └── Targets: map[string]*Target                  │ │
│  │   │       └── "myapp" → Target{Name, Kind, Deps, ...} │ │
│  │   ├── "lib" → Package                                  │ │
│  │   └── "app" → Package                                  │ │
│  └────────────────────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ Config: *ConfigFile                                    │ │
│  │   └── Packages: map[string]*PackageConfig              │ │
│  │       └── "myproject" → {Options: {"debug": false}}   │ │
│  └────────────────────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ targetIndex: map[string]*Target                        │ │
│  │   ├── "myproject:myapp" → *Target                      │ │
│  │   ├── "lib:utils" → *Target                            │ │
│  │   └── "app:main" → *Target                             │ │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```
