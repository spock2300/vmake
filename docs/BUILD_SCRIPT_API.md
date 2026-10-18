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
                pkg.CMakeBuild()
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
func (p *Package) OnPackage(fn PackageFunc) *Package      // 填充包元数据（插件提取阶段执行；单槽回调，重复注册 fatal）
```

### 元信息设置

```go
func (p *Package) SetGit(urls ...string) *Package
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

本地包也可通过 `SetGit` 选择外部源码。声明 `AddVersion` 映射时，按依赖约束选择版本并在 `.vmake/vmake.lock` 固定版本和提交；锁定版本的浅工作树已存在时可完全离线构建，工作树被删除后只要共享对象缓存仍保留该提交/引用也可从本地缓存重建，仅在缓存未命中或需要重新解析 ref 时才访问上游。`vmake lock update` 重新选择版本。没有版本映射时使用仓库 HEAD。项目目录保留，受管理的 `src` 链接指向项目内该包唯一的工作树（`.vmake_deps/local/<包名>/src`）。浅树只包含所选版本（`git fetch --unshallow` 可补全历史）。若 `src` 已是真实目录，构建会报错并保留该目录，需先由用户移开。

### Git Patch

```go
func (p *Package) AddPatches(paths ...string) *Package
func (p *Package) SetPatches(paths ...string) *Package
func (p *Package) GetPatches() []string
```

Patch 路径相对 `ScriptDir()`（build.go 所在目录）。Patch 文件在源码下载后、构建前通过 `git apply --3way` 自动应用。已应用的 patch 会被跳过。

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
func (p *Package) CMakeBuildDir() string
func (p *Package) CMakeInstallDir() string
func (p *Package) ScriptDir() string
```

### 编译器信息（代理 Toolchain）

```go
func (p *Package) CC() string
func (p *Package) CXX() string
func (p *Package) AR() string
func (p *Package) TargetOS() string
func (p *Package) TargetTriple() string
func (p *Package) SetPlatform(platform Platform) *Package
func (p *Package) Prefix() string
func (p *Package) CFlags() string
func (p *Package) CXXFlags() string
func (p *Package) LDFlags() string
func (p *Package) Env() map[string]string
```

`TargetTriple()` 返回本包解析后的 `target_triple`（例如 `arm-none-eabi`），`TargetOS()` 返回本包的目标系统；旧方法 `CrossTarget()` 已移除。裸机使用 `none`，`target_os` 为空（或为 `host`）时取运行 vmake 的主机系统。两者按“包级配置值 → 包内声明的非全局 `target_os`/`target_triple` 选项默认值 → 全局配置值/项目全局选项默认值 → 所选工具链 `toolchain.json` 默认值”的顺序解析（空值落到下一级），因此包级默认值可以覆盖项目全局值，同一个编译器可服务不同目标：

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("none")
    ctx.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString).SetDefault("arm-none-eabi")
})
```

`SetPlatform(api.Platform{OS: ..., Triple: ...})` 直接写入本包平台；常规构建会在 `OnBuild` 回调前按上面的选项解析结果设置平台，因此脚本通常只需读取 `TargetOS()`／`TargetTriple()`。

`CFlags()`／`CXXFlags()`／`LDFlags()` 返回内置 `host` 工具链的默认选项，交叉工具链下为空；同样的值可用 `DefaultBuildFlags(toolchainName, targetOS)` 取得（返回 `DefaultFlags{CFlags, CxxFlags, LdFlags}`，非 `host` 工具链返回零值）。目标 CPU/ABI 选项由 `build.go` 通过 `AddGlobalCFlags` 等或目标级 `AddCFlags` 提供。

`Prefix()` 原样返回工具链配置的前缀，例如 `arm-none-eabi-`，包含末尾的 `-`，不补充安装目录。拼接工具名时直接使用 `p.Prefix() + "gcc"`，不能再添加 `-`。迁移旧脚本时，应检查 `p.Prefix() + "-gcc"` 和传给外部工具链文件的前缀参数；要求目标三元组的参数使用 `TargetTriple()`。

`p.Env()` 返回已解析的工具路径，并把工具链的 `InstallPath/bin` 和绝对路径编译器 `CC`／`CXX` 所在目录前置到 `PATH`。`p.Run`、`p.RunIn`、`p.RunEnv`、`ctx.Exec` 及 CMake／Make 辅助方法也自动传递这个 `PATH`，外部程序启动的子进程会继承它；vmake 不修改进程级环境。`p.RunEnv` 显式提供的 `PATH` 优先，可以替换自动生成的路径。工具链声明安装目录时，`CROSS_COMPILE` 包含该目录下的 `bin` 路径和完整前缀，例如 `/path/to/toolchain/bin/arm-none-eabi-`，同样不能再添加 `-`。`CFLAGS`／`CXXFLAGS`／`LDFLAGS` 来自本包的默认与全局选项。CMake 工程优先使用 `p.CMakeConfigure()`，它会自动传递已解析的工具。仅在 API 无法表达的特殊操作中手动调用 CMake，并使用 `p.Env()["CC"]`、`p.Env()["CXX"]`、`p.Env()["AR"]` 等路径，避免从前缀重建工具名并依赖 PATH。

### RTOS 工具访问器

```go
func (p *Package) ObjCopy() string
func (p *Package) Size() string
func (p *Package) ObjDump() string
func (p *Package) NM() string
```

### 依赖 Linker Script

```go
func (p *Package) SetProvidedLinkerScript(path string) *Package  // 声明 linker script（重复调用抛出 BuildScriptError）
func (p *Package) ProvidedLinkerScript() string
```

### 构建辅助方法

```go
func (p *Package) CMakeConfigure(extraArgs ...string)
func (p *Package) CMakeBuild(args ...string)
func (p *Package) CMakeInstall(args ...string)
func (p *Package) SetCMakeBuildDir(dir string) *Package
func (p *Package) SetCMakeInstallDir(dir string) *Package
func (p *Package) SetCMakeBuildType(buildType string) *Package
func (p *Package) Configure(extraArgs ...string) error
func (p *Package) Make(args ...string) error
func (p *Package) Run(name string, args ...string)              // 在 BuildDir 运行命令（失败时抛出 BuildScriptError，由执行边界返回）
func (p *Package) RunIn(dir, name string, args ...string)       // 在指定目录运行命令（失败时抛出 BuildScriptError，由执行边界返回）
func (p *Package) RunEnv(env map[string]string, name string, args ...string) error
```

在 VMake 中构建 CMake 工程时，优先使用 `CMakeConfigure`、`CMakeBuild`、`CMakeInstall`。工具链解析、跨平台路径、构建目录、安装前缀、构建配置和并行规则由 API 管理；build.go 只声明项目选项及专用步骤。仅在 API 无法表达的特殊操作中直接调用 cmake。

三阶段共用 `CMakeBuildDir()`，默认是 `BuildDir()/cmake`。源码默认取 `SrcDir()`。`CMakeInstallDir()` 默认返回远程包的 `InstallDir()`，本地包则使用 `BuildDir()/staging`。通过目录 setter 修改时，相对路径基于 `BuildDir()`，绝对路径保持不变。这些设置不改变 `PkgDirs` 或远程包发布机制；远程包使用自定义安装目录时，需要将产物发布到原有 `InstallDir()` 或显式声明产物。

默认构建配置按 VMake mode 选择 Debug／Release／MinSizeRel，`SetCMakeBuildType("MinSizeRel")` 等设置对 configure、build、install 均生效。Build、Install 的显式 `--config` 覆盖本次调用的配置。通过 setter 管理构建目录、安装前缀和默认构建类型；原生目录覆盖参数、`--prefix`、`-DCMAKE_INSTALL_PREFIX`、`-DCMAKE_BUILD_TYPE` 会报错并提示对应 setter，其他项目参数继续透传。

多配置生成器要求所选配置已包含在工程或 preset 声明的 `CMAKE_CONFIGURATION_TYPES` 中。`SetCMakeBuildType` 只选择配置，不改写可用配置集合。例如 Ninja Multi-Config 默认不含 MinSizeRel；需要它时，可向 `CMakeConfigure` 传入 `"-DCMAKE_CONFIGURATION_TYPES=Debug;Release;MinSizeRel"`。

`CMakeConfigure` 严格解析已配置的编译器及 binutils，ASM 默认使用已解析的 CC 驱动。注入哪些编译器变量取决于 `CMakeLists.txt` 中 `project(... LANGUAGES ...)`（含 legacy `project(name LANG...)`）声明的语言：只注入 C/CXX/ASM 中实际声明的项及其 `*_COMPILER_TARGET`；AR/RANLIB/STRIP/OBJCOPY/NM 始终注入；没有可解析语言列表时保持全部注入。VMake 不将 `Tools.LD` 映射为 `CMAKE_LINKER`，底层链接器由 CMake 根据编译器识别。需要交叉时（`target_triple` 非空或 `target_os` 与宿主不同），`CMAKE_SYSTEM_NAME` 由目标系统决定；裸机使用 `Generic`、`CMAKE_TRY_COMPILE_TARGET_TYPE=STATIC_LIBRARY`，并从宿主查找程序、从目标根查找库和头文件。需要处理器信息时由项目显式传入 `CMAKE_SYSTEM_PROCESSOR`。

`CMakeConfigure` 只在指纹变化或 configure 产物缺失时重新运行 configure，空闲的重复构建不会重放 configure。指纹包含本次 configure 参数（生成器、目标系统、构建类型、解析后的工具与全局 flags）、configure 会读取的环境（`PATH`、`CMAKE_*`、`PKG_CONFIG_PATH`、`SDKROOT` 等，以及 `VMAKE_ENV_EXTRA` 指定的变量；已被 `-D` 显式覆盖的 `CC`/`CXX`/`AR` 与 `CFLAGS`/`CXXFLAGS`/`CPPFLAGS`/`LDFLAGS` 不计入）、cmake 可执行文件的内容，以及 `SrcDir()` 下所有 `CMakeLists.txt`／`CMakePresets.json`／`CMakeUserPresets.json`／`*.cmake`／`*.cmake.in` 的内容哈希；扫描跳过 `.git`、`.vmake_deps`、本次构建根（本地包为 `<SourceDir>/build`，远程包为 `out/<sha256(member)>/<key>`）、`CMakeFiles/` 与 `cmake_install.cmake`；`SrcDir()` 为符号链接时会先解析真实路径再扫描。命中缓存时输出 `cmake configure (cached): <CMakeBuildDir>` 并跳过 configure；命中还要求 configure 产物仍在（`CMakeCache.txt`、`CMakeFiles/` 及已知生成器的 `build.ninja`／`Makefile`），避免手工删掉构建文件后跳过 configure 导致 `cmake --build` 失败。configure 成功后写入 stamp 文件 `<CMakeBuildDir>/.vmake_configure.json`；dry-run 既不读取也不写入 stamp；指纹计算失败视为 miss，单个不可读输入被跳过（其可读性/内容变化会自然导致 miss），绝不让构建因此失败。使用 `file(GLOB)` 的项目在新增文件时不会触发重新 configure，需要改用 `CONFIGURE_DEPENDS` 或在 `CMakeLists.txt` 中显式列出文件。

Windows 主机默认使用 Ninja，尊重显式生成器、`CMAKE_GENERATOR` 环境变量及 configure preset。通过 `CMakeConfigure("--preset", "name")` 使用配置预设；`CMakeBuild` 拒绝 `--preset`，因为 build preset 的 `binaryDir` 会覆盖 API 管理的目录。构建目标和配置分别通过 `--target`、`--config` 选择。API 使用独立 argv 和规范化路径，不提前解析默认 make。`CMakeBuild()` 与 `Make()` 使用本次 `vmake -j N` 的并发上限。参数、环境变量及透传给后端的并发设置只能降低该上限；无限并发、非正数或超过上限均报错。未指定 `-j` 或 `-j0` 时，在构建开始时解析 CPU 数。`CMakeInstall(args...)` 支持 `--component` 等原生安装选项。

`Env()` 返回不含 shell 引号的工具绝对路径，可用于直接执行程序。`Make`、`Configure` 和 `EnsureConfig` 会针对 shell 单独引用环境中的工具路径，保留路径内的空格；make 环境还会保护美元符号，避免被 make 展开。`CleanContext.Make` 使用相同处理。

### CMake 编译与链接标志传递

`CMakeConfigure` 自动传递本包的默认符号可见性及 `AddGlobalCFlags`／`AddGlobalCxxFlags`／`AddGlobalLdFlags` 设置的全局 C、CXX 及可执行文件／共享库／模块库链接标志（`CMAKE_EXE_LINKER_FLAGS`、`CMAKE_SHARED_LINKER_FLAGS`、`CMAKE_MODULE_LINKER_FLAGS`）。包级可见性默认值位于全局及显式追加标志之前。显式传入某个 flags 变量会替换该变量的自动值；如需追加项目标志，使用 `Merged*Flags` 保留包级默认值与全局值。ASM flags 由项目显式声明，不自动复制全部 C flags。`AddGlobalLinks` 不参与该自动传递，CMake 工程需自行在 `CMakeLists.txt` 中链接这些库。

```go
func (p *Package) CMakeGlobalFlagsArgs() []string
func (p *Package) MergedCFlags(extra ...string) string
func (p *Package) MergedCxxFlags(extra ...string) string
func (p *Package) MergedLdFlags(extra ...string) string
```

使用示例：

```go
p.SetCMakeBuildType("MinSizeRel")
cflags := p.MergedCFlags("-fno-builtin")
p.CMakeConfigure(
    "-DBUILD_SHARED_LIBS=OFF",
    "-DCMAKE_C_FLAGS="+cflags,
    "-DCMAKE_ASM_FLAGS="+cflags,
)
p.CMakeBuild()
p.CMakeInstall()
```

`MergedCFlags`／`MergedCxxFlags` 按包级可见性默认值、全局标志、额外标志的顺序返回按空格拼接的字符串；`MergedLdFlags` 合并全局链接标志与额外标志。`CMakeGlobalFlagsArgs()` 同样包含本包可见性，保留给特殊的手动 CMake 调用；使用 `CMakeConfigure` 时无需再次传入。`Make`／`Configure` 不注入 CMake 风格的 flags 变量：相关标志经由 `p.Env()` 的 `CFLAGS`／`CXXFLAGS`／`LDFLAGS` 传入；已有使用 `Merged*Flags` 的脚本会获得本包的策略。

### CMake 脚本迁移

- 原先直接位于 `BuildDir()` 的 CMake cache 和产物迁至 `CMakeBuildDir()`；CMake 安装产物统一通过 `CMakeInstallDir()` 引用。
- 原生 `-B`、安装前缀及 build-type 覆盖改用 setter；删除通用工具链文件生成、编译器前缀拼接和默认并行参数计算。
- configure 之后调用 `CMakeBuild()`／`CMakeInstall()`，不再调用 `pkg.Make()` 构建 CMake 工程。
- 本地 `SetPrebuilt` 包保持产物与发布位置分离：例如 CMake 生成 `cmake/libfoo.a`、安装到 `staging/lib/libfoo.a`，VMake 的顶层 `libfoo.a` 链接指向安装产物。
- 迁移后清理旧构建目录再构建。所需 Void 构建回调每次构建执行一次，增量由 Make/CMake 自行判断；不再使用包级 stamp 或“安装目录非空”跳过回调。configure 阶段本身由 `CMakeConfigure` 的指纹缓存判断（见上文），不改变回调每次执行的行为。

### 获取方法

```go
func (p *Package) FullName() string                // 完整包名（repo/name 或 name）
func (p *Package) GetOptions() map[string]*Option
func (p *Package) GetRequires() *Requires
func (p *Package) GitURLs() []string
func (p *Package) Homepage() string
func (p *Package) Description() string
func (p *Package) License() string
func (p *Package) Submodules() bool
func (p *Package) DefaultVisibilityHidden() bool
func (p *Package) VisibilityFlags() (cflags, cxxflags []string)
func (p *Package) Versions() map[string]string
func (p *Package) GenConfigHeader() bool           // 配置头文件是否启用
func (p *Package) ExportConfig() bool
func (p *Package) ImportConfigs() []string
func (p *Package) TargetOS() string
func (p *Package) SrcDirRaw() string               // 原始 srcCodeDir，无 SourceDir 回退（SetSrcDir 未调用时返回空串）
```

### 配置文件声明

```go
func (p *Package) SetConfigFiles(files ...string) *Package  // 包级配置文件列表，不控制自定义回调是否执行
func (p *Package) ConfigFiles() []string
```

当前实现只保存这个列表，构建流程没有读取它。

### 源码与输出目录

```go
func (p *Package) SetSrcDir(dir string) *Package    // 设置源码目录（与 SourceDir 不同，当 SetGit 下载源码时使用）
func (p *Package) SrcDir() string                    // 返回 SetSrcDir 的值；未设置时回退到 SourceDir（构建阶段本地 SetGit 包通常指向 <SourceDir>/src 链接）
```

### DryRun

```go
func (p *Package) SetDryRun(v bool) *Package   // 设置 dry run 模式（只打印不执行）
func (p *Package) DryRun() bool
```

`vmake query`、`query targets`、`check-symbols` 等只声明场景在 `Inspect` 阶段不会物化源码（前面的依赖解析仍可能拉取远程包）：本地 SetGit 包的工作树尚未物化时，其 `BuildDir()` 为空。这类场景下内置的 `Run`/`Make`/`CMake*` 只打印不执行，但脚本自己的 `os.ReadFile`/`os.WriteFile` 等不会被拦截；`OnBuild` 中做真实文件读写前必须用 `p.DryRun()` 或 `p.BuildDir() == ""` 守卫，且不能假定 `SrcDir()` 下的源码存在。`BuildContext` 也提供 `SetDryRun(bool)`，仅由这类只声明流程使用。`vmake query`/`query targets`/`check-symbols` 与非 `--all` 的 `clean` 使用 `InspectWithOptions(... FilterDeps: true)`，在声明前按当前配置过滤依赖，因此与 `RunBuild` 的包集合一致；`clean --all` 扫描所有配置与包，不经过该过滤。直接调用 `Inspect` 仍保留 eager 图。`query config` 直接读取配置，不经过 `Inspect`。

### 版本选择

```go
func (p *Package) GetRef(version string) string              // 根据 version 名获取 git ref
func (p *Package) GetVersions() []string                     // 获取所有可用版本（未排序）
func (p *Package) SelectVersion(constraint string) (string, error)  // 根据约束选择最佳匹配版本
func (p *Package) SelectVersionMulti(constraints []string) (string, error)  // 根据多约束选择最佳匹配版本
```

## ConfigAccessor（条件表达式与值读取）

`ConfigAccessor` 被嵌入到 `Package`、`ConfigContext`、`BuildContext`、`InstallContext`、`CleanContext`、`RequireContext` 中，提供选项值读取和条件表达式。

脚本上下文（Config/Build/Install/Clean/Require）的访问器是**严格的**：读取未声明的选项名、或用与选项 `SetType` 不匹配的方法读取（如对 `OptionInt` 选项用 `Bool()`），会 fatal（panic `*BuildScriptError`）。`OnRequire` 的首次发现阶段选项值尚不可用，直接读取（`Bool()` 等）同样 fatal——请使用发现感知的 `ctx.When` / `ctx.If` / `ctx.Select`；`FilterDeps` 阶段的第二次 `OnRequire` 可以读取本包声明的选项（以及 `entries.<pkg>.options` 中的包级值）。`Package` 内嵌的访问器不启用严格检查。

```go
// 值读取（优先配置值，其次默认值）
func (a *ConfigAccessor) Bool(name string) bool
func (a *ConfigAccessor) String(name string) string
func (a *ConfigAccessor) Int(name string) int
func (a *ConfigAccessor) BoolStr(name string) string          // "ON" / "OFF"

// 条件表达式
func (a *ConfigAccessor) If(option string, then ...string) []string
func (a *ConfigAccessor) Select(option string, mapping map[string]string) string
func (a *ConfigAccessor) When(option string, value any) bool

// 选项管理
func (a *ConfigAccessor) Option(name string) *Option        // 配置阶段结束后（Build/Install/Clean 上下文）调用会 fatal
func (a *ConfigAccessor) SetOptions(options map[string]*Option) *ConfigAccessor
func (a *ConfigAccessor) SetCfgVals(vals map[string]any) *ConfigAccessor
func (a *ConfigAccessor) MergeGlobals(globalOptions map[string]*Option, globalVals map[string]any)
```

只有 `OnConfig` 中通过 `ctx.Option`/`ctx.GlobalOption` 声明的选项会经过正常的选项收集、校验和值解析。`BuildContext`/`InstallContext`/`CleanContext` 上的 `Option()` 因声明已锁定而 fatal；`RequireContext` 和 `Package` 上的 `Option()` 不会立即报错，但不会经过 OnConfig 的收集与校验，不应作为声明配置项的方式。

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

// 全局编译/链接标志（在 OnConfig 和 OnApply 回调中有效）
func (ctx *ConfigContext) AddGlobalCFlags(flags ...string)
func (ctx *ConfigContext) AddGlobalCxxFlags(flags ...string)
func (ctx *ConfigContext) AddGlobalLdFlags(flags ...string)
func (ctx *ConfigContext) AddGlobalLinks(links ...string)  // 添加全局链接库（原生链接；CMakeConfigure 不自动传递）
func (ctx *ConfigContext) SetDefaultVisibilityHidden() *ConfigContext  // 仅本包加 -fvisibility=hidden（C++ 另加 -fvisibility-inlines-hidden）

// 依赖 Linker Script
func (ctx *ConfigContext) SetProvidedLinkerScript(path string) *ConfigContext
```

`SetDefaultVisibilityHidden()` 在 `OnConfig` 或选项 `OnApply` 中启用本包所有目标的默认符号隐藏，重复调用幂等；无关联包时报告 `BuildScriptError`。本地、远程、Registry 和 Native 包采用相同语义，依赖包保留自身的导出规则，不会被注入 `-fvisibility=default`。多个自有包需要隐藏时，分别在各自的 `OnConfig` 中声明。显式 `AddGlobalCFlags`／`AddGlobalCxxFlags` 仍影响所有包，包括显式传入的 hidden 标志。

`DefaultVisibilityHidden()` 返回本包是否启用隐藏；`VisibilityFlags()` 返回对应的 C 和 C++ 默认标志，未启用时均为空。切换包级隐藏策略只改变该包的构建缓存标识；全局标志变化仍影响所有包。

## Option

配置选项定义。

```go
// 设置方法（链式调用）
func (o *Option) SetType(t OptionType) *Option
func (o *Option) SetDefault(v any) *Option
func (o *Option) SetDescription(desc string) *Option
func (o *Option) SetValues(vals ...string) *Option        // OptionChoice 使用
func (o *Option) SetShowIf(fn func(ctx *ConfigContext) bool) *Option  // 条件显示
func (o *Option) SetOnApply(fn func(ctx *ConfigContext, val any)) *Option  // 选项值解析后的回调；按选项名排序执行，非声明顺序
func (o *Option) SetGroup(group string) *Option          // SetGroup(api.GroupGlobal)（"Global"）使选项成为全局选项

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
func (o *Option) MacroName() string
func (o *Option) MacroDefines(val any) ([]string, error)  // 全局宏的 -D 内容，不含 "-D" 前缀

// 相关函数
func NormalizeOptionValue(opt *Option, val any) any
func ValidateOption(o *Option) error
```

`SetOnApply` 的 `val` 只对 `OptionInt` 做归一化：配置解码产生的 `float64`/`int64` 会转成 `int`；其他类型原样传入。每个包内，所有选项的 `OnApply` 按选项名字典序执行，与声明顺序无关。

配置阶段收集选项时会自动调用 `ValidateOption(o)`，校验默认值类型、Choice 默认值是否在已声明的 `SetValues` 中（列表非空时）以及宏名；`NormalizeOptionValue(opt, val)` 与上面的 `OnApply` 归一化规则相同，仅处理 `OptionInt`。

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
func (ctx *BuildContext) SetDefaultFlags(cflags, cxxflags, ldflags []string)  // 设置此后创建的 target 的默认编译/链接标志（不回溯修改已有 target）

// 条件表达式（继承自 ConfigAccessor）
func (ctx *BuildContext) If(option string, then ...string) []string
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

// 配置导出状态
func (ctx *BuildContext) GenConfigHeader() bool
func (ctx *BuildContext) GenConfigDefines() bool
func (ctx *BuildContext) ExportEnabled() bool
func (ctx *BuildContext) ImportConfigs() []string

// 其他
func (ctx *BuildContext) PackageName() string
func (ctx *BuildContext) Exec(name string, args ...string)
```

`DepOutput`/`DepBuildDir` 接受 `pkg` 或 `pkg:target`（含子包短名），但不支持 `AddDeps` 的“同包 target 简写”和 `pkg:*` 通配：`DepOutput("utils")` 会把 `utils` 当作包名，`DepOutput("lib:*")` 返回空串。省略 target 时，只有该包恰好一个 target 才返回产物路径，否则返回空串。dry-run 下 `DepOutput` 返回空串（`DepBuildDir` 因此为 `.`），`BuildSubGraph` 直接返回。

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
func (ctx *InstallContext) Int(name string) int
```

`SetPrefix` 为该包设置安装前缀，仅在未传 `--prefix` 时生效（显式 `--prefix` 对所有包优先，其目录会在安装前清空）。不带 `--prefix` 时默认的 `<project>/install` 会清空而自定义前缀不清空，`manifest.json` 仍只写入 CLI/默认前缀；建议传绝对路径。`AddInstalls(src, dest)` 的 `src` 相对包 `SourceDir()`，`dest` 相对安装前缀；缺失的 `src` 会被跳过。

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

func (ctx *CleanContext) Run(name string, args ...string)             // 在 BuildDir 运行命令（失败时抛出 BuildScriptError，由执行边界返回）
func (ctx *CleanContext) RunIn(dir, name string, args ...string)      // 在指定目录运行命令（失败时抛出 BuildScriptError，由执行边界返回）
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

构建目标定义。新 target 默认是 `TargetBinary` 且参与默认构建；重复调用 `Target(name)` 返回同一对象。

```go
// 类型设置
func (t *Target) SetKind(kind TargetKind) *Target
func (t *Target) SetDefault(isDefault bool) *Target
func (t *Target) SetTest(v bool) *Target              // 标记为测试目标（不改变 isDefault；默认 target 在普通 build 及安装中跳过，--tests/vmake test 才构建）

// 源文件与头文件
func (t *Target) AddFiles(files ...any) *Target
func (t *Target) AddIncludes(dirs ...any) *Target
func (t *Target) AddPublicIncludes(args ...any) *Target  // dirs + optional @"pattern"

// 编译配置
func (t *Target) AddDefines(defines ...any) *Target
func (t *Target) SetLanguages(langs ...string) *Target   // 目前无效果：语言按源文件扩展名自动识别

// 链接配置
func (t *Target) AddLinks(libs ...any) *Target
func (t *Target) AddProvidedLibs(libs ...string) *Target
func (t *Target) AddDeps(targets ...string) *Target

// 编译/链接选项
func (t *Target) AddCFlags(flags ...any) *Target
func (t *Target) AddCxxFlags(flags ...any) *Target
func (t *Target) AddLdFlags(flags ...any) *Target

// 第三方包构建
func (t *Target) SetBuildFunc(fn func(p *Package) error) *Target  // 仅 TargetVoid 执行；其他 kind 设置后不会调用
func (t *Target) SetPrebuilt(path string) *Target          // 预编译目标，跳过编译（无后链接步骤时 symlink 到输出路径，有后链接步骤时复制；TargetVoid 忽略；重复调用 fatal）

// RTOS/嵌入式
func (t *Target) SetLinkerScript(path string) *Target    // 仅 TargetBinary：传递 -T 给链接器（Darwin 目标报错；其他 kind 忽略；重复调用抛出 BuildScriptError）
func (t *Target) UseDependencyLinkerScript() *Target       // 仅 TargetBinary：从依赖自动继承 linker script（找不到时报错）
func (t *Target) SetVersionScript(path string) *Target     // 版本脚本，链接时加 -Wl,--version-script=；仅 Shared/Binary 且仅 ELF（Windows/macOS 目标报错）；路径拼接在包 SourceDir 之后（绝对路径不会保留为绝对路径）；重复调用 fatal
func (t *Target) AddExcludeLibs(libs ...string) *Target    // Shared/Binary 链接时加 -Wl,--exclude-libs=（仅 ELF；追加；ld 按去掉 .a 的完整库名匹配，写 libfoo 而非 foo）
func (t *Target) SetSymbolBinding(mode string) *Target     // Shared/Binary："static" → -Wl,-Bsymbolic；"static-functions" → -Wl,-Bsymbolic-functions（仅 ELF）；空串清除；其他值 fatal
func (t *Target) SetSymbolPrefix(prefix string) *Target    // post-link 追加 objcopy --prefix-symbols=<prefix>（需所选工具链提供 objcopy，GNU objcopy 不支持 Mach-O；重复调用 fatal）
func (t *Target) AddPostLink(tool string, args ...string) *Target  // 后链接步骤（仅工具链内置 objcopy/size/objdump/nm/strip），支持 {output} 占位符
func (t *Target) AddPostLinkOutputs(paths ...string) *Target
func (t *Target) AddPostLinkDeps(files ...string) *Target  // 声明 post-link 步骤依赖的额外输入文件（SourceDir 相对路径；不展开 {output}）；非 Void 目标上任一变化（内容或权限变化、文件缺失）触发 relink + 重跑全部 post-link
func (t *Target) AddPostLinkHex() *Target               // objcopy -O ihex {output} {output}.hex
func (t *Target) AddPostLinkBin() *Target               // objcopy -O binary {output} {output}.bin
func (t *Target) AddPostLinkSize() *Target              // size {output}
func (t *Target) AddPostLinkStrip() *Target             // strip -o {output}.stripped {output}
func (t *Target) AddBinHeader(inputs ...any) *Target    // 将二进制文件转换为 .h 头文件（GenRule），输入相对 SourceDir，输出到构建目录的 generated/<stem>.h，包含路径自动添加

// 安装控制
func (t *Target) SetInstallDir(dir string) *Target     // 安装到前缀下的该子目录（默认按 kind 进入 bin/lib）
func (t *Target) SetInstall(install bool) *Target      // SetInstall(false) 跳过该 target 的安装

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
func (t *Target) VersionScript() string
func (t *Target) ExcludeLibs() []string
func (t *Target) SymbolBinding() string
func (t *Target) SymbolPrefix() string
func (t *Target) PostLinkSteps() []PostLinkStep
func (t *Target) PostLinkOutputs() []string
func (t *Target) PostLinkDeps() []string
func (t *Target) ExcludedFiles() []string
func (t *Target) GenRules() []GenRule
```
`...any` 方法（`AddFiles`/`AddIncludes`/`AddPublicIncludes`/`AddDefines`/`AddLinks`/`AddCFlags`/`AddCxxFlags`/`AddLdFlags`/`RemoveFiles`/`AddBinHeader`）接受 `string`、`[]string` 或 `[]any`。注意 yaegi 限制：不能对 `...any` 参数使用 `[]string...` 展开，直接传 `[]string` 即可（方法内部会展开）。`...string` 方法（`AddDeps`、`AddProvidedLibs`、`AddExcludeLibs`、`RemoveCFlags`/`RemoveCxxFlags`/`RemoveLdFlags`/`RemoveDefines`/`RemoveIncludes`/`RemovePublicIncludes`/`RemoveLinks`/`RemoveProvidedLibs`/`RemoveDeps`、`SetLanguages`、`AddPostLink` 的参数、`AddPostLinkDeps`/`AddPostLinkOutputs`）可以 `[]string...` 展开，但不能传 `[]any`。

`AddPostLinkOutputs` 显式声明整个目标的后链接产物，支持 `{output}` 占位符；相对路径基于包的 SourceDir，绝对路径保持不变，主产物不会重复计入。非 Void 目标的任一声明产物缺失会触发重新链接并运行全部后链接步骤；`TargetVoid` 没有链接记录，后链接步骤每次构建都会执行，声明的产物缺失会直接报错。安装阶段只自动安装声明的额外产物，且主产物不存在时不会继续安装这些产物。`AddPostLinkHex/Bin/Strip` 自动声明各自产物，`AddPostLinkSize` 和 `SetSymbolPrefix` 不产生额外产物。

`AddPostLink` 的参数不再用于推断产物。已有自定义步骤需要补充输出声明，例如下面的 `.debug` 是第一步的输出，也是第二步的输入：

```go
ctx.Target("app").
    AddPostLink("objcopy", "--only-keep-debug", "{output}", "{output}.debug").
    AddPostLinkOutputs("{output}.debug").
    AddPostLink("objcopy", "--add-gnu-debuglink={output}.debug", "{output}")
```

`AddPublicIncludes` 支持 `@"pattern"` 作为最后一个参数进行 match。Pattern 应用到前面所有目录（省略目录默认为 `"."`）。Pattern 使用 `path.Match` 语法。

```go
// 安装所有 .h 文件到 dependents
ctx.Target("mylib").AddPublicIncludes("include")

// 只安装匹配 *.h 的文件
ctx.Target("mylib").AddPublicIncludes("include", "@*.h")

// 只匹配 foo*.h 到 src 目录
ctx.Target("mylib").AddPublicIncludes("include", "src", "@foo*.h")
```

### AddDeps 依赖引用格式

`AddDeps(targets ...string)` 的每个参数是一个依赖引用（`pkg/api/depref.go` `ParseDepRef`），支持 4 种格式：

| 格式 | 示例 | 含义 |
|------|------|------|
| 同包 target（不含 `:` 和 `/`） | `AddDeps("utils")` | 声明包自己的 `utils` target |
| 跨包 target | `AddDeps("lib:utils")` | `lib` 包的 `utils` target（构建顺序 + 链接 + PublicIncludes 传播） |
| 通配依赖 | `AddDeps("lib:*")`、`AddDeps("official/zlib:*")` | 该包所有 target + 传递依赖 |
| 包路径（含 `/` 不含 `:`） | `AddDeps("official/zlib")` | 该包所有 target + 传递依赖闭包 |

规则：

- **声明时校验**（fatal `*BuildScriptError`）：含空白字符、多个 `:`、`:` 前后任一段为空，以及不含 `:` 时首/尾 `/` 或 `//` 的畸形包路径。含 `:` 的引用不会在声明时校验包路径形态，畸形路径会在图构建时表现为 `dependency not found` 或 `package not found in build graph`。空字符串参数被 `AddDeps` 静默跳过，重复引用在图构建时去重。
- **子包短名**：当前包本身是子包且 `pkg:target` 的 pkg 部分不含 `/` 时，会先沿祖先链尝试相对解析（`ResolveSubPackageName`）——兄弟子包之间可写 `AddDeps("mylib:utils")` 而不必写全路径；顶层包不做这种相对解析。
- **图构建时报错**：target 不存在 → `dependency not found`；包不在构建图中 → `package not found in build graph`；循环依赖 → 错误（包引用递归由 `api.CheckCycle` 报告，target 级环由拓扑排序报告）。子包短名解析失败时错误信息附带已尝试的候选路径（`tried sub-package candidates: ...`）。
- 依赖边同时决定：链接输入（依赖 target 的产物路径）、PublicIncludes 传播、拓扑排序顺序。`TargetStatic`/`TargetObject` 不会把依赖的库合并进产物（`TargetStatic` 仍会归档依赖的可重定位 `.o`，`TargetObject` 不合并依赖），库依赖只提供排序和包含路径传播，构建时会打印相应 NOTE。

`api.ParseDepRef(ref string) (*api.DepRef, error)` 可单独解析引用，返回的 `api.DepRef` 含 `Raw`、`Pkg`、`Target`、`Wildcard`、`Qualified`；格式非法时返回 `error`，`AddDeps` 会把它转成 `*BuildScriptError`。

## 子包（Sub-Package）

native 远程包 checkout 内嵌套的 `build.go` 会被识别为**子包**：独立加载的包，
全名为 `父包全名/相对路径`（如 `official/mylib/sub_a`），拥有独立的 options/targets/build 目录，
但**版本完全跟随父包**（不单独进 lockfile/manifest）。完整设计见 `docs/DESIGN_DECISIONS.md`（DD-1~DD-3）。

要点：

- **发现**：父包（native 仓库）checkout 后自动扫描其中嵌套的 build.go（`Resolver.scanSubPackages`）；registry 包装包**没有**子包概念（DD-1，设计决策）。子包懒加载（DD-3）：只有被依赖时其 build.go 才被解释。
- **引用子包**：外部包用全名，如 `ctx.AddRequires("subtest/mother/sub_a")`、`AddDeps("subtest/mother/sub_a:*", "subtest/mother:base")`；同一 `AddRequires` 列表中父包必须写在子包之前。
- **子包互引短名**：子包内部引用兄弟子包时 pkg 部分可写短名（沿祖先链解析，见上文"子包短名"）。除 `AddDeps`/`AddRequires` 外，`DepOutput`/`DepBuildDir`、`BuildSubGraph` 和 `ImportConfig` 也接受同类短名。`api.SubPackageCandidates(currentPkg, depName, subParents)` 返回由近到远的候选全名，`api.ResolveSubPackageName` 按 `exists` 回调选择第一个存在的候选。
- **父包必须 needed**：若子包进入构建集合，其 native owner 也必须在 needed 集合中；否则 `RunBuild` 过滤依赖时（`Configure` 之后）报错提示补充父包依赖。这样父包补丁、版本锁定和源码树身份才能保持一致。
- **限制**：父包不能在自己的 `OnRequire` 中引用其子包（发现晚于依赖解析，见 DESIGN_DECISIONS.md 已知限制 5）；本地项目无子包概念（嵌套 build.go 是顶层裸名包，已知限制 1）。

示例见 `test_data/25_subpackage`（含本地 fixture native 仓）。

## 文件 IO 与工作目录（ScriptFS）

ScriptFS 包装的相对路径在 `pkg.SourceDir()` 已设置时以它为基准，否则以 **build.go 所在目录** 为基准（加载/发现阶段，以及本地 SetGit 源码尚未绑定到 `SourceDir` 时）。`OnBuild`/`OnInstall`/`OnClean` 回调执行期间，进程 cwd 是绑定后的 `SourceDir()`；`OnConfig` 在源码绑定前运行，cwd 是加载的 build.go 目录（registry 包为 wrapper checkout，native/local 通常与 `SourceDir()` 相同）。`TargetVoid` 的 `SetBuildFunc` 由调度器稍后调用，不经过这层 cwd 切换。远程包绑定到项目内该包唯一的工作树（`.vmake_deps/`），所有配置共享；补丁与配置会就地修改这棵树，不会写回上游。

```go
data, err := os.ReadFile("configs/app.conf")
os.WriteFile("generated.c", src, 0644)
wd, _ := os.Getwd()
cmd := exec.Command("git", "status")
```

覆盖范围：`os.Open/OpenFile/Create/ReadFile/WriteFile/Stat/Lstat/Mkdir/MkdirAll/Remove/RemoveAll/Rename/ReadDir/Getwd/TempDir/CreateTemp`、`filepath.Walk/WalkDir`、`exec.Command`（未显式设置 `Dir` 时）。绝对路径原样通过；`os.Getwd()` 返回 ScriptFS 基准目录。

限制：
- `os.Chdir` 返回错误——进程工作目录由回调执行边界管理；请用 `p.RunIn(dir, ...)` 或绝对路径
- 未包装的长尾 API 仍按进程 cwd 解析：`OnBuild`/`OnInstall`/`OnClean` 期间是 `SourceDir()`，`OnConfig` 是加载的 build.go 目录，`OnRequire`/`OnPackage`（加载阶段）以及 `SetBuildFunc` 等边界外代码则是 vmake 进程 cwd。例如 `exec.CommandContext`（仅 `exec.Command` 被包装）、`text/template.ParseFiles`、`io/ioutil`；请用绝对路径或 `filepath.Join(p.SourceDir(), ...)` 显式锚定
- 构建产物请继续用 `p.BuildDir()`；外部命令用 `p.Run/p.RunIn`（显式 Dir）

## RequireContext（依赖声明）

RequireContext 嵌入了 `ConfigAccessor`，因此 `Bool()`、`String()`、`If()`、`When()` 等方法均可使用（`OnRequire` 发现阶段的直接读取限制见上文）。

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

type Platform struct {
    OS     string
    Triple string
}
func (p Platform) OSOrHost() string

type DefaultFlags struct {
    CFlags, CxxFlags, LdFlags []string
}

type TargetKind string
const (
    TargetBinary TargetKind = "binary"
    TargetStatic TargetKind = "static"
    TargetShared TargetKind = "shared"
    TargetObject TargetKind = "object"
    TargetVoid   TargetKind = "void"      // 第三方包使用；SetBuildFunc 仅对该 kind 执行
)
func TargetFilename(kind TargetKind, name, targetOS string) string  // 按目标系统命名主产物

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

type Requires struct { ... }
func (r *Requires) Add(deps ...string)
func (r *Requires) AddInfos(infos ...RequireInfo)
func (r *Requires) Get() []RequireInfo
func (r *Requires) Reset()

type DepRef struct {
    Raw       string
    Pkg       string
    Target    string
    Wildcard  bool
    Qualified bool
}

type PostLinkStep struct {
    Tool string
    Args []string
}

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
| `>` | 大于，锁定 major | `>1.0.0` | `1.0.1` | `1.0.0`, `2.0.0` |
| `<=` | 小于等于，不锁定 major | `<=2.0` | `1.9.0`, `2.0.0` | `2.0.1` |
| `<` | 小于，不锁定 major | `<3.0` | `2.9.9` | `3.0.0` |
| `=` | 精确匹配（含 pre-release） | `=1.2.3` | `1.2.3` | `1.2.4` |
| `~` | 锁定 major.minor，patch >= | `~1.2.3` | `1.2.3`, `1.2.9` | `1.3.0`, `1.1.9` |
| （无） | 等同 `>=` | `1.2` | 同 `>=1.2` | |

#### Major 兼容性锁定

`>=` 运算符当 major > 0 时自动锁定同一 major 版本范围，确保只在兼容范围内选择版本：

- `>=1.2` → 只匹配 `1.x.x`（不匹配 `2.0.0`）
- `>=2.0` → 只匹配 `2.x.x`（不匹配 `3.0.0`，也不匹配 `1.9.9`）
- `>=0.0.0`（空约束）→ **不锁定**，匹配所有非 pre-release 版本（包括 `1.x`, `2.x`）

`>` 运算符与 `>=` 一样锁定 major（锚点 major > 0 时）：`>1.0.0` 只匹配 `1.x.x` 且大于 `1.0.0`（不匹配 `2.0.0`）。

`<=`, `<` 运算符**不锁定** major——用于跨版本范围比较。

#### Pre-release 规则

- 有 pre-release 的版本**低于**同版本无 pre-release 的：`1.0.0-rc.1 < 1.0.0`
- 带 pre-release 的版本只有在约束本身含 pre-release 且 major.minor.patch 相同时才会被选中：`>=1.4.0-rc.1` 可选中 `1.4.0-rc.1`；`>=1.0.0` 不会选中 `1.4.0-rc.1`
- Pre-release 按点号分段逐段比较：
  - 纯数字段按数值比较：`1.0.0-1 < 1.0.0-10`
  - 字符串段按字典序比较：`1.0.0-alpha < 1.0.0-beta`
  - **数字 < 字母**：`1.0.0-1 < 1.0.0-alpha`

#### 版本选择算法

1. 过滤满足**所有**约束的版本
2. 按 semver 降序排序
3. 返回**最高**的匹配版本

多约束为 AND 语义：版本必须同时满足**所有**约束（含各约束的 major 锁定），无交集时报错。`>=1.2` 和 `<1.5` 兼容（在交集中选最高），`>=1.0` 和 `>=2.0` 无交集（major 锁定冲突），报错。

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
| `--toolchain` | | 覆盖工具链 |
| `--mode` | | 覆盖构建模式（debug/release/size） |
| `--install` | `-i` | 构建后安装 |
| `--prefix` | `-p` | 安装前缀（默认 `./install/`）；项目内只允许默认 `install/` 及其子目录，其他项目内路径会被拒绝以避免清理源码；项目外自定义路径必须为空目录或带有上次 vmake 写入的 `.vmake-install` 标记，否则拒绝清理 |
| `--install-type` | | `runtime`（默认）或 `sdk` |
| `--manifest` | | 从清单文件固定版本 |
| `--tests` | | 包含测试目标 |
| `--jobs` | `-j` | 当前 target 的编译并行上限，同时约束 Make/CMake helper（0 = CPU 数，1 = 串行）；target 之间始终串行 |
| `--keep-going` | `-k` | 某目标失败后继续构建其他独立目标 |
| `--explain` | | 输出编译/链接目标重建原因 |

## 全局选项

内置全局选项（`pkg/api/global.go`）：

```go
const (
    ModeOptionName      = "mode"
    ToolchainOptionName = "toolchain"
    ModeDebug           = "debug"
    ModeRelease         = "release"
    ModeSize            = "size"
)
```

`BuiltInGlobalOptions` 暴露内置 `mode` 选项定义；`toolchain` 由 `MergeGlobalOptions` 按已安装工具链动态加入。`api.GroupGlobal`（`"Global"`，定义在 `pkg/api/option.go`）可用于 `SetGroup`：它会把通过 `ctx.Option` 声明的普通选项标记为全局选项；`ctx.GlobalOption` 是同一操作的简写。

项目平台选项（`pkg/api/platform.go`）由项目按需声明，用于交叉编译：

```go
const (
    TargetOSOptionName     = "target_os"
    TargetTripleOptionName = "target_triple"
)
```

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString)
    ctx.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString)
})
```

声明这两个选项用于在 TUI/`vmake config --set` 中可见和可覆盖，值本身可以留空：工具链 `toolchain.json` 中的 `target_os` / `target_triple` 默认值会填补未设置的值。`target_os` 决定产物命名、链接策略和 CMake 的 `CMAKE_SYSTEM_NAME`（`target_os=none` 映射为 `Generic`），空值取运行 vmake 的主机系统；`target_triple` 提供 `Configure` 的 `--host=` 与 `CMAKE_*_COMPILER_TARGET`。用户配置（`.vmake/` 当前生效配置文件的 `global.options` 或包级配置，默认文件为 `.vmake/config.json`）与项目声明的非空默认值均优先于工具链默认值。包内通过 `ctx.Option` 声明的非全局 `target_os`/`target_triple` 默认值优先于全局值（包级配置值仍最优先）；配置中的空字符串视为未设置，会落到下一级默认值，最终落到工具链默认值。

`mode` 选项自动添加编译标志：

| mode | cflags | defines |
|------|--------|---------|
| debug | `-O0 -g` | 无 |
| release | `-O2` | `NDEBUG` |
| size | `-Os` | `NDEBUG` |

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

全局选项在所有 Package 间共享。如果多个 Package 定义同名全局选项，类型和默认值必须一致，取值列表也须一致（内置 `mode`/`toolchain` 只校验类型和默认值，不比较 `SetValues`）；`SetMacroName` 允许只在部分声明中给出，但非空的宏名必须一致（声明按包名顺序确定性合并，`SetDescription`/`SetGroup` 等展示字段取第一个声明）。

### 全局选项自动导出 CONFIG_ 宏

通过 `ctx.GlobalOption(...)` 声明的自定义全局选项会自动导出为全局 `-D CONFIG_*` 宏，对构建中的每个包可见，值取当前配置（`.vmake/` 当前生效配置文件的 `global.options`，默认文件为 `.vmake/config.json`；未配置时取默认值）。修改配置后宏随之改变，并因进入全局 flags 哈希而使所有包的 BuildKey 失效，无需在 `SetOnApply` 里手写宏字符串。

| 类型 | 默认宏 | -D 内容 |
|------|--------|---------|
| Bool true | `CONFIG_<NAME>` | `-DCONFIG_<NAME>=1` |
| Bool false | 同上 | 不生成 |
| Int | 同上 | `-DCONFIG_<NAME>=<value>` |
| String | 同上 | `-DCONFIG_<NAME>="<value>"` |
| Choice | `CONFIG_<NAME>` + `CONFIG_<NAME>_<VALUE>` | 两条 |

Choice 的 `<VALUE>` 会转为大写并把 `-` 替换为 `_`。`SetMacroName(name)` 可覆盖宏名：普通名字整体替换默认名（Choice 仍会额外生成 `<自定义名>_<VALUE>`）；包含 `%s`/`%v` 时把选项值渲染进宏名（`SetMacroName("PY32F539xx%s")` + 值 `M` → `-DPY32F539xxM=1`），且只生成渲染后的宏。`SetMacroName` 仅对全局选项有效。

`mode`、`toolchain`、`target_os`、`target_triple` 默认不导出；重新声明该选项并设置 `SetMacroName` 即可导出（`mode`/`toolchain` 的重声明需与内置类型和默认值一致，宏名冲突时配置阶段报错）。两个全局选项生成同名宏但取值不同时，配置阶段报错；取值相同则去重。

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.GlobalOption("cpu_clock_hz").SetType(api.OptionInt).SetDefault(416000000)
    ctx.GlobalOption("mcu").SetType(api.OptionChoice).
        SetDefault("py32f539").SetValues("py32f539")
    ctx.GlobalOption("variant").SetType(api.OptionChoice).
        SetDefault("M").SetValues("G", "L", "M").
        SetMacroName("PY32F539xx%s")
})
```

`vmake config --set variant=L` 后，每个包获得：

```
-DCONFIG_CPU_CLOCK_HZ=416000000
-DCONFIG_MCU="py32f539" -DCONFIG_MCU_PY32F539=1
-DPY32F539xxL=1
```

这是全局投影，与包内 `GenerateConfigDefines` 的展开相互独立；依赖其他选项的复杂条件（如 `wifi || ble` 才定义某宏）仍可把两个选项导出为同名宏（同值去重），或使用 `SetOnApply`。

包级 entry（`entries.<pkg>.options`）里为全局选项保留的显式值在该包内仍然优先于全局值，但导出的宏始终使用全局值；把选项从包级迁移为全局时应删除旧的包级值。导出宏按所有已加载包声明的全局选项生成，不受 `FilterDeps` 裁剪影响（与全局选项本身的语义一致）。

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

`CopyFile` 拒绝把文件复制到自身，包括软链接和硬链接指向同一文件的情况；报错时保留原内容。`CopyDir` 拒绝源目录链接进入目标目录或其子目录。过滤器可以明确排除断链；未被排除的断链仍会报错。

### 包引用解析

```go
func SplitPackageRef(ref string) (repo, name string, ok bool)  // "official/zlib" -> ("official", "zlib", true)
```

### 构建模式标志

```go
func GetModeFlags(mode string) (cflags []string, defines []string)  // "debug" -> (["-O0","-g"], []); "release" -> (["-O2"], ["NDEBUG"]); "size" -> (["-Os"], ["NDEBUG"])
```

## 安装类型过滤

`--install-type` 控制 `vmake build --install` 安装哪些文件：

| 文件类型 | runtime | sdk |
|---------|---------|-----|
| binary → `bin/` | ✓ | ✓ |
| shared (.so/.dylib/.dll) → `lib/` | ✓ | ✓ |
| static (.a) → `lib/` | ✗ | ✓ |
| public includes → `include/` | ✗ | ✓ |
| object 目标产物 | ✗ | ✗ |
| AddInstalls 自定义文件 | ✓ | ✓ |

默认 `runtime`，只安装运行时所需文件（二进制和动态库）。`sdk` 还会安装静态库和公共头文件，适合需要二次开发的场景。测试目标和 object 目标从不安装；`TargetVoid` 通常由 `SetBuildFunc` 自行安装，只有构建目录中存在同名主产物时安装器才会继续处理该 target 及其声明的后链接产物。

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
func (p *Package) AddKConfig(name string) *KConfigEntry  // 创建 KConfigEntry（每包仅支持一个，重复调用 fatal）
func (p *Package) KConfigEntries() []*KConfigEntry
func (p *Package) SelectedPreset() string          // 返回选中的 preset 名（优先 selectedPreset，其次 defaultPreset）
func (p *Package) EnsureConfig(srcDir string) bool  // 检查 KConfig 配置（ConfigPath()，默认 ".config"）是否存在且非空，否则执行 make <preset> 并应用 patches（非默认 ConfigPath 时通过 KCONFIG_CONFIG 指向该文件）；返回 true 表示重新生成了配置；需要生成但未选择 preset、make 失败或补丁失败时 fatal（已有非空配置时不需要 preset）
```

### ConfigContext KConfig 方法

```go
func (ctx *ConfigContext) KConfig(name string) *KConfigEntry  // 创建 KConfigEntry（与 Package 关联；每包仅支持一个，重复调用 fatal）
```

### KConfigEntry

```go
// 获取方法
func (k *KConfigEntry) Name() string
func (k *KConfigEntry) Description() string
func (k *KConfigEntry) ConfigPath() string     // 默认 ".config"；TUI、恢复与 EnsureConfig 均使用该路径
func (k *KConfigEntry) SrcDir() string
func (k *KConfigEntry) Presets() []string
func (k *KConfigEntry) DefaultPreset() string
func (k *KConfigEntry) SelectedPreset() string
func (k *KConfigEntry) MenuconfigCmd() string
func (k *KConfigEntry) MenuconfigArgs() []string
func (k *KConfigEntry) Patches() map[string]string

// 设置方法（链式调用）
func (k *KConfigEntry) SetDescription(desc string) *KConfigEntry
func (k *KConfigEntry) SetConfigPath(path string) *KConfigEntry
func (k *KConfigEntry) SetSrcDir(dir string) *KConfigEntry
func (k *KConfigEntry) SetMenuconfigCmd(program string, args ...string) *KConfigEntry
func (k *KConfigEntry) AddPreset(name string) *KConfigEntry
func (k *KConfigEntry) SetDefaultPreset(presetName string) *KConfigEntry
func (k *KConfigEntry) SelectPreset(name string) *KConfigEntry
func (k *KConfigEntry) SetKConfigPatches(patches map[string]string) *KConfigEntry
```

`SetMenuconfigCmd` 分别接收可执行程序和参数，例如 `SetMenuconfigCmd("C:/Program Files/Kconfig/menu.exe", "--config", "project config")`。命令在 `SrcDir` 下执行，参数不经过 shell 拆分。未设置时运行所选工具链的 `make menuconfig`；生成 preset 始终使用所选工具链的 make，与自定义 menuconfig 命令无关。当 `ConfigPath` 非默认（`.config`）时，vmake 通过 `KCONFIG_CONFIG` 环境变量把目标配置文件位置传给预设生成、menuconfig 与 `EnsureConfig`；自定义 menuconfig 命令需要遵循该变量，否则修改不会写回目标文件。

TUI 仅在需要生成 preset 或运行默认 menuconfig 时解析 make。已有非空配置，或没有需要生成的 preset 时，自定义 menuconfig 程序无需安装 make。普通 C/C++ 构建也不要求默认 make；工具链中显式配置的 MAKE 仍会严格校验。

### 工具函数

```go
func ApplyKConfigPatches(configPath string, patches map[string]string) error
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
            SetDefaultPreset("evk_rk3568_defconfig").
            SetKConfigPatches(map[string]string{
                "CONFIG_LOCALVERSION=\"-custom\"": "CONFIG_LOCALVERSION=\"-myboard\"",
            })
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("uboot").
            SetKind(api.TargetVoid).
            SetBuildFunc(func(pkg *api.Package) error {
                srcDir := pkg.SrcDir()
                pkg.EnsureConfig(srcDir) // ConfigPath() 配置不存在或为空时执行 make <preset> 并应用 patches
                return pkg.Make("-C", filepath.ToSlash(srcDir))
            })
    })
}
```

## 配置导出

VMake 支持两种将配置选项导出给 C 代码的方式。两种方式独立使用，按需选择。

### GenerateConfigHeader — 生成 autoconf.h

在 `OnBuild` 中调用 `ctx.GenerateConfigHeader()`，vmake 会在构建目录的 `generated/autoconf.h` 中生成类似 Linux 内核的配置头文件，并自动将 `generated/` 加入该包所有目标的包含路径。

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

#define CONFIG_BUFFER_SIZE 4096
#define CONFIG_DEVICE_NAME "uart0"
#define CONFIG_FEATURE_FOO 1
#define CONFIG_PLATFORM "linux"
#define CONFIG_PLATFORM_LINUX 1

#endif
```

选项按名称排序生成，未声明的选项不会出现在输出中；Bool 选项为 false 时写成 `/* #undef CONFIG_<NAME> */`。

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
-DCONFIG_BUFFER_SIZE=4096 -DCONFIG_DEVICE_NAME="uart0"
-DCONFIG_FEATURE_FOO=1 -DCONFIG_PLATFORM="linux" -DCONFIG_PLATFORM_LINUX=1
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

宏名称规则：`CONFIG_` + 选项名大写 + `-` 替换为 `_`。全局选项不参与本包生成：自定义全局选项由全局导出机制统一生成，见「全局选项自动导出 CONFIG_ 宏」。

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

当前实现中 `ExportConfig()` 只设置标志，`ImportConfig` 按名称直接合并、不检查对方是否声明导出；只有 `GenerateConfigDefines`（或 `SyncConfigDefines`）才会注入 `-D`。同时调用 `GenerateConfigHeader` 时，导入的选项也会合并进本包视角的 `autoconf.h`。

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
// "zlib" -> ("", "zlib", false)
```

用于解析 `repo/name` 格式的包引用；不含 `/` 时 `ok=false`，`name` 为原字符串。

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
            SetTest(true)  // 测试目标：vmake build 默认跳过，--tests / vmake test 时才构建
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
            // use: if !ctx.When("debug", true) { target.AddCFlags("-O2") }
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
                pkg.CMakeBuild()
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
