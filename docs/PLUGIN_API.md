# VMake 插件 API 参考

VMake 使用 Go 插件（`.so`）作为配置语言。项目和第三方包的 `build.go` 文件会被编译为 Go 插件，由 vmake 加载执行。

## 入口函数

### 项目插件

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) { ... })  // 声明依赖
    p.OnConfig(func(ctx *api.ConfigContext) { ... })     // 定义配置选项
    p.OnBuild(func(ctx *api.BuildContext) { ... })       // 定义构建目标
    p.OnInstall(func(ctx *api.InstallContext) { ... })   // 定义安装规则
}
```

### 第三方包插件

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnPackage(func(pkg *api.Package) {  // 包元信息
        pkg.SetGit("https://github.com/...").
            AddVersion("1.0.0", "v1.0.0")
    })

    p.OnConfig(func(ctx *api.ConfigContext) { ... })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("lib").
            SetKind(api.TargetVoid).
            SetBuildFunc(func(pkg *api.Package) error {
                pkg.CMakeConfigure()
                pkg.CMakeBuild("-j4")
                pkg.CMakeInstall()
                return nil
            })
    })
}
```

## Package（主类型）

Package 是插件的主入口类型，包含生命周期方法和元信息设置。

```go
type Package struct {
    // 嵌入 ConfigAccessor，提供 If/IfNot/Select/When 等条件表达式
    ConfigAccessor
}

// 生命周期方法（注册回调）
func (p *Package) OnRequire(fn RequireFunc)    // 声明第三方依赖
func (p *Package) OnConfig(fn ConfigFunc)      // 定义配置选项
func (p *Package) OnBuild(fn BuildFunc)        // 定义构建目标
func (p *Package) OnInstall(fn InstallFunc)    // 定义安装规则
func (p *Package) OnPackage(fn PackageFunc)    // 包元信息（仅第三方包）

// 元信息设置（第三方包）
func (p *Package) SetGit(urls ...string) *Package
func (p *Package) SetHomepage(url string) *Package
func (p *Package) SetDescription(desc string) *Package
func (p *Package) SetLicense(license string) *Package
func (p *Package) AddVersion(version, ref string) *Package
func (p *Package) SetSubmodules(v bool) *Package
func (p *Package) SetLibs(libs ...string) *Package

// 目标定义
func (p *Package) Target(name string) *Target
func (p *Package) GetTargets() map[string]*Target

// 包依赖
func (p *Package) AddPackages(packages ...string) *Package
func (p *Package) GetPackages() []string
func (p *Package) Deps() map[string]*InstalledPackage

// 目录信息
func (p *Package) SourceDir() string
func (p *Package) BuildDir() string
func (p *Package) InstallDir() string

// 编译器信息（代理 Toolchain）
func (p *Package) CC() string
func (p *Package) CXX() string
func (p *Package) AR() string
func (p *Package) CrossTarget() string
func (p *Package) SysRoot() string
func (p *Package) CFlags() string
func (p *Package) CXXFlags() string
func (p *Package) LDFlags() string
func (p *Package) Env() map[string]string

// 构建辅助方法
func (p *Package) CMakeConfigure(extraArgs ...string) error
func (p *Package) CMakeBuild(args ...string) error
func (p *Package) CMakeInstall() error
func (p *Package) Configure(extraArgs ...string) error
func (p *Package) Make(args ...string) error
func (p *Package) Run(name string, args ...string) error
func (p *Package) RunIn(dir, name string, args ...string) error
func (p *Package) RunWithEnv(env map[string]string, name string, args ...string) error

// 获取方法
func (p *Package) PackageName() string
func (p *Package) GetOptions() map[string]*Option
func (p *Package) Versions() map[string]string
```

## ConfigContext

配置阶段上下文，用于定义选项和读取配置值。

```go
// 选项定义
func (ctx *ConfigContext) Option(name string) *Option
func (ctx *ConfigContext) GlobalOption(name string) *Option  // 标记为全局选项
func (ctx *ConfigContext) GlobalMode() *Option                // 内置 mode 选项

// 读取值（优先配置值，其次默认值）
func (ctx *ConfigContext) Bool(name string) bool
func (ctx *ConfigContext) String(name string) string
func (ctx *ConfigContext) Int(name string) int

// 其他
func (ctx *ConfigContext) PackageName() string
func (ctx *ConfigContext) SetConfigValue(name string, val any)
func (ctx *ConfigContext) GetOptions() map[string]*Option
```

## Option

配置选项定义。

```go
// 设置方法（链式调用）
func (o *Option) SetType(t OptionType) *Option
func (o *Option) SetDefault(v any) *Option
func (o *Option) SetDescription(desc string) *Option
func (o *Option) SetValues(vals ...string) *Option        // OptionChoice 使用
func (o *Option) SetShowIf(fn func(ctx *ConfigContext) bool) *Option  // 条件显示
func (o *Option) SetGroup(group string) *Option
func (o *Option) SetGlobal() *Option                       // 标记为全局选项

// 获取方法
func (o *Option) Name() string
func (o *Option) Type() OptionType
func (o *Option) Default() any
func (o *Option) Description() string
func (o *Option) Values() []string
func (o *Option) Group() string
func (o *Option) ShowIf() func(ctx *ConfigContext) bool
func (o *Option) IsGlobal() bool
```

## BuildContext

构建阶段上下文，用于定义构建目标和条件表达式。

```go
// 目标定义
func (ctx *BuildContext) Target(name string) *Target
func (ctx *BuildContext) GetTargets() map[string]*Target

// 条件表达式（Package 内选项）
func (ctx *BuildContext) If(option string, then ...string) []string
func (ctx *BuildContext) IfNot(option string, then ...string) []string
func (ctx *BuildContext) Select(option string, mapping map[string]string) string
func (ctx *BuildContext) When(option string, value any) bool

// 读取 Package 内选项值
func (ctx *BuildContext) Bool(name string) bool
func (ctx *BuildContext) String(name string) string
func (ctx *BuildContext) Int(name string) int

// 全局选项
func (ctx *BuildContext) GlobalBool(name string) bool
func (ctx *BuildContext) GlobalString(name string) string
func (ctx *BuildContext) IfGlobal(option string, then ...string) []string
func (ctx *BuildContext) SelectGlobal(option string, mapping map[string]string) string
func (ctx *BuildContext) Mode() string

// 安装规则
func (ctx *BuildContext) AddInstalls(src, dest string) *BuildContext
func (ctx *BuildContext) SetInstallFilter(filter InstallFilterFunc) *BuildContext

// 包依赖
func (ctx *BuildContext) AddPackages(packages ...string) *BuildContext
func (ctx *BuildContext) GetPackages() []string

// 其他
func (ctx *BuildContext) PackageName() string
```

## InstallContext

安装阶段上下文。

```go
func (ctx *InstallContext) SetPrefix(prefix string)
func (ctx *InstallContext) Prefix() string
func (ctx *InstallContext) PrefixSet() bool
func (ctx *InstallContext) PackageName() string

func (ctx *InstallContext) AddInstalls(src, dest string)
func (ctx *InstallContext) SetInstallFilter(filter InstallFilterFunc)
func (ctx *InstallContext) GetInstallFilter() InstallFilterFunc

func (ctx *InstallContext) Bool(name string) bool
func (ctx *InstallContext) String(name string) string
func (ctx *InstallContext) GlobalBool(name string) bool
func (ctx *InstallContext) GlobalString(name string) string
func (ctx *InstallContext) Mode() string
```

## Target

构建目标定义。

```go
// 类型设置
func (t *Target) SetKind(kind TargetKind) *Target
func (t *Target) SetDefault(isDefault bool) *Target

// 源文件与头文件
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

// 包依赖
func (t *Target) AddPackages(packages ...string) *Target

// 第三方包构建
func (t *Target) SetBuildFunc(fn func(p *Package) error) *Target

// 安装控制
func (t *Target) SetInstallDir(dir string) *Target
func (t *Target) SetInstall(install bool) *Target

// 移除方法
func (t *Target) RemoveCFlags(flags ...string) *Target
func (t *Target) RemoveCxxFlags(flags ...string) *Target
func (t *Target) RemoveLdFlags(flags ...string) *Target
func (t *Target) RemoveDefines(defines ...string) *Target
func (t *Target) RemoveIncludes(dirs ...string) *Target
func (t *Target) RemovePublicIncludes(dirs ...string) *Target
func (t *Target) RemoveLinks(libs ...string) *Target
func (t *Target) RemoveDeps(targets ...string) *Target

// 获取方法
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
func (t *Target) InstallDir() string
func (t *Target) NoInstall() bool
func (t *Target) Packages() []string
```

`AddFiles/Includes/Defines/Links/CFlags/CxxFlags/LdFlags` 接受 `string` 或 `[]string`（条件表达式返回）。

## RequireContext（依赖声明）

```go
// 项目依赖声明
func (ctx *RequireContext) AddRequires(deps ...string)       // "official/zlib >=1.2"
func (ctx *RequireContext) GetRequires() []RequireInfo

// 包依赖声明（相同的 API）
func (ctx *PackageRequireContext) AddRequires(deps ...string)
func (ctx *PackageRequireContext) GetRequires() []RequireInfo
```

## 关键类型

```go
type TargetKind string
const (
    TargetBinary TargetKind = "binary"
    TargetStatic TargetKind = "static"
    TargetShared TargetKind = "shared"
    TargetObject TargetKind = "object"
    TargetVoid   TargetKind = "void"      // 第三方包使用，配合 SetBuildFunc
)

type OptionType int
const (
    OptionBool   OptionType = iota
    OptionString
    OptionInt
    OptionChoice
)

type Toolchain struct {
    Target    string   // 交叉编译目标
    CC        string
    CXX       string
    LD        string
    AR        string
    CFlags    string
    CXXFlags  string
    LDFlags   string
    SysRoot   string
}

type InstalledPackage struct {
    Name       string
    Version    string
    InstallDir string
    IncludeDir string
    LibDir     string
    BinDir     string
    Libs       []string
    Deps       []string
}

type InstallItem struct {
    Src  string
    Dest string
}
type InstallFilterFunc func(path string, isTargetOutput bool) bool
```

## 全局选项

内置全局选项：

```go
const (
    ModeOptionName      = "mode"
    ToolchainOptionName = "toolchain"
    ModeDebug           = "debug"
    ModeRelease         = "release"
)
```

`mode` 选项自动添加编译标志：

| mode | cflags | defines |
|------|--------|---------|
| debug | `-O0 -g` | 无 |
| release | `-O2` | `NDEBUG` |

`GetModeFlags(mode string) (cflags, defines []string)` 返回上述值。

用户定义全局选项：

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.GlobalOption("ssl").
        SetType(api.OptionBool).
        SetDefault(true).
        SetDescription("Enable SSL support")
})
```

全局选项在所有 Package 间共享。如果多个 Package 定义同名全局选项，类型和默认值必须一致。

## 使用示例

### 简单项目 (`test_data/01_simple_c`)

```go
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("hello").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c")
    })
}
```

### 多目标项目 (`test_data/03_multi_target`)

```go
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("mylib").
            SetKind(api.TargetStatic).
            AddFiles("src/mylib.c").
            AddIncludes("include")

        ctx.Target("myapp").
            SetKind(api.TargetBinary).
            AddFiles("src/main.c").
            AddIncludes("include").
            AddDeps("mylib")

        ctx.Target("tests").
            SetKind(api.TargetBinary).
            AddFiles("tests/*.c").
            AddDeps("mylib").
            SetDefault(false)  // 不默认构建
    })
}
```

### 多模块项目 (`test_data/04_multi_module`)

```
project/
├── build.go          # 定义全局选项
├── lib/build.go      # 库
└── app/build.go      # 应用（依赖库）
```

**build.go**:
```go
func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("feature_x").
            SetType(api.OptionBool).
            SetDefault(true).
            SetDescription("Enable feature X")
    })
}
```

**app/build.go**:
```go
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").
            SetKind(api.TargetBinary).
            AddFiles("*.c").
            AddDeps("lib:utils")  // 跨包依赖
    })
}
```

### 条件表达式 (`test_data/05_conditional`)

```go
func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("debug").
            SetType(api.OptionBool).
            SetDefault(false).
            SetDescription("Enable debug mode").
            SetGroup("General")

        ctx.Option("platform").
            SetType(api.OptionChoice).
            SetDefault("linux").
            SetValues("linux", "macos", "windows").
            SetDescription("Target platform").
            SetGroup("Platform")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c").
            AddDefines(ctx.If("debug", "DEBUG_MODE")).
            AddCFlags(ctx.If("debug", "-g", "-O0")).
            AddCFlags(ctx.IfNot("debug", "-O2")).
            AddCFlags(ctx.Select("platform", map[string]string{
                "linux":   "-DLINUX",
                "macos":   "-DMACOS",
                "windows": "-DWINDOWS",
            }))
    })
}
```

### 使用第三方包 (`test_data/08_with_package`)

```go
func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires("official/zlib >=1.2")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("zlib_test").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c").
            AddPackages("official/zlib")
    })
}
```

### 第三方包定义 (`official_repo/z/zlib`)

```go
func Main(p *api.Package) {
    p.OnPackage(func(pkg *api.Package) {
        pkg.SetHomepage("http://www.zlib.net").
            SetDescription("Compression library").
            SetLicense("zlib").
            SetGit(
                "https://gitee.com/mirrors/zlib.git",
                "https://github.com/madler/zlib.git",
            ).
            SetLibs("z").
            AddVersion("1.3.1", "v1.3.1").
            AddVersion("1.2.13", "v1.2.13")
    })

    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("shared").
            SetType(api.OptionBool).
            SetDefault(false).
            SetDescription("Build shared library")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("zlib").
            SetKind(api.TargetVoid).
            SetBuildFunc(func(pkg *api.Package) error {
                pkg.CMakeConfigure(
                    "-DBUILD_SHARED_LIBS=" + pkg.BoolStr("shared"),
                )
                pkg.CMakeBuild("-j4")
                pkg.CMakeInstall()
                return nil
            })
    })
}
```
