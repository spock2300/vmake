# VMake 包管理系统设计

> 基于 Git Repo 的第三方依赖管理与构建系统

## 目录

- [概述](#概述)
- [整体架构](#整体架构)
- [目录结构](#目录结构)
- [核心概念](#核心概念)
- [API 设计](#api-设计)
- [配置存储](#配置存储)
- [执行流程](#执行流程)
- [源代码管理](#源代码管理)
- [构建系统支持](#构建系统支持)
- [缓存管理](#缓存管理)
- [CLI 命令](#cli-命令)
- [包定义格式](#包定义格式)
- [使用示例](#使用示例)
- [实现计划](#实现计划)

---

## 概述

VMake 包管理系统为 C/C++ 项目提供第三方依赖的管理能力，灵感来源于 xmake 的 repo 系统。

核心特性：

- **Git 仓库作为包源**：独立的 Git 仓库存储包定义
- **Go 插件定义包**：包定义使用 Go 语言，与项目 build.go 风格一致
- **统一 TUI 配置**：包的 options 与项目 options 在同一界面配置
- **Semver 版本管理**：支持语义化版本范围匹配
- **源代码与编译产物分离**：一个 Git 仓库包含所有版本，切换版本只需 `git checkout`
- **Option 隔离缓存**：不同配置选项的包独立缓存
- **多构建系统支持**：CMake、Autotools、Makefile、vmake Native
- **交叉编译支持**：通过 Target/SysRoot 指定交叉编译目标

设计原则：

- **少即是多**：仅支持 Git 源码，不支持 tarball/预编译
- **统一体验**：包配置通过 `vmake config` 完成，无需额外命令
- **类型安全**：Go 编译器保证包定义的正确性
- **API 简洁**：最小化 API 表面积，降低心智负担

---

## 整体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                            VMake CLI                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────────────┐  │
│  │ 项目 build.go │───>│ OnRequire()  │───>│ 收集依赖声明          │  │
│  └──────────────┘    └──────────────┘    └──────────────────────┘  │
│                                                  │                  │
│                                                  ▼                  │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │                      Resolver                                 │  │
│  │  ┌─────────────────┐  ┌─────────────────┐  ┌───────────────┐ │  │
│  │  │ ~/.vmake/repos/ │  │ 加载 package.go │  │ Semver 匹配   │ │  │
│  │  │ 查找包定义       │─>│ 获取 options    │─>│ 解析版本      │ │  │
│  │  └─────────────────┘  └─────────────────┘  └───────────────┘ │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                      │                              │
│                                      ▼                              │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │                     TUI / Config                              │  │
│  │      用户配置项目 options + 包 options                        │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                      │                              │
│                                      ▼                              │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │                      Installer                                │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌───────────────────────┐ │  │
│  │  │ 检查编译缓存 │  │ 源代码缓存  │  │ 编译安装到缓存         │ │  │
│  │  │ packages/   │  │ sources/    │  │ packages/{repo}/...   │ │  │
│  │  └─────────────┘  └─────────────┘  └───────────────────────┘ │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                      │                              │
│                                      ▼                              │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │                        Build                                  │  │
│  │      Target.AddPackages() → 注入 includes/links              │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 目录结构

### 全局目录 (`~/.vmake/`)

```
~/.vmake/
├── config.json                  # 全局配置（toolchain 等）
├── repos/                       # 包仓库目录（package.go 定义）
│   ├── official/                # 官方 repo（默认）
│   │   └── packages/
│   │       └── z/               # 按首字母分组
│   │           └── zlib/
│   │               └── package.go
│   │
│   └── my-custom/               # 用户添加的 repo
│       └── packages/
│           └── ...
│
├── sources/                     # 源代码缓存（按 repo/name 组织）
│   ├── official/                # repo 名称
│   │   └── zlib/                # 包名称
│   │       └── repo/            # Git 仓库（包含所有版本的完整历史）
│   │           ├── .git/
│   │           ├── CMakeLists.txt
│   │           ├── adler32.c
│   │           ├── compress.c
│   │           └── ...
│   │
│   └── my-custom/               # 另一个 repo，避免包名冲突
│       └── zlib/
│           └── repo/
│
└── packages/                    # 编译产物缓存（按 repo/name/version/hash 组织）
    └── official/                # repo 名称
        └── zlib/                # 包名称
            └── 1.3.1/           # 版本
                ├── Z2NjLWRlYnVnLXNoYXJlZC1mYWxzZQ/    # gcc + debug + {shared:false}
                │   ├── build/
                │   └── install/
                │       ├── include/
                │       └── lib/libz.a
                ├── Z2NjLXJlbGVhc2Utc2hhcmVkLWZhbHNl/  # gcc + release + {shared:false}
                │   └── ...
                └── YWFyY2g2NC1kZWJ1Zy1zaGFyZWQtZmFsc2U/ # aarch64 + debug + {shared:false}
                    └── ...
```

**路径计算**：hash 由全局参数 + 包 options 共同计算：

```
toolchain=gcc + mode=debug + options={shared:false}
    ↓ CacheHash()
目录名: Z2NjLWRlYnVnLXNoYXJlZC1mYWxzZQ
    ↓
完整路径: ~/.vmake/packages/official/zlib/1.3.1/{hash}/install/
```

**hash 包含的参数**：

| 参数 | 来源 | 示例 |
|------|------|------|
| toolchain | 全局配置 | `gcc`, `clang`, `aarch64-linux-gnu` |
| mode | 全局配置 | `debug`, `release` |
| options | 包配置 | `{shared:true, minizip:false}` |

**相同包 + 相同 options，不同全局参数 → 不同缓存目录**。

**目录说明**：

| 目录 | 用途 | 生命周期 |
|------|------|---------|
| `repos/` | 包定义（package.go） | 跟随 repo 更新 |
| `sources/{repo}/{name}/repo/` | 完整 Git 仓库 | 长期保留，支持版本切换 |
| `packages/{repo}/{name}/{ver}/{hash}/` | 编译产物 | 可清理，按需重建 |

**为什么包含 repo 名称**：避免不同 repo 之间的包名冲突，路径与 `AddRequires("official/zlib >=1.2.0")` 一一对应。

### 项目目录

```
project/
├── build.go                     # 项目配置
├── .vmake/
│   └── config.json              # 项目配置（含 requires 字段）
└── build/
    └── ...
```

---

## 核心概念

### Repo（仓库）

Repo 是包定义的集合，存储在独立的 Git 仓库中。

**约定**：包路径由名称自动推导
- `official/zlib` → `repos/official/packages/z/zlib/package.go`
- `official/openssl` → `repos/official/packages/o/openssl/package.go`

包搜索通过扫描 `packages/{first_char}/{name}/` 目录实现，无需索引文件。

### Package（包）

Package 是第三方库的抽象，定义了如何获取、配置和构建该库。

**包的标识**：`{repo_name}/{package_name}`

- 例如：`official/zlib`、`official/openssl`

### Require（依赖声明）

在项目的 `OnRequire` 回调中声明依赖：

```go
b.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("official/zlib >=1.2.0", "official/openssl >=3.0")
})
```

**依赖格式**：`{repo/name} {version_constraint}`

例如：
- `"official/zlib >=1.2.0"`
- `"official/openssl ~3.0"`

### PackageOption（包选项）

包可以定义可配置的选项，用户通过 TUI 配置：

```go
// 包定义中
p.Option("shared").
    SetType(api.OptionBool).
    SetDefault(false).
    SetDescription("Build shared library")
```

### Toolchain（工具链）

编译时使用的工具链信息，由 vmake 根据项目配置自动传递给包：

- **本地编译**：使用系统默认的 gcc/clang
- **交叉编译**：使用 Target 指定目标平台，如 `aarch64-linux-gnu`

```go
// PackageContext 中获取工具链信息
ctx.CC()      // C 编译器
ctx.CXX()     // C++ 编译器
ctx.Target()  // 交叉编译目标（可能为空）
ctx.SysRoot() // sysroot 路径（可能为空）
```

---

## API 设计

### 设计原则

1. **三阶段分离**：OnRequire（依赖声明）→ OnConfig（选项定义）→ OnBuild（构建执行）
2. **风格统一**：项目和包使用一致的回调模式
3. **无条件依赖**：所有依赖在 OnRequire 中声明，不支持条件依赖
4. **一个包一种配置**：同一个包在一个项目中只能有一种配置

### 统一入口函数风格

项目和包的插件都使用相同的入口函数风格：

```go
// build.go（项目插件）
func Main(b *api.Builder) {
    b.OnRequire(func(ctx *api.RequireContext) { ... })
    b.OnConfig(func(ctx *api.ConfigContext) { ... })
    b.OnBuild(func(ctx *api.BuildContext) { ... })
}

// package.go（包插件）
func Package(p *api.Package) {
    p.OnRequire(func(ctx *api.PackageRequireContext) { ... })
    p.Option("shared").SetType(api.OptionBool).SetDefault(false)
    p.Build(func(ctx *api.PackageContext) { ... })
}
```

### RequireContext（项目依赖声明）

```go
// pkg/api/require.go

type RequireFunc func(ctx *RequireContext)

type RequireContext struct{}

// 依赖声明（支持多个依赖，格式："repo/name >=version"）
func (ctx *RequireContext) AddRequires(deps ...string)
func (ctx *RequireContext) GetRequires() []RequireInfo

type RequireInfo struct {
    Name    string  // "official/zlib"
    Version string  // ">=1.2.0"
}
```

### PackageRequireContext（包依赖声明）

包声明依赖时使用独立的上下文类型：

```go
// pkg/api/require.go

type PackageRequireContext struct{}

// 依赖声明（与项目风格一致）
func (ctx *PackageRequireContext) AddRequires(deps ...string)
func (ctx *PackageRequireContext) GetRequires() []RequireInfo
```

### Package（包定义）

```go
### Package（包定义）

```go
// pkg/api/package.go

type Package struct {}

// 元信息
func (p *Package) SetGit(url string) *Package
func (p *Package) SetHomepage(url string) *Package
func (p *Package) SetDescription(desc string) *Package
func (p *Package) SetLicense(license string) *Package

// 版本管理（ref 可以是 tag、commit hash 或 branch）
func (p *Package) AddVersion(version, ref string) *Package

// 依赖声明
func (p *Package) OnRequire(fn func(ctx *PackageRequireContext)) *Package

// 选项定义（链式调用）
func (p *Package) Option(name string) *Option

// 构建回调
func (p *Package) Build(fn func(ctx *PackageContext)) *Package
```

### InstalledPackage

```go
// pkg/api/package.go

// InstalledPackage 描述已安装的包，供依赖方访问
type InstalledPackage struct {
    Name       string  // "official/zlib"
    Version    string  // "1.3.1"
    InstallDir string  // ~/.vmake/packages/official/zlib/1.3.1/{hash}/install
    IncludeDir string  // InstallDir + "/include"
    LibDir     string  // InstallDir + "/lib"
    BinDir     string  // InstallDir + "/bin"
}
```

### PackageContext

```go
// pkg/api/package.go

type PackageContext struct {
    deps map[string]*InstalledPackage  // 已安装的依赖
}

// 依赖访问（获取 OnRequire 中声明的依赖）
func (ctx *PackageContext) Dep(name string) *InstalledPackage  // name: "official/zlib"
func (ctx *PackageContext) Deps() map[string]*InstalledPackage

// 编译器信息
func (ctx *PackageContext) CC() string           // C 编译器: "gcc" | "clang" | "cl.exe"
func (ctx *PackageContext) CXX() string          // C++ 编译器: "g++" | "clang++" | "cl.exe"
func (ctx *PackageContext) AR() string           // 静态库工具: "ar" | "llvm-ar" | "lib.exe"
func (ctx *PackageContext) Target() string       // 交叉编译目标，如 "aarch64-linux-gnu"，为空表示本地编译
func (ctx *PackageContext) SysRoot() string      // 交叉编译 sysroot 路径，可能为空
func (ctx *PackageContext) CFlags() string       // C 编译标志
func (ctx *PackageContext) CXXFlags() string     // C++ 编译标志
func (ctx *PackageContext) LDFlags() string      // 链接标志

// 环境变量
func (ctx *PackageContext) Env() map[string]string  // 返回 CC, CXX, CFLAGS, CXXFLAGS, LDFLAGS 等

// 目录
func (ctx *PackageContext) SourceDir() string    // 源代码目录: ~/.vmake/sources/{repo}/{name}/repo/
func (ctx *PackageContext) BuildDir() string     // 编译目录: ~/.vmake/packages/{repo}/{name}/{ver}/{hash}/build/
func (ctx *PackageContext) InstallDir() string   // 安装目录: ~/.vmake/packages/{repo}/{name}/{ver}/{hash}/install/

// 选项读取
func (ctx *PackageContext) Bool(name string) bool
func (ctx *PackageContext) String(name string) string
func (ctx *PackageContext) Int(name string) int
func (ctx *PackageContext) BoolStr(name string) string  // 返回 "ON" 或 "OFF"（用于 CMake）

// 条件表达式
func (ctx *PackageContext) If(option string, then ...string) []string
func (ctx *PackageContext) IfNot(option string, then ...string) []string
func (ctx *PackageContext) Select(option string, mapping map[string]string) string

// CMake 辅助方法（自动处理标准参数）
// CMakeConfigure 执行 cmake 配置，自动添加: -S, -B, -DCMAKE_INSTALL_PREFIX, 
// -DCMAKE_C_COMPILER, -DCMAKE_CXX_COMPILER, -DCMAKE_BUILD_TYPE, 交叉编译参数等
func (ctx *PackageContext) CMakeConfigure(extraArgs ...string) error
// CMakeBuild 执行 cmake --build，可添加 -j4 等参数
func (ctx *PackageContext) CMakeBuild(args ...string) error
// CMakeInstall 执行 cmake --install
func (ctx *PackageContext) CMakeInstall() error

// Autotools 辅助方法
// Configure 执行 configure 脚本，自动添加: --prefix, --host, 环境变量(CC, CXX 等)
// 需要先用 RunIn(SourceDir, "autoreconf", "-fi") 生成 configure 脚本
func (ctx *PackageContext) Configure(extraArgs ...string) error
// Make 执行 make，可添加 -j4, install 等参数
func (ctx *PackageContext) Make(args ...string) error

// 底层命令执行（保留完全控制）
// Run 在 BuildDir 执行命令
func (ctx *PackageContext) Run(name string, args ...string) error
// RunIn 在指定目录执行命令
func (ctx *PackageContext) RunIn(dir, name string, args ...string) error
// RunWithEnv 在 BuildDir 执行命令，带额外环境变量
func (ctx *PackageContext) RunWithEnv(env map[string]string, name string, args ...string) error

// 文件操作
func (ctx *PackageContext) CopyDir(src, dest string) error
func (ctx *PackageContext) CopyFile(src, dest string) error
func (ctx *PackageContext) MkdirAll(path string) error

// 特性检测
func (ctx *PackageContext) AssertHasCFuncs(funcs ...string) error
func (ctx *PackageContext) AssertHasCxxFuncs(funcs ...string) error

// vmake 原生构建（复用现有 api.Target）
func (ctx *PackageContext) Target(name string) *Target   // 创建 Target，与项目 OnBuild 风格一致
func (ctx *PackageContext) Build(t *Target) error        // 执行编译，产物输出到 BuildDir
func (ctx *PackageContext) GetTargets() map[string]*Target
```

### Toolchain

```go
// pkg/api/toolchain.go

type Toolchain struct {
    Target    string  // 交叉编译目标，如 "aarch64-linux-gnu"，为空表示本地编译
    CC        string  // "gcc" | "clang" | "cl.exe"
    CXX       string  // "g++" | "clang++" | "cl.exe"
    LD        string  // "ld" | "lld" | "link.exe"
    AR        string  // "ar" | "llvm-ar" | "lib.exe"
    CFlags    string  // "-O2 -g"
    CXXFlags  string  // "-O2 -g"
    LDFlags   string  // "-L/path/to/lib"
    SysRoot   string  // 交叉编译 sysroot
}

// Env 返回标准化的编译器环境变量
func (t *Toolchain) Env() map[string]string {
    env := map[string]string{
        "CC":      t.CC,
        "CXX":     t.CXX,
        "LD":      t.LD,
        "AR":      t.AR,
        "CFLAGS":  t.CFlags,
        "CXXFLAGS": t.CXXFlags,
        "LDFLAGS": t.LDFlags,
    }
    if t.SysRoot != "" {
        env["SYSROOT"] = t.SysRoot
    }
    return env
}
```

### Builder 扩展

```go
// pkg/api/builder.go

type Builder struct {
    configFuncs  []ConfigFunc
    buildFuncs   []BuildFunc
    installFuncs []InstallFunc
    requireFuncs []RequireFunc  // 新增
}

func (b *Builder) OnRequire(fn RequireFunc)
func (b *Builder) GetRequireFuncs() []RequireFunc
```

### BuildContext 扩展

```go
// pkg/api/context.go

type BuildContext struct {
    // ...existing fields
    packageRefs []string  // 引用的包别名
}

func (ctx *BuildContext) AddPackages(packages ...string) *BuildContext

// Target 扩展
func (t *Target) AddPackages(packages ...string) *Target
```

---

## 配置存储

### 项目配置 (`.vmake/config.json`)

```json
{
  "version": "1",
  "global": {
    "toolchain": "gcc",
    "mode": "debug"
  },
  "packages": {
    "myproject": {
      "options": {
        "feature_x": true
      }
    }
  },
  "requires": {
    "official/zlib": {
      "version": "1.3.1",
      "options": {
        "shared": false,
        "minizip": false
      }
    },
    "official/openssl": {
      "version": "3.0.2",
      "options": {
        "shared": false
      }
    }
  }
}
```

**字段说明**：

| 字段 | 说明 |
|------|------|
| `requires` | 包配置映射 |
| `key` | 包名（`repo/name`） |
| `version` | 解析后的版本号 |
| `options` | 该包的配置选项值 |

**缓存查找**：根据 options 计算 hash，直接定位缓存目录，无需额外的索引文件。

---

## 执行流程

### vmake config 流程

```
┌─────────────────────────────────────────────────────────────────────┐
│ 1. 扫描项目 build.go → 编译 → 加载插件                               │
│                                                                     │
│ 2. 执行 Main() → 执行 OnRequire → 收集项目直接依赖                   │
│                                                                     │
│ 3. Resolver 递归解析依赖                                             │
│    ├─ 加载 zlib/package.go → 执行 Package()                         │
│    ├─ 执行 OnRequire → 收集包的依赖 → 无依赖                         │
│    ├─ 收集 zlib 的 Option() 定义                                    │
│    ├─ 加载 openssl/package.go → 执行 Package()                      │
│    ├─ 执行 OnRequire → 收集包的依赖 → 依赖 zlib                      │
│    ├─ zlib 已在列表中 → 跳过（全局唯一）                             │
│    ├─ 收集 openssl 的 Option() 定义                                 │
│    ├─ 构建完整 DAG                                                  │
│    └─ 拓扑排序 → [zlib, openssl]                                   │
│                                                                     │
│ 4. 执行项目 OnConfig() → 收集项目 options                            │
│                                                                     │
│ 5. 从 .vmake/config.json 加载已保存值                                │
│                                                                     │
│ 6. 渲染 TUI → 用户配置 → 保存                                        │
└─────────────────────────────────────────────────────────────────────┘
```

### vmake build 流程

```
┌─────────────────────────────────────────────────────────────────────┐
│ 1-4. 同 config 流程 (OnRequire → 解析依赖 → 收集 options)            │
│                                                                     │
│ 5. 从 .vmake/config.json 加载配置值                                  │
│                                                                     │
│ 6. Resolver 解析依赖树                                               │
│    ├─ Semver 匹配版本                                               │
│    ├─ 合并 options (配置值 + 默认值)                                 │
│    └─ 拓扑排序确定安装顺序                                           │
│                                                                     │
│ 7. Installer 按拓扑顺序安装包                                        │
│                                                                     │
│    安装 zlib:                                                       │
│    ├─ 检查缓存: packages/official/zlib/1.3.1/{hash}/install/       │
│    │   └─ 存在？跳过                                                 │
│    │                                                                 │
│    └─ 缓存缺失？执行安装：                                           │
│        ├─ EnsureSource: clone/fetch/checkout                        │
│        ├─ 加载 package.go 插件                                      │
│        ├─ 创建 PackageContext { deps: {} }                          │
│        └─ 执行 Build() 回调                                         │
│                                                                     │
│    安装 openssl:                                                    │
│    ├─ 检查缓存: packages/official/openssl/3.0.2/{hash}/install/    │
│    ├─ 缓存缺失？执行安装：                                           │
│    │   ├─ 创建 PackageContext { deps: {zlib: ...} }                │
│    │   │   └─ ctx.Dep("official/zlib") 返回已安装的 zlib           │
│    │   └─ 执行 Build() 回调                                         │
│    └─ 安装完成                                                       │
│                                                                     │
│ 8. 执行项目 OnBuild()                                                │
│                                                                     │
│ 9. 构建 Target                                                      │
│     ├─ Target.AddPackages("official/zlib", "official/openssl")     │
│     ├─ 解析包名 → 找到缓存路径                                       │
│     ├─ AddIncludes: ~/.vmake/.../install/include                   │
│     └─ AddLinks: ~/.vmake/.../install/lib/libxxx.a                 │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 源代码管理

### 设计理念

源代码与编译产物分离存储，实现：

- **版本共享**：一个 Git 仓库包含所有版本，切换版本只需 `git checkout`
- **快速切换**：无需重复下载，离线环境下也能切换已 fetch 的版本
- **独立清理**：清理编译缓存不影响源代码，重新编译更快

### 源代码目录结构

```
~/.vmake/sources/
└── {repo_name}/
    └── {package_name}/
        └── repo/                    # 完整的 Git 仓库
            ├── .git/                # Git 元数据（包含所有 tags）
            ├── CMakeLists.txt       # 源代码文件
            ├── src/
            └── ...
```

### 核心操作

| 操作 | 命令 | 说明 |
|------|------|------|
| 首次下载 | `git clone {url} sources/{repo}/{name}/repo` | 完整 clone |
| 更新源码 | `git fetch --all --tags` | 获取新 tags（静默失败支持离线） |
| 切换版本 | `git checkout --force {tag}` | 切换到指定 tag |
| 清理修改 | `git checkout --force {tag}` | 覆盖本地修改 |

### SourceManager API

```go
// pkg/repo/source.go

type SourceManager struct {
    sourcesDir string  // ~/.vmake/sources
}

// EnsureSource 确保源代码存在并切换到指定版本
// 返回源代码目录路径
func (m *SourceManager) EnsureSource(pkg *PackageDef, version string) (string, error) {
    // 路径: sources/{repo}/{name}/repo/
    repoDir := filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, "repo")
    
    if !exists(repoDir) {
        if err := git.Clone(pkg.GitURL, repoDir); err != nil {
            return "", err
        }
    }
    
    // 获取最新 tags（静默失败，离线时可继续）
    _ = git.FetchTags(repoDir)
    
    // 切换到指定版本
    tag := pkg.GetTag(version)
    if err := git.Checkout(repoDir, tag); err != nil {
        return "", err
    }
    
    return repoDir, nil
}

// UpdateSource 强制更新源代码（拉取最新 tags）
func (m *SourceManager) UpdateSource(pkg *PackageDef) error {
    repoDir := filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, "repo")
    
    if !exists(repoDir) {
        return git.Clone(pkg.GitURL, repoDir)
    }
    
    return git.FetchAndReset(repoDir)
}

// GetSourceDir 获取源代码目录
func (m *SourceManager) GetSourceDir(repo, name string) string {
    return filepath.Join(m.sourcesDir, repo, name, "repo")
}

// HasSource 检查源代码是否存在
func (m *SourceManager) HasSource(repo, name string) bool {
    repoDir := filepath.Join(m.sourcesDir, repo, name, "repo")
    return exists(repoDir) && exists(filepath.Join(repoDir, ".git"))
}

// CleanSource 清理源代码
func (m *SourceManager) CleanSource(repo, name string) error {
    return os.RemoveAll(filepath.Join(m.sourcesDir, repo, name))
}
```

### Git 操作封装

```go
// pkg/repo/git.go

package repo

import (
    "os/exec"
    "strings"
)

func Clone(url, dir string) error {
    cmd := exec.Command("git", "clone", url, dir)
    cmd.Stdout = nil
    cmd.Stderr = nil
    return cmd.Run()
}

func FetchTags(dir string) error {
    cmd := exec.Command("git", "fetch", "--all", "--tags")
    cmd.Dir = dir
    return cmd.Run()
}

func Checkout(dir, ref string) error {
    cmd := exec.Command("git", "checkout", "--force", ref)
    cmd.Dir = dir
    return cmd.Run()
}

func FetchAndReset(dir string) error {
    cmds := [][]string{
        {"git", "fetch", "--all", "--tags"},
        {"git", "reset", "--hard", "origin/HEAD"},
    }
    for _, args := range cmds {
        cmd := exec.Command(args[0], args[1:]...)
        cmd.Dir = dir
        if err := cmd.Run(); err != nil {
            return err
        }
    }
    return nil
}

func GetCurrentCommit(dir string) (string, error) {
    cmd := exec.Command("git", "rev-parse", "HEAD")
    cmd.Dir = dir
    output, err := cmd.Output()
    if err != nil {
        return "", err
    }
    return strings.TrimSpace(string(output)), nil
}

func GetCurrentTag(dir string) (string, error) {
    cmd := exec.Command("git", "describe", "--tags", "--exact-match")
    cmd.Dir = dir
    output, err := cmd.Output()
    if err != nil {
        return "", err
    }
    return strings.TrimSpace(string(output)), nil
}

func ListTags(dir string) ([]string, error) {
    cmd := exec.Command("git", "tag", "-l")
    cmd.Dir = dir
    output, err := cmd.Output()
    if err != nil {
        return nil, err
    }
    tags := strings.Split(strings.TrimSpace(string(output)), "\n")
    if len(tags) == 1 && tags[0] == "" {
        return nil, nil
    }
    return tags, nil
}
```

### 版本切换流程

```
┌─────────────────────────────────────────────────────────────────────┐
│                    版本切换流程                                       │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  用户请求: 需要 official/zlib 1.2.13                                  │
│                                                                     │
│  当前状态: sources/official/zlib/repo/ 当前在 v1.3.1                │
│                                                                     │
│  1. 检查源代码是否存在                                                │
│     sources/official/zlib/repo/.git 存在 → 是                       │
│                                                                     │
│  2. 尝试获取最新 tags (可选，失败不影响)                              │
│     git fetch --all --tags → 静默失败                               │
│                                                                     │
│  3. 切换到目标版本                                                    │
│     git checkout --force v1.2.13                                    │
│                                                                     │
│  4. 验证切换成功                                                      │
│     git describe --tags --exact-match → v1.2.13                     │
│                                                                     │
│  5. 返回源代码路径                                                    │
│     sources/official/zlib/repo/                                     │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 构建系统支持

### 支持的构建系统

| 构建系统 | 典型命令 | out-of-source | vmake 辅助方法 |
|---------|---------|--------------|---------------|
| **CMake** | `cmake -B build && cmake --build build` | 支持（推荐） | `CMakeConfigure`, `CMakeBuild`, `CMakeInstall` |
| **Autotools** | `autoreconf -fi && ./configure && make` | 支持（VPATH） | `Configure`, `Make` |
| **Makefile** | `make && make install` | 不支持 | `RunIn`, `Make` |
| **vmake Native** | `ctx.Target().Build()` | 支持 | `Target()`, `Build()` |

### 辅助方法设计

为了简化包定义，提供 CMake 和 Autotools 的高级辅助方法：

#### CMakeConfigure 自动处理的参数

```
-S {SourceDir}                      # 源代码目录
-B {BuildDir}                       # 编译目录
-DCMAKE_INSTALL_PREFIX={InstallDir} # 安装目录
-DCMAKE_C_COMPILER={CC}             # C 编译器
-DCMAKE_CXX_COMPILER={CXX}          # C++ 编译器
-DCMAKE_BUILD_TYPE=Release          # 默认 Release
-DCMAKE_SYSTEM_NAME=Linux           # Target 不为空时自动添加
-DCMAKE_C_COMPILER_TARGET={Target}  # Target 不为空时自动添加
-DCMAKE_CXX_COMPILER_TARGET={Target} # Target 不为空时自动添加
-DCMAKE_SYSROOT={SysRoot}           # SysRoot 不为空时自动添加
```

#### Configure 自动处理的参数

```
--prefix={InstallDir}               # 安装目录
--host={Target}                     # Target 不为空时自动添加
--build={currentHost}               # Target 不为空时自动添加
CC={CC} CXX={CXX} CFLAGS=...        # 通过环境变量自动传递
```

### 工作目录策略

**规则**：`Run()` 和辅助方法默认在 **BuildDir** 执行

```
操作                    工作目录        方法
────────────────────────────────────────────────
autoreconf -fi      →   SourceDir   →  RunIn(SourceDir, "autoreconf", "-fi")
cmake 配置          →   自动处理    →  CMakeConfigure("-DOPT=ON")
cmake 编译          →   BuildDir    →  CMakeBuild("-j4")
cmake 安装          →   BuildDir    →  CMakeInstall()
configure           →   BuildDir    →  Configure("--enable-xxx")
make                →   BuildDir    →  Make("-j4")
```

### CMake 包示例（简化版）

```go
func Package(p *api.Package) {
    p.SetGit("https://github.com/madler/zlib.git")
    p.AddVersion("1.3.1", "v1.3.1")
    
    p.Option("shared").
        SetType(api.OptionBool).
        SetDefault(false).
        SetDescription("Build shared library")
    
    p.Build(func(ctx *api.PackageContext) {
        // CMakeConfigure 自动处理: -S, -B, 编译器, 安装目录, 交叉编译等
        ctx.CMakeConfigure(
            "-DBUILD_SHARED_LIBS="+ctx.BoolStr("shared"),  // "ON" 或 "OFF"
        )
        ctx.CMakeBuild("-j4")
        ctx.CMakeInstall()
    })
}
```

**对比**：使用底层 `Run()` 的写法（更冗长，保留用于特殊需求）：

```go
p.Build(func(ctx *api.PackageContext) {
    cmakeArgs := []string{
        "-S", ctx.SourceDir(),
        "-B", ctx.BuildDir(),
        "-DCMAKE_INSTALL_PREFIX=" + ctx.InstallDir(),
        "-DCMAKE_C_COMPILER=" + ctx.CC(),
        "-DCMAKE_CXX_COMPILER=" + ctx.CXX(),
        "-DCMAKE_BUILD_TYPE=Release",
    }
    if ctx.Target() != "" {
        cmakeArgs = append(cmakeArgs,
            "-DCMAKE_SYSTEM_NAME=Linux",
            "-DCMAKE_C_COMPILER_TARGET="+ctx.Target())
    }
    if ctx.Bool("shared") {
        cmakeArgs = append(cmakeArgs, "-DBUILD_SHARED_LIBS=ON")
    }
    ctx.Run("cmake", cmakeArgs...)
    ctx.Run("cmake", "--build", ctx.BuildDir(), "-j4")
    ctx.Run("cmake", "--install", ctx.BuildDir())
})
```

### Autotools 包示例（简化版）

```go
func Package(p *api.Package) {
    p.SetGit("https://github.com/libexpat/libexpat.git")
    p.AddVersion("2.5.0", "R_2_5_0")
    
    p.Option("shared").
        SetType(api.OptionBool).
        SetDefault(true).
        SetDescription("Build shared library")
    
    p.Build(func(ctx *api.PackageContext) {
        // autoreconf 手动执行（不封装，因为参数可能变化）
        ctx.RunIn(ctx.SourceDir(), "autoreconf", "-fi")
        
        // Configure 自动处理: --prefix, --host, CC/CXX 环境变量, 交叉编译等
        if ctx.Bool("shared") {
            ctx.Configure()
        } else {
            ctx.Configure("--disable-shared", "--enable-static")
        }
        
        ctx.Make("-j4")
        ctx.Make("install")
    })
}
```

### Makefile 包示例

裸 Makefile 没有统一的辅助方法，使用底层 `RunIn()` 或 `Make()`：

```go
func Package(p *api.Package) {
    p.SetGit("https://github.com/json-c/json-c.git")
    p.AddVersion("0.17", "json-c-0.17-20230812")
    
    p.Build(func(ctx *api.PackageContext) {
        // 裸 Makefile 通常需要在源代码目录编译
        ctx.RunIn(ctx.SourceDir(), "make", 
            "CC="+ctx.CC(),
            "CFLAGS="+ctx.CFlags(),
            "-j4")
        
        // 复制产物
        ctx.CopyFile(ctx.SourceDir()+"/libjson-c.a", ctx.InstallDir()+"/lib/")
        ctx.CopyDir(ctx.SourceDir()+"/include/", ctx.InstallDir()+"/include/")
    })
}
```

### CMake 包示例（详细版）

```go
func Package(p *api.Package) {
    p.SetGit("https://github.com/madler/zlib.git")
    p.AddVersion("1.3.1", "v1.3.1")
    
    p.Option("shared").
        SetType(api.OptionBool).
        SetDefault(false).
        SetDescription("Build shared library")
    
    p.Build(func(ctx *api.PackageContext) {
        // CMake 配置（不依赖工作目录，用 -S -B 指定路径）
        cmakeArgs := []string{
            "-S", ctx.SourceDir(),
            "-B", ctx.BuildDir(),
            "-DCMAKE_INSTALL_PREFIX=" + ctx.InstallDir(),
            "-DCMAKE_C_COMPILER=" + ctx.CC(),
            "-DCMAKE_CXX_COMPILER=" + ctx.CXX(),
            "-DCMAKE_BUILD_TYPE=Release",
        }
        
        // 交叉编译支持
        if ctx.Target() != "" {
            cmakeArgs = append(cmakeArgs,
                "-DCMAKE_SYSTEM_NAME=Linux",
                "-DCMAKE_C_COMPILER_TARGET="+ctx.Target(),
                "-DCMAKE_CXX_COMPILER_TARGET="+ctx.Target())
        }
        if ctx.SysRoot() != "" {
            cmakeArgs = append(cmakeArgs,
                "-DCMAKE_SYSROOT="+ctx.SysRoot())
        }
        
        // 选项
        if ctx.Bool("shared") {
            cmakeArgs = append(cmakeArgs, "-DBUILD_SHARED_LIBS=ON")
        }
        
        ctx.Run("cmake", cmakeArgs...)
        
        // 编译
        ctx.Run("cmake", "--build", ctx.BuildDir(), "-j4")
        
        // 安装
        ctx.Run("cmake", "--install", ctx.BuildDir())
    })
}
```

### Autotools 包示例

```go
func Package(p *api.Package) {
    p.SetGit("https://github.com/libexpat/libexpat.git")
    p.AddVersion("2.5.0", "R_2_5_0")
    
    p.Option("shared").
        SetType(api.OptionBool).
        SetDefault(true).
        SetDescription("Build shared library")
    
    p.Build(func(ctx *api.PackageContext) {
        // autoreconf 必须在源代码目录执行
        ctx.RunIn(ctx.SourceDir(), "autoreconf", "-fi")
        
        // configure 参数
        configArgs := []string{
            "--prefix=" + ctx.InstallDir(),
        }
        
        if !ctx.Bool("shared") {
            configArgs = append(configArgs, "--disable-shared", "--enable-static")
        }
        
        // 交叉编译支持
        if ctx.Target() != "" {
            configArgs = append(configArgs,
                "--host="+ctx.Target(),
                "--build="+currentHost())
        }
        
        // 在 BuildDir 执行 configure（用绝对路径）
        // Autotools 支持 VPATH build，会自动在 SourceDir 查找源文件
        ctx.RunWithEnv(ctx.Env(), ctx.SourceDir()+"/configure", configArgs...)
        
        // 在 BuildDir 编译
        ctx.Run("make", "-j4")
        ctx.Run("make", "install")
    })
}
```

### Makefile 包示例

```go
func Package(p *api.Package) {
    p.SetGit("https://github.com/json-c/json-c.git")
    p.AddVersion("0.17", "json-c-0.17-20230812")
    
    p.Build(func(ctx *api.PackageContext) {
        // 裸 Makefile 通常需要在源代码目录编译
        ctx.RunIn(ctx.SourceDir(), "make", 
            "CC="+ctx.CC(),
            "CFLAGS="+ctx.CFlags(),
            "-j4")
        
        // 复制产物
        ctx.CopyFile(ctx.SourceDir()+"/libjson-c.a", ctx.InstallDir()+"/lib/")
        ctx.CopyDir(ctx.SourceDir()+"/include/", ctx.InstallDir()+"/include/")
    })
}
```

### vmake 原生构建

有些第三方包的构建系统无法整合到 vmake 中：
- 没有构建系统的小型库（只有源文件）
- 构建系统过于复杂或自定义
- 需要精细控制编译过程

这种情况下可以直接复用 vmake 的 Target API 进行编译。

#### 核心设计

**完全复用现有 `api.Target`**，通过 `PackageContext.Target()` 获取 Target 实例，与项目 `OnBuild` 风格完全一致。

```
┌─────────────────────────────────────────────────────────────────────┐
│                    vmake Native 构建流程                              │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  PackageContext                                                     │
│  ├─ SourceDir:  ~/.vmake/sources/{repo}/{name}/repo/               │
│  ├─ BuildDir:   ~/.vmake/packages/{repo}/{name}/{ver}/{hash}/build/│
│  └─ InstallDir: ~/.vmake/packages/{repo}/{name}/{ver}/{hash}/install/│
│                                                                     │
│  1. ctx.Target("lib").AddFiles(...).SetKind(...).AddIncludes(...)  │
│     └─ 返回 *api.Target（与项目 BuildContext.Target() 相同）        │
│                                                                     │
│  2. ctx.Build(target)                                               │
│     ├─ 使用 PackageContext 的 Toolchain 编译                        │
│     ├─ 产物输出到 BuildDir（如 libxxx.a）                           │
│     └─ 支持交叉编译（自动继承 Target/SysRoot）                      │
│                                                                     │
│  3. ctx.CopyFile/CopyDir                                            │
│     └─ 手动复制产物到 InstallDir                                    │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

#### PackageContext 扩展 API

```go
// vmake 原生构建
func (ctx *PackageContext) Target(name string) *api.Target  // 复用现有 Target
func (ctx *PackageContext) Build(t *api.Target) error       // 执行编译
func (ctx *PackageContext) GetTargets() map[string]*api.Target
```

#### 构建系统对比

| 构建系统 | 编译产物位置 | 安装命令 | 安装目标 |
|---------|-------------|---------|---------|
| CMake | BuildDir | `cmake --install` | InstallDir |
| Autotools | BuildDir | `make install` | InstallDir |
| Makefile | SourceDir/BuildDir | 手动复制 | InstallDir |
| **vmake Native** | BuildDir | `ctx.CopyXxx()` | InstallDir |

#### vmake 原生构建示例

```go
// tinylib/package.go - 一个没有构建系统的小型库
func Package(p *api.Package) {
    p.SetGit("https://github.com/example/tinylib.git")
    p.AddVersion("1.0.0", "v1.0.0")
    
    p.Option("shared").
        SetType(api.OptionBool).
        SetDefault(false).
        SetDescription("Build shared library")
    
    p.Build(func(ctx *api.PackageContext) {
        kind := api.TargetStatic
        if ctx.Bool("shared") {
            kind = api.TargetShared
        }
        
        // 定义 Target（与项目 OnBuild 风格完全一致）
        t := ctx.Target("tinylib").
            SetKind(kind).
            AddFiles(ctx.SourceDir() + "/src/*.c").
            AddIncludes(ctx.SourceDir() + "/include").
            AddDefines("NDEBUG")
        
        // 执行编译，产物输出到 BuildDir
        ctx.Build(t)
        
        // 手动复制到 InstallDir
        ctx.MkdirAll(ctx.InstallDir() + "/lib")
        ctx.MkdirAll(ctx.InstallDir() + "/include")
        ctx.CopyDir(ctx.SourceDir() + "/include/", ctx.InstallDir() + "/include/")
        
        if ctx.Bool("shared") {
            ctx.CopyFile(ctx.BuildDir() + "/libtinylib.so", ctx.InstallDir() + "/lib/")
        } else {
            ctx.CopyFile(ctx.BuildDir() + "/libtinylib.a", ctx.InstallDir() + "/lib/")
        }
    })
}
```

#### 多 Target 示例

```go
func Package(p *api.Package) {
    p.SetGit("https://github.com/example/complib.git")
    p.AddVersion("2.0.0", "v2.0.0")
    
    p.Option("with_tools").
        SetType(api.OptionBool).
        SetDefault(false).
        SetDescription("Build command-line tools")
    
    p.Build(func(ctx *api.PackageContext) {
        // 构建主库
        lib := ctx.Target("complib").
            SetKind(api.TargetStatic).
            AddFiles(ctx.SourceDir() + "/lib/*.c").
            AddIncludes(ctx.SourceDir() + "/include")
        ctx.Build(lib)
        
        // 可选：构建工具
        if ctx.Bool("with_tools") {
            tool := ctx.Target("complib-tool").
                SetKind(api.TargetBinary).
                AddFiles(ctx.SourceDir() + "/tools/*.c").
                AddIncludes(ctx.SourceDir() + "/include").
                AddLinkDirs(ctx.BuildDir()).
                AddLinks("complib")
            ctx.Build(tool)
            
            ctx.MkdirAll(ctx.InstallDir() + "/bin")
            ctx.CopyFile(ctx.BuildDir() + "/complib-tool", ctx.InstallDir() + "/bin/")
        }
        
        // 复制头文件和库
        ctx.MkdirAll(ctx.InstallDir() + "/lib")
        ctx.MkdirAll(ctx.InstallDir() + "/include")
        ctx.CopyDir(ctx.SourceDir() + "/include/", ctx.InstallDir() + "/include/")
        ctx.CopyFile(ctx.BuildDir() + "/libcomplib.a", ctx.InstallDir() + "/lib/")
    })
}
```

#### 实现架构

```
pkg/api/package.go
├─ PackageContext.Target(name) *api.Target    // 复用现有 Target 类型
├─ PackageContext.Build(t *api.Target) error  // 调用注入的 buildFunc
└─ PackageContext.buildFunc func(*Target) error

pkg/build/native_builder.go（内部实现）
├─ NativeBuilder { compiler, linker, toolchain, sourceDir, buildDir, origDir }
├─ NewNativeBuilder(tc, sourceDir, buildDir)
├─ Build(t *api.Target) error
│   ├─ chdir(sourceDir) + defer chdir(origDir)  // 与 Scheduler 一致
│   ├─ glob.Match() 匹配源文件（相对路径）
│   ├─ compiler.Compile() 并行编译
│   └─ linker.LinkXxx() 链接（产物输出到 buildDir）
└─ **不依赖 Scheduler**（无需依赖调度、无 BuildCache）

pkg/repo/installer.go
└─ 创建 NativeBuilder，注入 buildFunc 到 PackageContext
```

**NativeBuilder vs Scheduler 对比**：

| 特性 | Scheduler（项目构建） | NativeBuilder（包构建） |
|------|---------------------|------------------------|
| 依赖调度 | 需要（跨 Target 拓扑排序） | 不需要（简单场景） |
| 增量编译 | BuildCache | 不需要（hash 目录隔离） |
| 工作目录 | chdir 到 projectDir | chdir 到 sourceDir |
| build 输出 | `projectDir/build/{tc}-{mode}/` | `buildDir`（绝对路径） |
| compile_commands.json | 生成 | 不生成 |
| 核心逻辑 | BuildGraph + 并行编译 | 直接并行编译 |

**输出路径差异**：

```go
// Scheduler（项目构建）
chdir(projectDir)  // /home/user/myproject/
output := "build/gcc-debug/libfoo.a"  // 相对路径，源代码与 build 在同一目录

// NativeBuilder（包构建）
chdir(sourceDir)   // ~/.vmake/sources/official/zlib/repo/
output := buildDir + "/libfoo.a"       // 绝对路径，指向 ~/.vmake/packages/official/zlib/1.3.1/{hash}/build/
```

### 源代码污染与清理

Autotools 的 `autoreconf -fi` 会在源代码目录生成文件，污染 Git 仓库。

**处理策略**：

1. **接受污染**：源代码目录可能包含生成的 `configure`、`config.h.in` 等文件
2. **支持清理**：提供 `make distclean` 等命令清理生成的文件
3. **极端情况**：`vmake pkg clean-source` 完全删除源代码目录，重新 clone

```bash
# 清理 autotools 生成的文件
vmake pkg distclean official/libexpat

# 完全重新 clone 源代码
vmake pkg clean-source official/libexpat
vmake pkg update official/libexpat
```

**SourceManager 扩展**：

```go
// DistClean 清理源代码目录中的构建系统生成文件
func (m *SourceManager) DistClean(repo, name string) error {
    repoDir := filepath.Join(m.sourcesDir, repo, name, "repo")
    
    // 尝试 make distclean
    cmd := exec.Command("make", "distclean")
    cmd.Dir = repoDir
    _ = cmd.Run()  // 忽略错误，可能没有 Makefile
    
    // 也尝试其他清理命令
    for _, cleanCmd := range [][]string{
        {"make", "maintainer-clean"},
        {"git", "clean", "-fdX"},  // 清理 gitignore 中的文件
    } {
        cmd := exec.Command(cleanCmd[0], cleanCmd[1:]...)
        cmd.Dir = repoDir
        _ = cmd.Run()
    }
    
    return nil
}
```

### 缓存 Hash 计算

```go
// pkg/repo/cache.go

// CacheHash 计算缓存目录名，包含全局参数和包 options
func CacheHash(toolchain, mode string, options map[string]any) string {
    // 1. 排序 options keys
    keys := slices.Sorted(maps.Keys(options))
    
    // 2. 拼接为 "toolchain-mode-key1-value1_key2-value2" 格式
    var parts []string
    parts = append(parts, toolchain, mode)
    for _, k := range keys {
        parts = append(parts, fmt.Sprintf("%s-%v", k, options[k]))
    }
    key := strings.Join(parts, "_")
    
    // 3. base64url 编码（不截断，保持完整可逆）
    return base64.URLEncoding.WithPadding(base64.NoPadding).
        EncodeToString([]byte(key))
}

// ParseCacheHash 从 base64url 字符串还原缓存参数
func ParseCacheHash(hash string) (toolchain, mode string, options map[string]any, err error) {
    decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).
        DecodeString(hash)
    if err != nil {
        return "", "", nil, fmt.Errorf("invalid cache hash: %w", err)
    }
    
    parts := strings.Split(string(decoded), "_")
    if len(parts) < 2 {
        return "", "", nil, fmt.Errorf("invalid cache hash format")
    }
    
    toolchain = parts[0]
    mode = parts[1]
    options = make(map[string]any)
    
    for _, part := range parts[2:] {
        kv := strings.SplitN(part, "-", 2)
        if len(kv) == 2 {
            options[kv[0]] = parseValue(kv[1])
        }
    }
    return toolchain, mode, options, nil
}
```

**编码示例**：

| toolchain | mode | options | 编码结果 |
|-----------|------|---------|---------|
| gcc | debug | `{shared: false}` | `Z2NjX2RlYnVnX3NoYXJlZC1mYWxzZQ` |
| gcc | release | `{shared: false}` | `Z2NjX3JlbGVhc2Vfc2hhcmVkLWZhbHNl` |
| gcc | debug | `{shared: true}` | `Z2NjX2RlYnVnX3NoYXJlZC10cnVl` |
| aarch64-linux-gnu | debug | `{shared: false}` | `YWFyY2g2NC1saW51eC1nbnVfZGVidWdfc2hhcmVkLWZhbHNl` |

### 缓存示例

```
~/.vmake/
├── sources/                           # 源代码缓存
│   └── official/                      # repo 名称
│       └── zlib/
│           └── repo/                  # Git 仓库（所有版本共享）
│               ├── .git/
│               └── ...
│
└── packages/                          # 编译产物缓存
    └── official/                      # repo 名称
        └── zlib/
            └── 1.3.1/
                ├── Z2NjX2RlYnVnX3NoYXJlZC1mYWxzZQ/  # gcc + debug + {shared: false}
                │   ├── build/
                │   └── install/
                │       ├── include/zlib.h
                │       └── lib/libz.a
                ├── Z2NjX3JlbGVhc2Vfc2hhcmVkLWZhbHNl/  # gcc + release + {shared: false}
                │   └── install/
                │       └── lib/libz.a
                ├── Z2NjX2RlYnVnX3NoYXJlZC10cnVl/    # gcc + debug + {shared: true}
                │   └── install/
                │       └── lib/libz.so
                └── YWFyY2g2NF9kZWJ1Z19zaGFyZWQtZmFsc2U/  # aarch64 + debug + {shared: false}
                    └── install/
                        └── lib/libz.a
```

**还原验证**：

```go
hash := "Z2NjX2RlYnVnX3NoYXJlZC1mYWxzZQ"
toolchain, mode, options, _ := ParseCacheHash(hash)
// toolchain = "gcc"
// mode = "debug"
// options = map[string]any{"shared": false}
```

---

## CLI 命令

### Repo 管理

```bash
vmake repo add <name> <git-url>     # 添加 repo
vmake repo remove <name>            # 移除 repo
vmake repo list                     # 列出所有 repo
vmake repo update [<name>]          # 更新 repo (git pull)
```

**示例**：

```bash
# 添加官方 repo（首次运行自动添加）
vmake repo add official https://github.com/vmake/official-repo

# 添加自定义 repo
vmake repo add my-repo https://github.com/myorg/vmake-repo

# 列出所有 repo
vmake repo list

# 更新 repo
vmake repo update official
```

### 包管理

```bash
vmake pkg install <repo/name> [version]  # 手动安装包
vmake pkg list                            # 列出已安装包
vmake pkg search <pattern>                # 搜索包
vmake pkg info <repo/name>                # 显示包信息

# 源代码管理
vmake pkg update <repo/name>              # 更新源代码 (git fetch --all --tags)
vmake pkg distclean <repo/name>           # 清理源代码中的构建产物 (make distclean)
vmake pkg clean-source <repo/name>        # 清理源代码缓存（保留编译产物）
vmake pkg clean-build <repo/name>         # 清理编译缓存（保留源代码）
vmake pkg clean <repo/name>               # 清理全部（源代码 + 编译缓存）
vmake pkg clean-all                       # 清理所有包的全部缓存
```

**示例**：

```bash
# 更新 zlib 源代码（获取最新 tags）
vmake pkg update official/zlib

# 清理 autotools 生成的文件（configure, config.h.in 等）
vmake pkg distclean official/libexpat

# 只清理编译产物，保留源代码（重新编译更快）
vmake pkg clean-build official/zlib

# 只清理源代码，保留编译产物
vmake pkg clean-source official/zlib

# 完全清理 zlib（源代码 + 所有版本的编译产物）
vmake pkg clean official/zlib

# 清理所有包的所有缓存
vmake pkg clean-all
```

### 配置

```bash
vmake config                        # TUI 配置（含项目 options + 包 options）
```

---

## 包定义格式

### 目录结构

```
~/.vmake/repos/official/
└── packages/
    └── z/
        └── zlib/
            └── package.go
```

### package.go 示例（CMake）

```go
// zlib/package.go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Package(p *api.Package) {
    p.SetHomepage("http://www.zlib.net").
        SetDescription("Compression library").
        SetLicense("zlib").
        SetGit("https://github.com/madler/zlib.git")
    
    p.AddVersion("1.3.1", "v1.3.1")
    p.AddVersion("1.2.13", "v1.2.13")
    
    p.Option("shared").
        SetType(api.OptionBool).
        SetDefault(false).
        SetDescription("Build shared library")
    
    p.Option("minizip").
        SetType(api.OptionBool).
        SetDefault(false).
        SetDescription("Build minizip")
    
    p.Build(func(ctx *api.PackageContext) {
        ctx.CMakeConfigure(
            "-DBUILD_SHARED_LIBS="+ctx.BoolStr("shared"),
            "-DENABLE_MINIZIP="+ctx.BoolStr("minizip"),
        )
        ctx.CMakeBuild("-j4")
        ctx.CMakeInstall()
    })
}
```

### package.go 示例（带依赖）

```go
// openssl/package.go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Package(p *api.Package) {
    p.SetGit("https://github.com/openssl/openssl.git")
    
    // 声明对 zlib 的依赖
    p.OnRequire(func(ctx *api.PackageRequireContext) {
        ctx.AddRequires("official/zlib >=1.2.0")
    })
    
    p.Option("shared").
        SetType(api.OptionBool).
        SetDefault(false).
        SetDescription("Build shared library")
    
    p.Build(func(ctx *api.PackageContext) {
        // 获取已安装的依赖
        zlib := ctx.Dep("official/zlib")
        
        ctx.CMakeConfigure(
            "-DOPENSSL_USE_STATIC_LIB=ON",
            "-DZLIB_ROOT="+zlib.InstallDir,
            "-DBUILD_SHARED_LIBS="+ctx.BoolStr("shared"),
        )
        ctx.CMakeBuild("-j4")
        ctx.CMakeInstall()
    })
}
```

### package.go 示例（Autotools）

```go
// expat/package.go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Package(p *api.Package) {
    p.SetHomepage("https://libexpat.github.io/").
        SetDescription("XML parser library").
        SetLicense("MIT").
        SetGit("https://github.com/libexpat/libexpat.git")
    
    p.AddVersion("2.5.0", "R_2_5_0")
    p.AddVersion("2.4.9", "R_2_4_9")
    
    p.Option("shared").
        SetType(api.OptionBool).
        SetDefault(true).
        SetDescription("Build shared library")
    
    p.Option("xml-attr-info").
        SetType(api.OptionBool).
        SetDefault(false).
        SetDescription("Enable XML_GetAttributeInfo")
    
    p.Build(func(ctx *api.PackageContext) {
        ctx.RunIn(ctx.SourceDir(), "autoreconf", "-fi")
        
        args := []string{"--without-docbook"}
        if !ctx.Bool("shared") {
            args = append(args, "--disable-shared", "--enable-static")
        }
        if ctx.Bool("xml-attr-info") {
            args = append(args, "--enable-xml-attr-info")
        }
        ctx.Configure(args...)
        
        ctx.Make("-j4")
        ctx.Make("install")
    })
}
```

### 依赖传递示例

```
项目 build.go
  └─ OnRequire: curl, openssl
        │
        ▼
curl/package.go
  └─ OnRequire: openssl, zlib
        │
        ▼
openssl/package.go
  └─ OnRequire: zlib
        │
        ▼
zlib/package.go
  └─ 无依赖

依赖解析后（拓扑排序）:
  1. zlib     → deps: {}
  2. openssl  → deps: {zlib}
  3. curl     → deps: {zlib, openssl}
```

### 包定义 API 对照

| xmake | vmake | 说明 |
|-------|-------|------|
| `set_homepage` | `SetHomepage` | 主页 URL |
| `set_description` | `SetDescription` | 描述 |
| `set_license` | `SetLicense` | 许可证 |
| `add_urls` | `SetGit` | Git 源（简化为仅支持 Git） |
| `add_versions` | `AddVersion` | 版本映射（version, ref） |
| `add_configs` | `Option().SetType().SetDefault()` | 配置选项（链式调用） |
| `add_deps` | `OnRequire` + `AddRequires` | 包依赖 |
| `on_install` | `Build` | 构建逻辑 |

---

## 使用示例

### 项目依赖声明

```go
// build.go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(b *api.Builder) {
    // 声明依赖（无条件）
    b.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires(
            "official/zlib >=1.2.0",
            "official/tbox ~1.6",
            "official/openssl >=3.0",
        )
    })
    
    // 项目配置
    b.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("enable_ssl").
            SetType(api.OptionBool).
            SetDefault(true).
            SetDescription("Enable SSL support")
    })
    
    // 构建配置
    b.OnBuild(func(ctx *api.BuildContext) {
        if ctx.Bool("enable_ssl") {
            ctx.Target("app").
                SetKind(api.TargetBinary).
                AddFiles("src/*.c").
                AddPackages("official/zlib", "official/openssl").
                AddLinks("m", "pthread")  // 系统库
        } else {
            ctx.Target("app").
                SetKind(api.TargetBinary).
                AddFiles("src/*.c").
                AddPackages("official/zlib")
        }
    })
}
```

### 包依赖传递

```go
// packages/official/curl/package.go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Package(p *api.Package) {
    p.SetGit("https://github.com/curl/curl.git")
    
    // 声明依赖
    p.OnRequire(func(ctx *api.PackageRequireContext) {
        ctx.AddRequires(
            "official/openssl >=3.0",
            "official/zlib >=1.2.0",
        )
    })
    
    p.Option("shared").
        SetType(api.OptionBool).
        SetDefault(false).
        SetDescription("Build shared library")
    
    p.Build(func(ctx *api.PackageContext) {
        // 获取已安装的依赖
        openssl := ctx.Dep("official/openssl")
        zlib := ctx.Dep("official/zlib")
        
        ctx.CMakeConfigure(
            "-DCMAKE_PREFIX_PATH="+openssl.InstallDir+";"+zlib.InstallDir,
            "-DCURL_USE_OPENSSL=ON",
            "-DZLIB_ROOT="+zlib.InstallDir,
            "-DBUILD_SHARED_LIBS="+ctx.BoolStr("shared"),
        )
        ctx.CMakeBuild("-j4")
        ctx.CMakeInstall()
    })
}
```

### TUI 配置界面

```
┌─────────────────────────────────────────────────────────────────────┐
│ vmake config                                                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│ ▸ Global                                                            │
│   Mode                 [debug]                                      │
│   Toolchain            [gcc]                                        │
│                                                                     │
│ ▸ myproject                                                         │
│   enable_ssl          [✓] Enable SSL support                       │
│                                                                     │
│ ▸ official/zlib                                                     │
│   shared              [ ] Build shared library                     │
│   minizip             [ ] Build minizip                            │
│                                                                     │
│ ▸ official/openssl                                                  │
│   shared              [ ] Build shared library                     │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 实现计划

### 阶段 1: 核心 API（预计文件：5 个）

| 文件 | 内容 |
|------|------|
| `pkg/api/require.go` | RequireContext, RequireFunc, RequireInfo, PackageRequireContext |
| `pkg/api/package.go` | Package, PackageContext, InstalledPackage |
| `pkg/api/toolchain.go` | Toolchain 类型，编译器信息 |
| `pkg/api/builder.go` | 添加 requireFuncs, OnRequire |
| `pkg/api/context.go` | BuildContext.AddPackages, Target.AddPackages |

### 阶段 2: Repo 管理（预计文件：5 个）

| 文件 | 内容 |
|------|------|
| `pkg/repo/manager.go` | Repo 管理（目录扫描查找包） |
| `pkg/repo/resolver.go` | 递归依赖解析, 循环检测, 拓扑排序, semver 匹配 |
| `pkg/repo/cache.go` | 缓存管理, CacheHash（含 toolchain+mode+options） |
| `pkg/repo/source.go` | 源代码管理, SourceManager, DistClean |
| `pkg/repo/git.go` | Git 操作封装 |

### 阶段 3: 包安装（预计文件：4 个）

| 文件 | 内容 |
|------|------|
| `pkg/repo/installer.go` | 包安装，注入 deps 到 PackageContext |
| `pkg/repo/package_loader.go` | 加载 package.go 插件，执行 OnRequire/Build |
| `pkg/repo/semver.go` | Semver 版本匹配 |
| `pkg/build/native_builder.go` | NativeBuilder，复用 Compiler/Linker 实现原生构建 |

### 阶段 4: CLI 命令（预计文件：2 个）

| 文件 | 内容 |
|------|------|
| `cmd/vmake/repo_cmd.go` | repo 子命令 |
| `cmd/vmake/pkg_cmd.go` | pkg 子命令（含源代码管理、distclean） |

### 阶段 5: 构建集成（预计修改：4 个）

| 文件 | 修改 |
|------|------|
| `pkg/plugin/loader.go` | 执行 OnRequire，收集顶层依赖 |
| `pkg/config/store.go` | 添加 requires 字段支持 |
| `pkg/tui/model.go` | 支持包 options 分组显示 |
| `pkg/build/scheduler.go` | 解析 Target.packages, 注入 includes/links |

### 阶段 6: 官方 Repo（预计文件：N 个）

- 创建 `vmake-repo` 仓库
- 添加常用包定义（zlib, openssl, curl 等）
- 文档更新

---

## 版本语法

支持 Semver 范围语法：

| 语法 | 说明 | 示例 |
|------|------|------|
| `1.2.3` | 精确版本 | `1.2.3` |
| `>=1.2.0` | 最小版本 | `>=1.2.0` |
| `>1.2.0` | 大于 | `>1.2.0` |
| `<=1.2.0` | 小于等于 | `<=1.2.0` |
| `<1.2.0` | 小于 | `<1.2.0` |
| `~1.2.0` | 兼容版本（>=1.2.0, <1.3.0） | `~1.2.0` |
| `^1.2.0` | 主版本兼容（>=1.2.0, <2.0.0） | `^1.2.0` |
| `1.2.x` | 通配符 | `1.2.x` |
| `*` | 任意版本 | `*` |

---

## 依赖解析流程

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Resolver.Resolve()                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  输入: 顶层 requires (来自 build.go OnRequire)                       │
│  requires = [                                                       │
│    {Name: "official/zlib", Version: ">=1.2.0"},                    │
│    {Name: "official/openssl", Version: "~3.0"}                     │
│  ]                                                                  │
│                                                                     │
│  1. 递归解析依赖树                                                   │
│     │                                                               │
│     ▼                                                               │
│  resolveRecursive(req, graph, path):                                │
│    ├─ 循环检测: path 中已有此包？报错                                │
│    ├─ 已解析过？跳过                                                 │
│    ├─ 加载 package.go 插件                                          │
│    ├─ 执行 OnRequire() 收集子依赖                                   │
│    ├─ Semver 匹配选择版本                                           │
│    ├─ 添加到 graph                                                  │
│    └─ 递归解析子依赖                                                 │
│     │                                                               │
│     ▼                                                               │
│  2. 构建完整 DAG                                                     │
│     │                                                               │
│     │  项目 → openssl → zlib                                        │
│     │         ↘ zlib (已存在，复用)                                 │
│     │                                                               │
│     ▼                                                               │
│  3. 拓扑排序                                                        │
│     │                                                               │
│     │  [zlib, openssl]                                              │
│     │                                                               │
│     ▼                                                               │
│  输出: DependencyGraph                                              │
│    order: ["official/zlib", "official/openssl"]                    │
│    packages: {                                                      │
│      "official/zlib": {version: "1.3.1", options: {...}},          │
│      "official/openssl": {version: "3.0.2", options: {...}}        │
│    }                                                                │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Resolver API

```go
// pkg/repo/resolver.go

type DependencyGraph struct {
    Order    []string                    // 拓扑排序后的包名列表
    Packages map[string]*ResolvedPackage // key: "repo/name"
}

type ResolvedPackage struct {
    Name       string
    Version    string
    Options    map[string]any
    Definition *api.Package
    Deps       []string  // 依赖的包名
}

type Resolver struct {
    reposDir string  // ~/.vmake/repos
}

func (r *Resolver) Resolve(initial []api.RequireInfo) (*DependencyGraph, error) {
    graph := &DependencyGraph{
        Order:    []string{},
        Packages: make(map[string]*ResolvedPackage),
    }
    
    // 递归解析
    for _, req := range initial {
        if err := r.resolveRecursive(req, graph, nil); err != nil {
            return nil, err
        }
    }
    
    // 拓扑排序
    graph.Order = r.topologicalSort(graph)
    return graph, nil
}

func (r *Resolver) resolveRecursive(req api.RequireInfo, graph *DependencyGraph, path []string) error {
    name := req.Name
    
    // 循环检测
    for _, p := range path {
        if p == name {
            return fmt.Errorf("circular dependency: %s → ... → %s", 
                strings.Join(path, " → "), name)
        }
    }
    
    // 已解析过，跳过
    if _, exists := graph.Packages[name]; exists {
        return nil
    }
    
    // 加载 package.go 插件
    pkg, err := r.loadPackage(name)
    if err != nil {
        return err
    }
    
    // 执行 OnRequire 收集子依赖
    subDeps := []string{}
    if pkg.requireFunc != nil {
        ctx := &api.PackageRequireContext{}
        pkg.requireFunc(ctx)
        for _, subReq := range ctx.GetRequires() {
            subDeps = append(subDeps, subReq.Name)
            if err := r.resolveRecursive(subReq, graph, append(path, name)); err != nil {
                return err
            }
        }
    }
    
    // Semver 匹配版本
    version := r.matchVersion(pkg, req.Version)
    
    // 添加到 graph
    graph.Packages[name] = &ResolvedPackage{
        Name:       name,
        Version:    version,
        Definition: pkg,
        Deps:       subDeps,
    }
    
    return nil
}
```

---

## 设计决策记录

### 为什么分离源代码缓存和编译缓存？

- **版本共享**：一个 Git 仓库包含所有版本的完整历史，切换版本只需 `git checkout`，无需重新下载
- **快速重建**：清理编译产物后，源代码仍然存在，重新编译只需几分钟而非重新下载
- **离线支持**：已 clone 的仓库可以离线切换版本
- **磁盘效率**：一个仓库 vs 多个版本的独立目录

### 为什么 sources/ 和 packages/ 路径包含 repo 名称？

- **避免冲突**：不同 repo 可能有同名包（official/zlib vs my-repo/zlib）
- **路径一致**：与 `AddRequires("official/zlib >=1.2.0")` 的命名一一对应
- **清理直观**：`vmake pkg clean official/zlib` 直接对应目录 `~/.vmake/packages/official/zlib/`
- **独立管理**：每个 repo 的源代码和编译产物完全隔离

### 为什么 Run() 默认在 BuildDir 执行？

- **大多数命令在 build 目录**：CMake 的 `--build`、Autotools 的 `make`、Meson 的 `compile`
- **Autotools 支持 VPATH build**：`configure` 可以在 build 目录用绝对路径调用
- **只有少数命令需要在 SourceDir**：`autoreconf` 等用 `RunIn(SourceDir)` 显式指定

### 为什么接受源代码污染？

- Autotools 等构建系统天然需要在源代码目录生成文件（configure, config.h.in 等）
- `make distclean` 可以清理大部分生成的文件
- 极端情况下可以 `vmake pkg clean-source` 重新 clone
- 相比复制源代码或 git worktree，实现更简单，磁盘占用更小

### 为什么使用环境变量传递编译器？

- **最通用**：所有构建系统都支持 `CC=gcc ./configure` 或 `make CC=gcc`
- **CMake 额外支持**：`-DCMAKE_C_COMPILER=gcc`
- **Autotools 通过环境变量**：`CC=arm-linux-gnueabihf-gcc ./configure --host=arm-linux-gnueabihf`
- **交叉编译友好**：环境变量 + `--host` 参数配合使用

### 为什么封装 CMake/Autotools 辅助方法？

- **减少重复代码**：每个 CMake 包都需要 -S, -B, -DCMAKE_INSTALL_PREFIX 等标准参数
- **自动处理交叉编译**：辅助方法自动判断 Target/SysRoot 并添加相应参数
- **保留底层控制**：对于特殊需求仍可使用 `Run()` 完全控制命令参数
- **不封装 autoreconf**：参数可能变化（-fi, -iv, -isrc），保持底层控制更灵活

### 为什么支持 vmake 原生构建？

- **无构建系统的库**：一些小型库只有源文件，没有 CMakeLists.txt 或 Makefile
- **构建系统过于复杂**：某些库的构建脚本难以用标准方法调用
- **精细控制**：需要逐个指定源文件、编译参数的场景
- **一致性**：包定义与项目 build.go 使用相同的 API，零学习成本
- **交叉编译保证**：自动继承 PackageContext 中的 Toolchain 设置

### 为什么 vmake 原生构建复用现有 Target API？

- **零学习成本**：与项目 `OnBuild` 中的 `ctx.Target()` 用法完全一致
- **代码复用**：无需定义新的 PackageTarget 类型，直接使用 `*api.Target`
- **功能完整**：Target 的所有方法（AddFiles, AddIncludes, AddDeps 等）都可用
- **实现简单**：通过函数注入解耦，NativeBuilder 复用 Compiler/Linker 逻辑

### 为什么 NativeBuilder 不使用 Scheduler？

- **无需依赖调度**：包内 Target 依赖关系简单，由包定义自己控制构建顺序
- **无需增量编译**：每个 hash 目录是独立缓存，重新构建即可
- **无需 compile_commands.json**：包是第三方依赖，IDE 支持不是必须的
- **实现更轻量**：只包含 Compiler + Linker + glob 匹配 + 并行编译
- **chdir 行为一致**：与 Scheduler 一样 chdir 到 sourceDir，glob 使用相对路径

### 为什么只用 Git 源？

- 简化实现，避免处理多种下载方式
- Git 是最通用的源码托管方式
- 可以精确定位到 commit/tag

### 为什么包 options 通过 TUI 配置？

- 统一用户体验，所有配置在一个界面完成
- 避免在 build.go 中硬编码配置
- 支持不同环境使用不同配置

### 为什么缓存 hash 包含 toolchain 和 mode？

- **编译器不同产物不同**：gcc 和 clang 编译的 .a/.so 不兼容
- **模式不同产物不同**：debug 和 release 的 ABI 可能不兼容
- **交叉编译隔离**：aarch64 交叉编译的产物不能用于 x86_64
- **可逆性**：从目录名可完全还原 `toolchain + mode + options`

### 为什么使用完整 base64url 编码缓存 key（不截断）？

- **可逆性**：从目录名可以完全还原参数值，便于调试和验证
- **避免冲突**：完整编码不会因截断导致不同参数映射到相同目录
- **明确性**：每个缓存目录都对应一组确定的参数，没有 "default" 这种隐含情况
- **无特殊处理**：所有缓存目录使用统一规则，代码更简洁

### 为什么删除别名机制？

- **简化心智模型**：一个包只有一种配置，不需要跟踪别名
- **避免配置冲突**：同一个包的不同配置可能导致 ABI 不兼容
- **简化 TUI**：用户不需要为每个别名单独配置
- **简化实现**：无需处理别名解析、缓存隔离等复杂逻辑

### 为什么 git fetch 失败时静默继续？

- 支持离线构建场景
- 已 clone 的仓库可以正常使用本地 tags
- 只有首次 clone 失败才报错

### 为什么项目和包使用不同的入口函数风格？

- **语义清晰**：`Main(b *api.Builder)` 表示项目主入口，`Package(p *api.Package)` 表示包定义
- **类型区分**：`Builder` 和 `Package` 是不同的类型，避免混淆
- **API 一致性**：两者都使用回调模式（OnRequire、Build 等）

### 为什么包使用 OnRequire 而非声明式 .Requires()？

- **与项目 API 一致**：build.go 和 package.go 都使用 OnRequire 回调
- **代码简洁**：无需额外的 DSL 方法链
- **灵活性**：可以在回调中添加逻辑（尽管推荐纯声明）

### 为什么包入口函数是 Package() 而非 Main()？

- **语义明确**：Package() 清楚地表示这是包定义，不是程序入口
- **避免混淆**：Main() 通常表示可执行程序的入口点
- **与项目区分**：项目的 build.go 用 Main()，包的 package.go 用 Package()

### 为什么通过 Dep() 获取已安装的依赖？

- **类型安全**：返回 InstalledPackage 结构体，包含完整路径信息
- **清晰的依赖关系**：在 OnRequire 中声明，在 Build 中使用
- **支持传递依赖**：Resolver 自动处理依赖的依赖，上层包只需关心直接依赖
