# VMake 构建脚本 API 参考

VMake 使用 [yaegi](https://github.com/traefik/yaegi) Go 解释器在运行时直接解释执行 `build.go` 文件，无需编译为插件。支持多文件：一个包目录下可以有多个 `.go` 文件，它们会被合并后一起解释执行。

## 入口函数

### 项目插件

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) { ... })  // 声明依赖
    p.OnConfig(func(ctx *api.ConfigContext) { ... })     // 定义配置选项
    p.OnBuild(func(ctx *api.BuildContext) { ... })       // 定义构建目标
    p.OnInstall(func(ctx *api.InstallContext) { ... })   // 定义安装规则
    p.OnClean(func(ctx *api.CleanContext) { ... })       // 定义清理规则
}
```

### 第三方包插件

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnPackage(func(pkg *api.Package) {  // 填充元数据
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
    PackageMeta
    ConfigAccessor
    *TargetRegistry
    *InstallItemHolder
    // ... 内部字段
}
```

### 生命周期方法（注册回调）

```go
func (p *Package) OnRequire(fn RequireFunc) *Package    // 声明第三方依赖
func (p *Package) OnConfig(fn ConfigFunc) *Package       // 定义配置选项
func (p *Package) OnBuild(fn BuildFunc) *Package          // 定义构建目标
func (p *Package) OnInstall(fn InstallFunc) *Package      // 定义安装规则
func (p *Package) OnClean(fn CleanFunc) *Package          // 定义清理规则（vmake clean 时执行）
func (p *Package) OnPackage(fn PackageFunc) *Package      // 填充包元数据（插件提取阶段执行）
```

### 元信息设置

```go
func (p *Package) SetGit(urls ...string) *Package        // Git 仓库 URL（仅 registry 包）
func (p *Package) SetHomepage(url string) *Package
func (p *Package) SetDescription(desc string) *Package
func (p *Package) SetLicense(license string) *Package
func (p *Package) AddVersion(version, ref string) *Package
func (p *Package) SetVersions(versions map[string]string) *Package
func (p *Package) SetSubmodules(v bool) *Package
func (p *Package) SetRepo(repo string) *Package          // 仓库名
func (p *Package) SetName(name string) *Package          // 包名
func (p *Package) SetScriptDir(dir string) *Package      // 构建脚本目录
func (p *Package) SetCfgVals(vals map[string]any) *Package  // 设置配置值
func (p *Package) SetGenConfigHeader(v bool) *Package    // 启用生成配置头文件
```

### Git Patch

```go
func (p *Package) AddPatches(paths ...string) *Package
func (p *Package) SetPatches(paths ...string) *Package
func (p *Package) GetPatches() []string
```

Patch 文件在源码下载后、构建前通过 `git apply --3way` 自动应用。已应用的 patch 会被跳过。

### 目标定义

```go
func (p *Package) Target(name string) *Target
func (p *Package) GetTargets() map[string]*Target
```

### 包依赖

```go
func (p *Package) Deps() map[string]*InstalledPackage
```

### 目录信息

```go
func (p *Package) SourceDir() string
func (p *Package) BuildDir() string
func (p *Package) InstallDir() string
func (p *Package) OutputDir() string
func (p *Package) ScriptDir() string
```

### 编译器信息（代理 Toolchain）

```go
func (p *Package) CC() string
func (p *Package) CXX() string
func (p *Package) AR() string
func (p *Package) CrossTarget() string
func (p *Package) Prefix() string
func (p *Package) CFlags() string
func (p *Package) CXXFlags() string
func (p *Package) LDFlags() string
func (p *Package) Env() map[string]string
```

### RTOS 工具访问器

```go
func (p *Package) ObjCopy() string
func (p *Package) Size() string
func (p *Package) ObjDump() string
func (p *Package) NM() string
```

### 依赖 Linker Script

```go
func (p *Package) SetProvidedLinkerScript(path string) *Package  // 声明 linker script（重复调用 vlog.Fatal）
func (p *Package) ProvidedLinkerScript() string
```

### 构建辅助方法

```go
func (p *Package) CMakeConfigure(extraArgs ...string) error
func (p *Package) CMakeBuild(args ...string) error
func (p *Package) CMakeInstall() error
func (p *Package) Configure(extraArgs ...string) error
func (p *Package) Make(args ...string) error
func (p *Package) Run(name string, args ...string) error
func (p *Package) RunIn(dir, name string, args ...string) error
func (p *Package) RunEnv(env map[string]string, name string, args ...string) error
```

### CMake 全局标志传递

将 `AddGlobalCFlags`/`AddGlobalCxxFlags`/`AddGlobalLdFlags` 设置的全局标志传递给 CMake 外部构建：

```go
func (p *Package) CMakeGlobalFlagsArgs() []string          // 返回 -DCMAKE_C_FLAGS=... 等 CMake 参数
func (p *Package) MergedCFlags(extra ...string) string      // 合并全局 C 标志 + extra，返回空格拼接字符串
func (p *Package) MergedCxxFlags(extra ...string) string    // 合并全局 C++ 标志 + extra
func (p *Package) MergedLdFlags(extra ...string) string     // 合并全局链接标志 + extra
```

使用示例：

```go
// 方式一：用 CMakeConfigure 便捷方法
p.CMakeConfigure(append(p.CMakeGlobalFlagsArgs(), "-DBUILD_SHARED_LIBS=OFF")...)

// 方式二：手动 cmake（如自定义 toolchain file 场景），追加自己的 cflags
args := []string{"-S", pkg.SrcDir(), "-B", pkg.BuildDir(), "-G", "Ninja",
    "--toolchain", tcPath,
    "-DCMAKE_C_FLAGS=" + pkg.MergedCFlags("-D__SINGLE_THREAD=ON"),
}
pkg.RunIn(pkg.BuildDir(), "cmake", args...)
```

### 获取方法

```go
func (p *Package) FullName() string                // 完整包名（repo/name 或 name）
func (p *Package) GetOptions() map[string]*Option
func (p *Package) Versions() map[string]string
func (p *Package) GenConfigHeader() bool           // 配置头文件是否启用
func (p *Package) SrcDirRaw() string               // 原始 srcCodeDir，无 SourceDir 回退（SetSrcDir 未调用时返回空串）
```

### Stamp 控制

```go
func (p *Package) SetConfigFiles(files ...string) *Package  // 配置文件列表，变更时使 stamp 失效
func (p *Package) ConfigFiles() []string
```

### 源码与输出目录

```go
func (p *Package) SetSrcDir(dir string) *Package    // 设置源码目录（与 SourceDir 不同，当 SetGit 下载源码时使用）
func (p *Package) SrcDir() string                    // 注意：当 SetGit 下载源码时，SrcDir 返回 <SourceDir>/src/，而非 SourceDir
func (p *Package) SetOutputDir(dir string) *Package
```

### DryRun

```go
func (p *Package) SetDryRun(v bool) *Package   // 设置 dry run 模式（只打印不执行）
func (p *Package) DryRun() bool
```

### 版本选择

```go
func (p *Package) GetRef(version string) string              // 根据 version 名获取 git ref
func (p *Package) GetVersions() []string                     // 获取所有可用版本（未排序）
func (p *Package) SelectVersion(constraint string) (string, error)  // 根据约束选择最佳匹配版本
func (p *Package) SelectVersionMulti(constraints []string) (string, error)  // 根据多约束选择最佳匹配版本
```

## ConfigAccessor（条件表达式与值读取）

`ConfigAccessor` 被嵌入到 `Package`、`ConfigContext`、`BuildContext`、`InstallContext`、`RequireContext` 中，提供选项值读取和条件表达式。

```go
// 值读取（优先配置值，其次默认值）
func (a *ConfigAccessor) Bool(name string) bool
func (a *ConfigAccessor) String(name string) string
func (a *ConfigAccessor) Int(name string) int
func (a *ConfigAccessor) BoolStr(name string) string          // "ON" / "OFF"

// 条件表达式
func (a *ConfigAccessor) If(option string, then ...string) []string
func (a *ConfigAccessor) IfNot(option string, then ...string) []string
func (a *ConfigAccessor) Equal(option, value, dep string) string
func (a *ConfigAccessor) Select(option string, mapping map[string]string) string
func (a *ConfigAccessor) When(option string, value any) bool

// 选项管理
func (a *ConfigAccessor) Option(name string) *Option
func (a *ConfigAccessor) SetOptions(options map[string]*Option) *ConfigAccessor
func (a *ConfigAccessor) MergeGlobals(globalOptions map[string]*Option, globalVals map[string]any)
```

## ConfigContext

配置阶段上下文，用于定义选项和读取配置值。

```go
type ConfigContext struct {
    ConfigAccessor
    // ...
}

// 选项定义
func (ctx *ConfigContext) Option(name string) *Option
func (ctx *ConfigContext) GlobalOption(name string) *Option  // 标记为全局选项
func (ctx *ConfigContext) GlobalMode() *Option                // 内置 mode 选项

// 读取值（继承自 ConfigAccessor）
func (ctx *ConfigContext) Bool(name string) bool
func (ctx *ConfigContext) String(name string) string
func (ctx *ConfigContext) Int(name string) int

// 其他
func (ctx *ConfigContext) PackageName() string
func (ctx *ConfigContext) SetConfigValue(name string, val any) *ConfigContext
func (ctx *ConfigContext) GetOptions() map[string]*Option
func (ctx *ConfigContext) Toolchains() []string
func (ctx *ConfigContext) ToolchainOption() *Option      // 创建工具链选择选项（自动填充可用工具链）

// 全局编译/链接标志（仅在 OnApply 回调中有效）
func (ctx *ConfigContext) AddGlobalCFlags(flags ...string)
func (ctx *ConfigContext) AddGlobalCxxFlags(flags ...string)
func (ctx *ConfigContext) AddGlobalLdFlags(flags ...string)
func (ctx *ConfigContext) AddGlobalLinks(links ...string)  // 添加全局链接库

// 依赖 Linker Script
func (ctx *ConfigContext) SetProvidedLinkerScript(path string) *ConfigContext
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
func (o *Option) SetOnApply(fn func(ctx *ConfigContext, val any)) *Option  // 选项值解析后的回调，val 为原始类型值
func (o *Option) SetGroup(group string) *Option
func (o *Option) IsGlobal() bool                      // 是否为全局选项（group == "Global"）

// 获取方法
func (o *Option) Name() string
func (o *Option) Type() OptionType
func (o *Option) Default() any
func (o *Option) Description() string
func (o *Option) Values() []string
func (o *Option) Group() string
func (o *Option) ShowIf() func(ctx *ConfigContext) bool
func (o *Option) OnApply() func(ctx *ConfigContext, val any)
func (o *Option) IsGlobal() bool
```

## BuildContext

构建阶段上下文，用于定义构建目标和条件表达式。

```go
type BuildContext struct {
    ConfigAccessor
    *TargetRegistry
    *InstallItemHolder
    // ...
}

// 目标定义
func (ctx *BuildContext) Target(name string) *Target
func (ctx *BuildContext) GetTargets() map[string]*Target
func (ctx *BuildContext) SetDefaultFlags(cflags, cxxflags, ldflags []string)  // 设置所有目标的默认编译/链接标志

// 条件表达式（继承自 ConfigAccessor）
func (ctx *BuildContext) If(option string, then ...string) []string
func (ctx *BuildContext) IfNot(option string, then ...string) []string
func (ctx *BuildContext) Select(option string, mapping map[string]string) string
func (ctx *BuildContext) When(option string, value any) bool

// 读取 Package 内选项值
func (ctx *BuildContext) Bool(name string) bool
func (ctx *BuildContext) String(name string) string
func (ctx *BuildContext) Int(name string) int

// 安装规则
func (ctx *BuildContext) AddInstalls(src, dest string) *InstallItemHolder
func (ctx *BuildContext) SetInstallFilter(filter InstallFilterFunc) *InstallItemHolder

// 子构建
func (ctx *BuildContext) BuildSubGraph(pkgName string)
func (ctx *BuildContext) DepOutput(depRef string) string
func (ctx *BuildContext) DepBuildDir(depRef string) string

// 配置导出
func (ctx *BuildContext) GenerateConfigHeader() *BuildContext
func (ctx *BuildContext) GenerateConfigDefines() *BuildContext
func (ctx *BuildContext) ExportConfig() *BuildContext
func (ctx *BuildContext) ImportConfig(pkgNames ...string) *BuildContext
func (ctx *BuildContext) SyncConfigDefines(pkgNames ...string) *BuildContext

// 其他
func (ctx *BuildContext) PackageName() string
func (ctx *BuildContext) Exec(name string, args ...string)
```

## InstallContext

安装阶段上下文。

```go
type InstallContext struct {
    ConfigAccessor
    *InstallItemHolder
    // ...
}

func (ctx *InstallContext) SetPrefix(prefix string) *InstallContext
func (ctx *InstallContext) Prefix() string
func (ctx *InstallContext) PrefixSet() bool
func (ctx *InstallContext) PackageName() string

func (ctx *InstallContext) AddInstalls(src, dest string) *InstallItemHolder
func (ctx *InstallContext) SetInstallFilter(filter InstallFilterFunc) *InstallItemHolder
func (ctx *InstallContext) GetInstallFilter() InstallFilterFunc
func (ctx *InstallContext) GetInstallItems() []InstallItem

func (ctx *InstallContext) Bool(name string) bool
func (ctx *InstallContext) String(name string) string
```

## CleanContext

清理阶段上下文，用于定义自定义清理逻辑。`vmake clean` 时在目录清理前执行。

```go
type CleanContext struct {
    ConfigAccessor
    // ...
}

func (ctx *CleanContext) SourceDir() string
func (ctx *CleanContext) BuildDir() string
func (ctx *CleanContext) SrcDir() string
func (ctx *CleanContext) PackageName() string

func (ctx *CleanContext) Run(name string, args ...string) error
func (ctx *CleanContext) RunIn(dir, name string, args ...string) error
func (ctx *CleanContext) RunEnv(env map[string]string, name string, args ...string) error
func (ctx *CleanContext) Make(args ...string) error
```

使用示例：

```go
p.OnClean(func(ctx *api.CleanContext) {
    ctx.RunIn(ctx.SrcDir(), "make", "clean")
})
```

## Target

构建目标定义。

```go
// 类型设置
func (t *Target) SetKind(kind TargetKind) *Target
func (t *Target) SetDefault(isDefault bool) *Target
func (t *Target) SetTest(v bool) *Target              // 标记为测试目标（自动设置 isDefault=false）

// 源文件与头文件
func (t *Target) AddFiles(files ...any) *Target
func (t *Target) AddIncludes(dirs ...any) *Target
func (t *Target) AddPublicIncludes(args ...any) *Target  // dirs + optional @"pattern"

// 编译配置
func (t *Target) AddDefines(defines ...any) *Target
func (t *Target) SetLanguages(langs ...string) *Target

// 链接配置
func (t *Target) AddLinks(libs ...any) *Target
func (t *Target) AddProvidedLibs(libs ...string) *Target
func (t *Target) AddDeps(targets ...string) *Target

// 编译/链接选项
func (t *Target) AddCFlags(flags ...any) *Target
func (t *Target) AddCxxFlags(flags ...any) *Target
func (t *Target) AddLdFlags(flags ...any) *Target

// 第三方包构建
func (t *Target) SetBuildFunc(fn func(p *Package) error) *Target
func (t *Target) SetPrebuilt(path string) *Target          // 预编译目标，跳过编译直接 symlink 到输出路径

// RTOS/嵌入式
func (t *Target) SetLinkerScript(path string) *Target    // 传递 -T 给链接器（重复调用 vlog.Fatal）
func (t *Target) UseDependencyLinkerScript() *Target       // 从依赖自动继承 linker script
func (t *Target) AddPostLink(tool string, args ...string) *Target  // 通用后链接步骤，支持 {output} 占位符
func (t *Target) AddPostLinkDeps(files ...string) *Target  // 声明 post-link 步骤依赖的额外输入文件（SourceDir 相对路径）；任一变化（mtime 新于输出或缺失）触发 relink + 重跑全部 post-link
func (t *Target) AddPostLinkHex() *Target               // objcopy -O ihex {output} {output}.hex
func (t *Target) AddPostLinkBin() *Target               // objcopy -O binary {output} {output}.bin
func (t *Target) AddPostLinkSize() *Target              // size {output}
func (t *Target) AddPostLinkStrip() *Target             // strip -o {output}.stripped {output}
func (t *Target) AddBinHeader(inputs ...any) *Target    // 将二进制文件转换为 .h 头文件（GenRule），输出到 build/<tc>-<mode>/generated/<stem>.h，包含路径自动添加

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
func (t *Target) RemoveProvidedLibs(libs ...string) *Target
func (t *Target) RemoveDeps(targets ...string) *Target
func (t *Target) RemoveFiles(files ...any) *Target   // 从 AddFiles 扩展结果中排除文件（模式匹配）

// 获取方法
func (t *Target) Name() string
func (t *Target) Kind() TargetKind
func (t *Target) IsDefault() bool
func (t *Target) IsTest() bool
func (t *Target) Files() []string
func (t *Target) Includes() []string
func (t *Target) PublicIncludes() []string
func (t *Target) Defines() []string
func (t *Target) Languages() []string
func (t *Target) IncludeRule(dir string) []string
func (t *Target) HasDep(depRef string) bool
func (t *Target) Links() []string
func (t *Target) ProvidedLibs() []string
func (t *Target) Deps() []string
func (t *Target) CFlags() []string
func (t *Target) CxxFlags() []string
func (t *Target) LdFlags() []string
func (t *Target) InstallDir() string
func (t *Target) NoInstall() bool
func (t *Target) BuildFunc() func(*Package) error
func (t *Target) Prebuilt() string
func (t *Target) LinkerScript() string
func (t *Target) UseDepLinkerScript() bool
func (t *Target) PostLinkSteps() []PostLinkStep
func (t *Target) PostLinkDeps() []string
func (t *Target) ExcludedFiles() []string
func (t *Target) GenRules() []GenRule
```
`AddFiles/Includes/Defines/Links/CFlags/CxxFlags/LdFlags` 接受 `string` 或 `[]string`（条件表达式返回）。

`AddPublicIncludes` 支持 `@"pattern"` 作为最后一个参数进行 match。Pattern 应用到前面所有目录（省略目录默认为 `"."`）。Pattern 使用 `filepath.Match` 语法。

```go
// 安装所有 .h 文件到 dependents
ctx.Target("mylib").AddPublicIncludes("include")

// 只安装匹配 *.h 的文件
ctx.Target("mylib").AddPublicIncludes("include", "@*.h")

// 只匹配 foo*.h 到 src 目录
ctx.Target("mylib").AddPublicIncludes("include", "src", "@foo*.h")
```

## RequireContext（依赖声明）

RequireContext 嵌入了 `ConfigAccessor`，因此 `Bool()`、`String()`、`If()`、`When()` 等方法均可使用。

```go
// 项目依赖声明
func (ctx *RequireContext) AddRequires(deps ...string) *RequireContext   // "official/zlib >=1.2"
func (ctx *RequireContext) GetRequires() []RequireInfo
func (ctx *RequireContext) ResetRequires()
func (ctx *RequireContext) RunFuncs()
```

## 关键类型

```go
// 回调类型别名
type RequireFunc func(ctx *RequireContext)
type ConfigFunc func(ctx *ConfigContext)
type BuildFunc func(ctx *BuildContext)
type InstallFunc func(ctx *InstallContext)
type CleanFunc func(ctx *CleanContext)
type PackageFunc func(p *Package)

// 结构体
type PkgDirs struct {
    SourceDir, BuildDir, InstallDir string
}

type PackageMeta struct {
    Repo string
    Name string
}
func (m *PackageMeta) FullName() string

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

type SourceOrigin int
const (
    SourceLocal  SourceOrigin = iota
    SourceRemote
)

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

type RequireInfo struct {
    Name       string
    Constraint string
}

type PostLinkStep struct {
    Tool string
    Args []string
}
func (s PostLinkStep) OutputPaths(outputPath string) []string  // 从 post-link 步骤中解析 {output} 占位符生成的文件路径

type GenRuleKind string
const GenRuleBinHeader GenRuleKind = "binheader"

type GenRule struct { ... }
func (r *GenRule) Kind() GenRuleKind
func (r *GenRule) Input() string
func (r *GenRule) OutputStem() string
```

### 语义版本 (`pkg/api/semver.go`)

#### 版本格式

```
[v]MAJOR[.MINOR][.PATCH][-PRERELEASE]
```

| 示例 | 解析结果 |
|------|---------|
| `1.2.3` | `{1, 2, 3, ""}` |
| `v2.0` | `{2, 0, 0, ""}` |
| `1.0.0-rc.1` | `{1, 0, 0, "rc.1"}` |
| `1.0.0-alpha.1` | `{1, 0, 0, "alpha.1"}` |

`v` 前缀可选。MINOR 和 PATCH 缺省为 0。

#### 约束运算符

| 运算符 | 含义 | 示例 | 匹配 | 不匹配 |
|--------|------|------|------|--------|
| `>=` | 大于等于（**默认**），锁定 major | `>=1.2` | `1.2.0`, `1.9.9` | `2.0.0`, `1.1.0` |
| `>` | 大于，不锁定 major | `>1.0.0` | `1.0.1`, `2.0.0` | `1.0.0` |
| `<=` | 小于等于，不锁定 major | `<=2.0` | `1.9.0`, `2.0.0` | `2.0.1` |
| `<` | 小于，不锁定 major | `<3.0` | `2.9.9` | `3.0.0` |
| `=` | 精确匹配（含 pre-release） | `=1.2.3` | `1.2.3` | `1.2.4` |
| `~` | 锁定 major.minor，patch >= | `~1.2.3` | `1.2.3`, `1.2.9` | `1.3.0`, `1.1.9` |
| （无） | 等同 `>=` | `1.2` | 同 `>=1.2` | |

#### Major 兼容性锁定

`>=` 运算符当 major > 0 时自动锁定同一 major 版本范围，确保只在兼容范围内选择版本：

- `>=1.2` → 只匹配 `1.x.x`（不匹配 `2.0.0`）
- `>=2.0` → 只匹配 `2.x.x`（不匹配 `3.0.0`，也不匹配 `1.9.9`）
- `>=0.0.0`（空约束）→ **不锁定**，匹配所有版本（包括 `1.x`, `2.x`）

`>`, `<=`, `<` 运算符**不锁定** major——用于跨版本范围比较。

#### Pre-release 规则

- 有 pre-release 的版本**低于**同版本无 pre-release 的：`1.0.0-rc.1 < 1.0.0`
- Pre-release 按点号分段逐段比较：
  - 纯数字段按数值比较：`1.0.0-1 < 1.0.0-10`
  - 字符串段按字典序比较：`1.0.0-alpha < 1.0.0-beta`
  - **数字 < 字母**：`1.0.0-1 < 1.0.0-alpha`

#### 版本选择算法

1. 过滤满足**所有**约束的版本
2. 按 semver 降序排序
3. 返回**最高**的匹配版本

多约束兼容性：两个约束互相满足即可。`>=1.0` 和 `>=2.0` 兼容（最终选 `>=2.0`），`>=2.0` 和 `<1.5` 不兼容。

#### API

```go
type Version struct {
    Major, Minor, Patch int
    Pre                 string
}

type Constraint struct {
    Op      string    // ">=", "<=", ">", "<", "=", "~"
    Version Version
}

func ParseVersion(s string) (Version, bool)
func (v Version) Compare(other Version) int
func (v Version) String() string
func ParseConstraint(s string) (Constraint, bool)
func (c Constraint) Match(v Version) bool
func MatchVersion(available []string, constraint string) (string, bool)
```

## 构建标志

| 标志 | 短选项 | 说明 |
|------|--------|------|
| `--force` | `-f` | 强制重新编译构建脚本 |
| `--toolchain` | | 覆盖工具链 |
| `--mode` | | 覆盖构建模式（debug/release） |
| `--install` | `-i` | 构建后安装 |
| `--prefix` | `-p` | 安装前缀（默认：`./install/`） |
| `--install-type` | | `runtime`（默认）或 `sdk` |
| `--manifest` | | 从清单文件固定版本 |
| `--tests` | | 包含测试目标 |

## 全局选项

内置全局选项（`pkg/api/global.go`）：

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

全局选项合并：`ConfigAccessor.MergeGlobals(globalOptions, globalVals)` 合并全局选项/值作为回退。合并后 `ctx.Bool()` 和 `ctx.String()` 可同时读取本地和全局值。

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

## 工具函数

### 文件复制 (`pkg/api/copy.go`)

```go
func CopyFile(src, dest string) error
func CopyDir(src, dest string) error
func CopyDirWithFilter(src, dest string, filter CopyFilter) error
func CopyDirIfExists(src, dst string) error
func MatchPatterns(patterns []string, name string) bool

type CopyFilter func(path string, isDir bool) bool
```

`CopyDir` 自动跳过 `.git` 目录。`CopyDirWithFilter` 通过 filter 回调控制复制行为。

### 包引用解析

```go
func SplitPackageRef(ref string) (repo, name string, ok bool)  // "official/zlib" -> ("official", "zlib", true)
```

### 构建模式标志

```go
func GetModeFlags(mode string) (cflags []string, defines []string)  // "debug" -> (["-O0","-g"], []); "release" -> (["-O2"], ["NDEBUG"])
```

## 安装类型过滤

`--install-type` 控制 `vmake build --install` 安装哪些文件：

| 文件类型 | runtime | sdk |
|---------|---------|-----|
| binary → `bin/` | ✓ | ✓ |
| shared (.so) → `lib/` | ✓ | ✓ |
| static (.a) → `lib/` | ✗ | ✓ |
| public includes → `include/` | ✗ | ✓ |
| AddInstalls 自定义文件 | ✓ | ✓ |

默认 `runtime`，只安装运行时所需文件（二进制和动态库）。`sdk` 安装全部内容（含静态库和公共头文件），适合需要二次开发的场景。

## 安装清单

`vmake build --install` 在安装前缀生成 `manifest.json`，记录构建元数据和每个包的版本信息：

- 本地包：`source: "local"`，含 `ref`（git 完整哈希）和 `path`（相对路径）
- Native 包：`source: "native"`，含 `url` 和 `ref`（git tag）
- Registry 包：`source: "registry"`，含 `url` 和 `ref`

通过 `vmake manifest show <path>` 查看，`vmake manifest checkout <path> [name]` 恢复到记录的版本。

## KConfig（固件配置）

KConfig 用于管理基于 `make defconfig` / `make menuconfig` 的固件项目配置（如 Linux 内核、U-Boot、Busybox）。

### Package KConfig 方法

```go
func (p *Package) AddKConfig(name string) *KConfigEntry
func (p *Package) KConfigEntries() []*KConfigEntry
func (p *Package) SelectedPreset() string          // 返回选中的 preset 名（优先 selectedPreset，其次 defaultPreset）
func (p *Package) EnsureConfig(srcDir string) bool  // 检查 .config 是否存在且非空，否则执行 make <preset> 并应用 patches；返回 true 表示重新生成了 .config
```

### ConfigContext KConfig 方法

```go
func (ctx *ConfigContext) KConfig(name string) *KConfigEntry  // 创建或获取 KConfigEntry（与 Package 关联）
```

### KConfigEntry

```go
// 获取方法
func (k *KConfigEntry) Name() string
func (k *KConfigEntry) Description() string
func (k *KConfigEntry) ConfigPath() string     // 默认 ".config"
func (k *KConfigEntry) SrcDir() string
func (k *KConfigEntry) Presets() []string
func (k *KConfigEntry) DefaultPreset() string
func (k *KConfigEntry) SelectedPreset() string
func (k *KConfigEntry) MenuconfigCmd() string
func (k *KConfigEntry) Patches() map[string]string

// 设置方法（链式调用）
func (k *KConfigEntry) SetDescription(desc string) *KConfigEntry
func (k *KConfigEntry) SetConfigPath(path string) *KConfigEntry
func (k *KConfigEntry) SetSrcDir(dir string) *KConfigEntry
func (k *KConfigEntry) SetMenuconfigCmd(cmd string) *KConfigEntry
func (k *KConfigEntry) AddPreset(name string) *KConfigEntry
func (k *KConfigEntry) SetDefault(presetName string) *KConfigEntry
func (k *KConfigEntry) SetSelectedPreset(name string) *KConfigEntry
func (k *KConfigEntry) PatchKConfig(patches map[string]string) *KConfigEntry
```

### 工具函数

```go
func ApplyKConfigPatches(configPath string, patches map[string]string)
```

对 `.config` 文件执行字符串替换（在 defconfig 之后应用补丁）。

### 使用示例

```go
func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.KConfig("uboot").
            SetDescription("U-Boot configuration").
            AddPreset("evk_rk3568_defconfig").
            AddPreset("evk_rk3588_defconfig").
            SetDefault("evk_rk3568_defconfig").
            PatchKConfig(map[string]string{
                "CONFIG_LOCALVERSION=\"-custom\"": "CONFIG_LOCALVERSION=\"-myboard\"",
            })
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("uboot").
            SetKind(api.TargetVoid).
            SetBuildFunc(func(pkg *api.Package) error {
                srcDir := pkg.SrcDir()
                if pkg.EnsureConfig(srcDir) {
                    pkg.RunIn(srcDir, "make", "-j"+strconv.Itoa(runtime.NumCPU()))
                } else {
                    pkg.RunIn(srcDir, "make", "-j"+strconv.Itoa(runtime.NumCPU()))
                }
                return nil
            })
    })
}
```

## 配置导出

VMake 支持两种将配置选项导出给 C 代码的方式。两种方式独立使用，按需选择。

### GenerateConfigHeader — 生成 autoconf.h

在 `OnBuild` 中调用 `ctx.GenerateConfigHeader()`，vmake 会在构建目录的 `generated/autoconf.h` 中生成类似 Linux 内核的配置头文件，并自动将 `generated/` 加入所有目标的包含路径。

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Option("feature_foo").
        SetType(api.OptionBool).
        SetDefault(true).
        SetDescription("Enable feature foo")

    ctx.Option("buffer_size").
        SetType(api.OptionInt).
        SetDefault(4096).
        SetDescription("Buffer size")

    ctx.Option("device_name").
        SetType(api.OptionString).
        SetDefault("uart0").
        SetDescription("Device name")

    ctx.Option("platform").
        SetType(api.OptionChoice).
        SetDefault("linux").
        SetValues("linux", "macos", "windows").
        SetDescription("Target platform")
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.GenerateConfigHeader()

    ctx.Target("app").
        SetKind(api.TargetBinary).
        AddFiles("src/*.c")
})
```

生成的 `generated/autoconf.h`：

```c
#ifndef VMAKE_AUTOCONF_H
#define VMAKE_AUTOCONF_H

#define CONFIG_FEATURE_FOO 1
/* #undef CONFIG_FEATURE_BAR */
#define CONFIG_BUFFER_SIZE 4096
#define CONFIG_DEVICE_NAME "uart0"
#define CONFIG_PLATFORM "linux"
#define CONFIG_PLATFORM_LINUX 1

#endif
```

在 C 代码中使用：

```c
#include "autoconf.h"

#ifdef CONFIG_FEATURE_FOO
void foo_init(void) { ... }
#endif
```

### GenerateConfigDefines — 编译器 -D 宏

在 `OnBuild` 中调用 `ctx.GenerateConfigDefines()`，vmake 会自动将所有配置选项转为 `-D` 编译器宏，添加到该包的所有目标。

```go
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.GenerateConfigDefines()

    ctx.Target("app").
        SetKind(api.TargetBinary).
        AddFiles("src/*.c")
})
```

等效于手动添加：

```
-DCONFIG_FEATURE_FOO=1 -DCONFIG_BUFFER_SIZE=4096
-DCONFIG_DEVICE_NAME="uart0" -DCONFIG_PLATFORM="linux" -DCONFIG_PLATFORM_LINUX=1
```

Bool 选项为 false 时不生成 `-D` 宏（与 `#ifdef` 语义一致）。

在 C 代码中使用：

```c
if (CONFIG_FEATURE_FOO == 1) { ... }
printf("size=%d\n", CONFIG_BUFFER_SIZE);
```

### 宏命名规则

| 选项类型 | 宏名称 | Header 格式 | -D 格式 |
|---------|--------|-------------|---------|
| Bool (true) | `CONFIG_<NAME>` | `#define CONFIG_<NAME> 1` | `-DCONFIG_<NAME>=1` |
| Bool (false) | `CONFIG_<NAME>` | `/* #undef CONFIG_<NAME> */` | 不生成 |
| Int | `CONFIG_<NAME>` | `#define CONFIG_<NAME> <value>` | `-DCONFIG_<NAME>=<value>` |
| String | `CONFIG_<NAME>` | `#define CONFIG_<NAME> "<value>"` | `-DCONFIG_<NAME>="<value>"` |
| Choice | `CONFIG_<NAME>` + `CONFIG_<NAME>_<VALUE>` | `#define` 两个宏 | `-D` 两个宏 |

宏名称规则：`CONFIG_` + 选项名大写 + `-` 替换为 `_`。全局选项（`mode`、`toolchain`）不导出。

### 注意事项

- 两种方式可以同时使用，语义一致：Bool false 时 header 写 `/* #undef */`，defines 不生成 `-D`，`#ifdef` 行为相同
- `GenerateConfigHeader` 适合需要 `#ifdef` 条件编译的场景（嵌入式/固件风格）
- `GenerateConfigDefines` 适合不需要头文件、直接通过编译器宏传递配置的场景
- **公开头文件不要 `#include "autoconf.h"`**：`autoconf.h` 仅限包内使用，不应出现在通过 `AddPublicIncludes` 暴露的头文件中。跨包配置共享应使用 `-D` 宏（`GenerateConfigDefines` + `ImportConfig`）

### 配置跨包传播

分布式工程中，包的配置选项默认只在定义它的包内生效。通过以下 API 实现跨包配置共享：

| API | 调用者 | 作用 |
|-----|--------|------|
| `ExportConfig()` | 被依赖方 | 设置 `exportConfig = true` 标志，传给 Package.SetExportConfig(true) |
| `ImportConfig(names...)` | 依赖方 | 向 `importConfigs` 追加包名；实际的配置合并和 `-D` 注入在 `GenerateConfigDefines` 的处理块中完成 |
| `SyncConfigDefines(names...)` | 父包（编排者） | 等价于 `GenerateConfigDefines` + `ImportConfig`，一次性同步多个子包 |

示例——芯片包导出配置，驱动包导入，根包统一同步：

```go
// chip/build.go — 芯片包（被依赖方，声明导出）
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.GenerateConfigDefines()
    ctx.ExportConfig()

    ctx.Target("chip").SetKind(api.TargetStatic).
        AddFiles("src/*.c")
})

// driver/build.go — 驱动包（依赖 chip，导入其配置）
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.GenerateConfigDefines()
    ctx.ImportConfig("chip")

    ctx.Target("driver").SetKind(api.TargetStatic).
        AddFiles("src/*.c")
})

// firmware/build.go — 根包（编排者，一次性同步所有子包）
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.SyncConfigDefines("chip", "driver", "rtos")

    ctx.Target("firmware").SetKind(api.TargetBinary).
        AddFiles("src/*.c")
})
```

传播的配置以 `-D` 编译器宏形式注入到当前包的所有目标。选项合并时，本包选项优先于导入选项（同名不覆盖）。

`autoconf.h` 不跨包传播——每个包的 `autoconf.h` 只包含本包视角的合并选项，不会通过 `AddPublicIncludes` 暴露给下游。

## SplitPackageRef (`pkg/api/package.go`)

```go
func SplitPackageRef(ref string) (repo, name string, ok bool)
// "official/zlib" -> ("official", "zlib", true)
```

用于解析 `repo/name` 格式的包引用。

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
            SetTest(true)  // 测试目标，不默认构建
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
            AddDeps("lib:*")     // 通配依赖：链接 lib 包下所有 target
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
            AddDeps("official/zlib")
    })
}
```

### 使用 Git Patch 修复第三方包

```go
func Main(p *api.Package) {
    p.AddPatches("patches/fix-build.patch")

    p.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires("official/zlib >=1.2")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c").
            AddDeps("official/zlib")
    })
}
```

Patch 文件在源码下载后、构建前通过 `git apply --3way` 自动应用。已应用的 patch 会被跳过。

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
            AddProvidedLibs("z").
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

### Native 仓库包定义

Native 仓库的 `build.go` 与本地项目完全相同 — 不需要 `OnPackage`、`SetGit`、`AddVersion`。版本由 git tag 自动提取。

```go
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("mylib").
            SetKind(api.TargetStatic).
            AddFiles("src/*.c").
            AddPublicIncludes("include")
    })
}
```

使用前需要添加 native 仓库：

```bash
vmake repo add --native myorg "https://git.example.com/{name}.git"
```

在消费方项目中声明依赖：

```go
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("myorg/mylib >=1.0")
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").
        SetKind(api.TargetBinary).
        AddFiles("src/main.c").
        AddDeps("myorg/mylib")
})
```

两种仓库类型对比：

| | Index 仓库 | Native 仓库 |
|--|--|--|
| **用途** | 包装第三方 C/C++ 库 | VMake 原生包，跨项目共享 |
| **build.go** | 包装器（调用 CMake 等） | 真正的构建描述（同本地项目） |
| **源码位置** | build.go 在仓库中，源码在别处 | build.go 在包的 git 仓库根目录 |
| **版本来源** | `AddVersion()` 手动映射 | git tag（自动过滤有效 semver） |
| **添加命令** | `vmake repo add name url` | `vmake repo add --native name "https://..../{name}.git"` |
| **更新** | `vmake repo update name` | `vmake pkg update repo/name` |

## 扩展插件 API

扩展插件用于扩展 vmake CLI 命令和工具链管理。详见 [扩展插件指南](EXTENSION_PLUGIN.md)。
