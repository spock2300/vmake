# VMake 架构文档

## 运行时执行流程

vmake build 执行三个阶段（含延迟解析子阶段）：

```
Phase 1: OnRequire
    扫描 build.go → 编译构建脚本 → 加载构建脚本 → 收集依赖
    │
    [afterPhase1 钩子：manifest 版本锁定]
    │
Phase 2: 配置准备 (runPostPhase1)
    ├── 2a: ResolveDeferred — 解析延迟依赖（远程包）→ 更新拓扑排序
    └── 2b: OnConfig — 执行 OnConfig 回调 → 收集 Option 定义 → 合并全局选项
    │
Phase 3: OnBuild (runBuildPhase)
    ├── resolveBuildConfig — 解析构建配置（模式、工具链）
    ├── filterAndCollectNeeded — BFS 过滤本地包依赖
    ├── resolveAllPackageDirs — 解析所有包目录
    ├── prepareAllPackages — 下载源码、编译 buildscript 插件
    ├── applyPatches — 对远程包应用 git patch（git apply --3way）
    ├── restoreKConfigFiles — 恢复 KConfig 配置
    ├── executeAllOnBuild — 执行 OnBuild 回调 → 生成 Target
    ├── NewBuildGraph — 构建依赖图 → 拓扑排序
    ├── BuildPipeline.Run — 统一编排调度器
    │   └── NewScheduler → scheduler.BuildAll()
    │       └── ForEachDefault:
    │           ├── resolveTarget（解析 include/define/flag/dep/genrule）
    │           ├── runGenRules（二进制头文件生成等）
    │           ├── generateConfigHeader（可选配置头文件）
    │           ├── realizePrebuilt（预编译产物 symlink）
    │           ├── compile（并行编译源文件）
    │           ├── link（链接目标）
    │           ├── postLink（objcopy/size/strip 等后处理）
    │           └── publishTarget（发布产物到 InstallDir）
    ├── 生成 compile_commands.json
    │
    [afterBuild 钩子：test 命令执行测试二进制]
    │
(Optional) Install
    清理安装前缀 → 执行 OnInstall 回调 → dry-run OnBuild 收集安装项
    → ArtifactInstaller.InstallAll → 安装目标产物 → 生成 manifest.json
```

### Clean Pipeline

```
vmake clean
│
├── Phase 1-2b: 同 build（OnRequire → OnConfig）
│
├── Phase 3: OnClean
│   └── 对每个包执行 OnClean 回调（自定义清理逻辑，如 make clean）
│
└── Directory Cleanup
    └── 删除构建产物目录（build/、out/）
    
Fallback: 若插件加载失败，降级为仅扫描目录清理（不执行 OnClean 回调）
```

### Build Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--force` | `-f` | 强制重新编译构建脚本 |
| `--toolchain` | | 覆盖工具链 |
| `--mode` | | 覆盖构建模式（debug/release） |
| `--install` | `-i` | 构建后安装 |
| `--prefix` | `-p` | 安装前缀（默认: `./install/`） |
| `--install-type` | | 安装类型: `runtime`（默认）或 `sdk` |
| `--manifest` | | 从 manifest 文件锁定版本 |
| `--tests` | | 包含测试目标 |

### Install Type 过滤

`--install-type` 控制安装内容：

| 文件类型 | runtime | sdk |
|---------|---------|-----|
| binary → `bin/` | ✓ | ✓ |
| shared (.so) → `lib/` | ✓ | ✓ |
| static (.a) → `lib/` | ✗ | ✓ |
| public includes → `include/` | ✗ | ✓ |
| AddInstalls 自定义文件 | ✓ | ✓ |

### Install Manifest

`--install` 在安装前缀生成 `<prefix>/manifest.json`，记录构建元数据和每个包的信息：

```json
{
  "vmake": "0.x.x",
  "toolchain": "gcc",
  "mode": "debug",
  "generated": "2026-03-29T01:55:53Z",
  "packages": [
    {
      "name": "myapp",
      "version": "v1.0.0-2-g3a4b5c6",
      "source": "local",
      "ref": "3a4b5c6789abcdef...",
      "path": "."
    },
    {
      "name": "test_build/mathlib",
      "version": "2.1.0",
      "source": "native",
      "url": "https://gitee.com/.../test_build_mathlib.git",
      "ref": "v2.1.0"
    }
  ]
}
```

- 本地包: `source: "local"`，版本来自 `git describe`，`ref` 来自 `git rev-parse HEAD`（完整哈希），`path` 相对于 cwd
- Native 包: `source: "native"`，`url` 来自 `NativeGitURL`，`ref` 来自 `NativeVersions`
- Registry 包: `source: "registry"`，`url` 来自首个 `GitURLs()`，`ref` 来自 `Versions()`

CLI: `vmake manifest show <path>` / `vmake manifest checkout <path> [name]`

### Phase 1: 构建脚本扫描与依赖解析

```
Scan(root)          Compile             Load              Resolve
递归扫描 build.go   编译为 .so          加载构建脚本      解析依赖树
    │                   │                  │                 │
    ▼                   ▼                  ▼                 ▼
[]Source           build.so         LoadedScript      Graph
                                      ├─ pkg   *Package  ├─ Order []
                                      ├─ Source          └─ Packages map
                                      └─ Script *plugin.Plugin
                                                           └─ *PackageNode
```

1. `buildscript.Scan(root)` 递归扫描 `build.go`，返回 `[]buildscript.Source`
2. `buildscript.Compile()` 编译为 `.so`，缓存在 `vmake_deps/<pkg>/build.so`
3. `buildscript.Load()` 加载 `.so`，调用 `Main(*api.Package)` 获取 `*api.Package`
4. `resolver.Resolver` 递归解析依赖，生成 `Graph`（拓扑排序）
5. `Resolver.ResolveDeferred()` 解析远程（延迟）依赖

远程包在 Phase 3 源码下载后、构建前会自动应用 git patch（`git apply --3way`），已应用的 patch 会被跳过。

源码：`pkg/buildscript/scanner.go`, `compiler.go`, `loader.go`, `pkg/resolver/resolver.go`

### Phase 2: 配置收集

```
OnConfig 回调 ──▶ 收集 Option 定义 ──▶ 合并全局选项
```

1. 执行所有 `OnConfig` 回调，收集 `Option` 定义
2. `ConfigAccessor.MergeGlobals` 合并全局选项（内置 `mode` + `toolchain` + 用户定义）作为回退
3. 配置值在后续 TUI 或 CLI flag 中加载

源码：`cmd/vmake/root.go`, `pkg/api/global.go`

**ConfigContext 方法**（`pkg/api/context.go`）：
- `GlobalOption(name)` — 获取全局选项值
- `GlobalMode()` — 获取全局构建模式
- `Toolchains()` — 获取可用工具链列表
- `ToolchainOption()` — 创建工具链选择选项（自动填充可用工具链）
- `SetConfigValue(name, val)` — 设置配置值
- `AddGlobalCFlags(flags...)` — 添加全局 C 编译器标志（仅在 OnApply 回调中生效）
- `AddGlobalCxxFlags(flags...)` — 添加全局 C++ 编译器标志（仅在 OnApply 回调中生效）
- `AddGlobalLdFlags(flags...)` — 添加全局链接器标志（仅在 OnApply 回调中生效）
- `SetProvidedLinkerScript(path)` — 声明链接脚本供消费者 target 使用（重复调用触发 Fatal）
- `KConfig(name)` — 声明 KConfig 条目（委托给 `Package.AddKConfig`）

### Phase 3: 构建执行

`runBuildPhase` 包含多个子步骤：

1. **resolveBuildConfig** — 解析构建模式（debug/release）和工具链选择
2. **filterAndCollectNeeded** — 从 `IsLocal()` 根节点 BFS 遍历，过滤需要构建的包
3. **resolveAllPackageDirs** — 解析所有包的 SourceDir/BuildDir/InstallDir
4. **prepareAllPackages** — 下载远程包源码，编译 buildscript 插件
5. **applyPatches** — 对远程包应用 git patch（`git apply --3way`），已应用自动跳过
6. **restoreKConfigFiles** — 从 config.json 恢复 KConfig 配置（详见 KConfig 章节）
7. **executeAllOnBuild** — 执行所有 `OnBuild` 回调，生成 `map[string]*Target`
8. **build.NewBuildGraph** — 构建依赖图，`BuildGraph` 展开包级依赖为 target 级传递闭包
9. **build.NewBuildPipeline** — 创建 `BuildPipeline`（封装图、工具链、包目录、模式、选项、调度器）
10. **pipeline.Run()** → `NewScheduler(compiler, linker, cache)` → `BuildAll()`:
    - `ForEachDefault` 按拓扑顺序构建每个默认 Target
    - 每个 Target: resolveTarget → runGenRules → generateConfigHeader → realizePrebuilt → compile → link → postLink → publishTarget
    - 并行编译源文件（`compileWorker`）
11. **生成 compile_commands.json**（通过 `CompileCommandsWriter`）

**BuildContext 方法**（`pkg/api/context.go`）：
- `Exec(name, args...)` — 构建阶段执行命令（vlog.Fatal 退出）
- `BuildSubGraph(pkgName)` — 将包及其依赖作为独立子图构建
- `DepOutput(depRef)` — 获取依赖目标输出文件路径
- `DepBuildDir(depRef)` — 获取依赖构建目录
- `GenerateConfigHeader()` — 启用配置头文件自动生成
- `GenerateConfigDefines()` — 启用配置宏定义自动生成
- `ExportConfig()` — 设置配置导出标志
- `ImportConfig(names...)` — 注册可导入的包名
- `SyncConfigDefines(names...)` — 快捷方式：`GenerateConfigDefines` + `ImportConfig`
- `SetDryRun(v bool)` — 设置为 dry-run 模式（安装阶段使用）

**BuildPipeline**（`pkg/build/pipeline.go`）：
```
BuildPipeline
├── Graph              *BuildGraph
├── PkgDirs            map[string]*api.PkgDirs
├── Toolchain          *toolchain.Toolchain
├── Mode               string
├── Options            map[string]map[string]any
├── Packages           map[string]*api.Package
└── BuildKeyOverrides  map[string]string
```

源码：`pkg/build/scheduler.go`, `pkg/build/graph.go`, `pkg/build/pipeline.go`, `pkg/build/stamp.go`

## 第三方包流程

```
OnRequire          Resolver            SourceManager       Scheduler
声明依赖           解析依赖树           下载源码            构建安装
    │                 │                    │                  │
    ▼                 ▼                    ▼                  ▼
AddRequires      Graph                vmake_deps/<repo>/  TargetVoid.BuildFunc()
"official/zlib"  ├─ Order []          <pkg>/src/           → CMakeConfigure
                   └─ Packages map                        → CMakeBuild
                    └─ *PackageNode                        → CMakeInstall
```

1. `OnRequire` 回调调用 `AddRequires("official/zlib >=1.2")`
2. `Resolver` 在 `repos/` 中查找包定义，递归解析依赖
3. `SourceManager.EnsureSource` 通过 git clone 下载源码到 `vmake_deps/<repo>/<pkg>/src/`
4. `Scheduler` 按拓扑顺序构建所有目标，包括 `TargetVoid` 目标

对于 `TargetVoid` 类型的目标（第三方包），Scheduler 调用 `Target.BuildFunc()` 并传入 `*api.Package`，执行 CMake/Autotools 等构建命令。

源码：`pkg/resolver/resolver.go`, `pkg/repo/source.go`, `pkg/build/scheduler.go`

## Native 仓库流程

Native 仓库是 VMake 原生的包生态系统，用于跨项目共享包。每个包是一个独立的 Git 仓库，`build.go` 位于仓库根目录。

```
OnRequire            Resolver.findNativeSource          Phase 2a              Scheduler
声明依赖             解析 native 源                      编译 build.go         构建
    │                      │                                  │                  │
    ▼                      ▼                                  ▼                  ▼
AddRequires          1. 检查 registry 仓库（未找到）      编译 build.so        同本地包
"myorg/lib >=1.0"    2. 识别 native 仓库                   加载插件
                      3. 解析 URL 模板 → clone/fetch      发现依赖
                      4. git tag → filter semver
                      5. 选择版本 → checkout
                      6. 注册 PackageNode（含 native 字段）
```

### 两种仓库类型对比

| | Registry 仓库 | Native 仓库 |
|--|--|--|
| **用途** | 包装第三方 C/C++ 库（zlib、curl） | VMake 原生包，跨项目共享 |
| **build.go** | 包装器（调用 CMake 等） | 真正的构建描述（与本地项目相同） |
| **源码位置** | build.go 在 registry 仓库中，源码在别处 | build.go 在包的 git 仓库根目录 |
| **版本来源** | `AddVersion()` 手动映射 | git tag（自动过滤有效 semver） |
| **版本选择时机** | Phase 3（build.go 编译后） | Phase 1（build.go 编译前 — 需先 clone） |
| **添加命令** | `vmake repo add name url` | `vmake repo add --native name "https://..../{name}.git"` |
| **更新** | `vmake repo update name` | `vmake pkg update repo/name` |
| **搜索** | 列出仓库中所有包 | 仅显示已缓存的包 |

### Native 源码解析流程 (`findNativeSource`)

1. `findSource` 先检查 registry 仓库（`FindPackageGo`），未找到再检查 native
2. 解析 URL 模板（`{name}` → 包名）
3. `repo.EnsureRepoAtRef(gitURL, repoDir, "")` 确保 clone/fetch
4. `repo.ListTags(repoDir)` → `FilterValidVersions`（过滤有效 semver）
5. `SelectNativeVersion`（按约束选择最高匹配版本）
6. `repo.EnsureRepoAtRef(gitURL, repoDir, selectedRef)` checkout 到选中 tag
7. 在仓库根目录查找 `build.go`
8. 创建 `PackageNode`，注册到 `graph.Packages`（含 `Native *NativePackageInfo`）
9. Phase 2a `resolveDeferredNode` 编译/加载 build.go，保留 native 字段

### PackageNode Native 字段

```go
type PackageNode struct {
    ID          string
    Source      *buildscript.Source
    Pkg         *api.Package
    Deps        []string
    Deferred    bool
    Native      *NativePackageInfo
    Constraints []string
}

type NativePackageInfo struct {
    GitURL   string            // 解析后的 git URL
    Versions map[string]string // version_string → git_tag
    Selected string            // 选中的版本号
}
```

源码：`pkg/resolver/resolver.go`, `pkg/repo/native.go`, `pkg/repo/manager.go`

## CLI 命令树

```
vmake (RootCmd)
├── build          # 构建项目
├── clean          # 执行 OnClean 钩子后清理构建产物
├── rebuild        # 完全重新构建
├── distclean      # 深度清理（删除所有构建产物、build.so、vmake_deps/、install/）
├── config         # TUI 配置界面
├── update [ver]   # 自我更新（go install）
├── version        # 版本信息
├── toolchain      # 工具链管理
│   ├── list       # 列出工具链
│   └── show       # 显示详情
├── repo           # 包仓库管理
│   ├── add --native  # 添加 native 仓库（URL 模板，含 {name} 占位符）
│   ├── add            # 添加 registry 仓库
│   ├── remove     # 删除仓库
│   ├── list       # 列出仓库（显示 registry/native 类型）
│   └── update     # 更新仓库（native 仓库提示使用 pkg update）
├── pkg            # 包管理
│   ├── list       # 列出已安装包
│   ├── search     # 搜索包
│   ├── clean      # 清理包缓存
│   └── update     # 更新包源码
├── ext            # 扩展仓库管理
│   ├── add        # 添加扩展仓库
│   ├── remove     # 删除扩展仓库
│   ├── list       # 列出扩展和插件
│   └── update     # 更新扩展仓库
├── git
│   └── tag        # Git 标签操作（支持版本号自动递增）
├── query          # 显示依赖树
├── manifest       # 安装清单管理
│   ├── show       # 显示清单内容
│   └── checkout   # 按记录版本 checkout
├── skill          # AI skill 管理
│   ├── install    # 安装 AI skill
│   ├── uninstall  # 卸载 AI skill
│   └── path       # 显示安装路径
├── completion     # 生成 shell 补全脚本
│   ├── bash       # Bash 补全
│   ├── zsh        # Zsh 补全
│   ├── fish       # Fish 补全
│   ├── powershell # PowerShell 补全
│   └── install    # 自动安装补全
├── test           # 构建并运行测试目标
└── <plugin>       # 扩展插件提供的命令
    └── ...        # 插件自定义子命令
```

全局选项：`-v` (verbose), `-V` (very verbose), `-q` (quiet)

源码：`cmd/vmake/`（`distclean.go`, `completion.go`）

## 统一依赖系统

VMake 使用 `AddDeps` 统一管理所有依赖类型：

| 类型 | 示例 | 识别规则 |
|------|------|---------|
| 同包 target | `AddDeps("utils")` | 不含 `:` 和 `/` |
| 跨包 target | `AddDeps("lib:utils")` | 含 `:`，指定具体 target |
| 通配依赖 | `AddDeps("lib:*")` 或 `AddDeps("official/zlib:*")` | `:*` 结尾，展开为该包所有 target + 传递依赖 |
| 第三方包 | `AddDeps("official/zlib")` | 含 `/`，展开为该包所有 target + 传递依赖 |

### 解析流程

`BuildGraph` 构建时，包引用（`repo/pkg`、`pkg:*`、`repo/pkg:*`）被自动展开：

1. 查找该包的所有 target 节点，添加为直接依赖
2. 递归展开该包的传递依赖（来自 `resolver.Graph` 中的 `PackageNode.Deps`）
3. 结果：每个 target 的 `Deps` 是扁平的传递闭包，包含所有直接和间接依赖的 target

```
Target.AddDeps("official/zlib")        Target.AddDeps("official/curl:*")
         │                                       │
         ▼                                       ▼
    展开 zlib targets                     展开 curl targets
    + zlib 传递依赖                        + curl 传递依赖 (zlib, ssl)
         │                                       │
         ▼                                       ▼
    BuildGraph (统一拓扑排序)              同一个 BuildGraph
         │                                       │
         ▼                                       ▼
    统一 resolveTarget 循环               PublicIncludes / artifact path / install dir
```

### 两阶段生命周期

依赖声明仍在两个不同阶段完成：

| 阶段 | API | 职责 |
|------|-----|------|
| Phase 1 (OnRequire) | `RequireContext.AddRequires()` | 声明包级别需求，触发源码下载，构建 `resolver.Graph` |
| Phase 3 (OnBuild) | `Target.AddDeps()` | 将包引用关联到具体 target，`BuildGraph` 展开为 target 级依赖 |

`resolver.Graph` 仍负责包级别的源码获取和版本管理，`BuildGraph` 负责统一的构建排序和依赖注入。

## 核心数据结构

### resolver.Graph (`pkg/resolver/resolver.go`)

```
Graph
├── Order    []string                        // 拓扑排序后的包名列表
└── Packages map[string]*PackageNode

PackageNode
├── ID          string
├── Source      *buildscript.Source
├── Pkg         *api.Package
├── Deps        []string
├── Deferred    bool
├── Native      *NativePackageInfo
└── Constraints []string          // 版本约束列表（如 [">=1.2", "<2.0"]）
    ├── GitURL   string            // native 仓库：解析后的 git URL
    ├── Versions map[string]string // native 仓库：version_string → git_tag
    └── Selected string            // native 仓库：选中的版本号
```

### buildscript.Source (`pkg/buildscript/source.go`)

```
Source
├── Path      string          // build.go 文件路径
├── Name      string          // 包名（如 "official/zlib"）
├── Dir       string          // 包目录
├── OutputDir string          // 输出目录
├── Origin    api.SourceOrigin // SourceLocal 或 SourceRemote
└── Force     bool
```

### BuildGraph (`pkg/build/graph.go`)

```
BuildGraph
├── Nodes map[string]*BuildNode              // "pkg:target" → Node
└── Order []string                           // 拓扑排序结果

BuildNode
├── FullName string                          // "pkg:target"
├── PkgName  string
├── Target   *api.Target
└── Deps     []string                        // 统一依赖列表（含展开的第三方包 target）
```

`BuildGraph` 提供辅助方法：
- `GetNode(name) (*BuildNode, error)` — 按 `pkg:target` 全名查找节点
- `ForEachDefault(fn func(*BuildNode) error) error` — 遍历所有默认目标

### PkgBuildMeta (`pkg/build/graph.go`)

`BuildGraph` 在展开包级依赖时使用 `PkgBuildMeta` 记录每个包的信息：

```
PkgBuildMeta
├── Origin api.SourceOrigin    // 包来源（本地/远程）
└── Deps   []string            // 包级依赖列表
```

### ConfigFile (`pkg/config/store.go`)

```
ConfigFile
├── Version  string
├── Global   *GlobalConfig
└── Entries  map[string]*EntryConfig

GlobalConfig
├── Toolchain string         // 默认工具链
├── Mode      string         // 默认构建模式（debug/release）
└── Options   map[string]any // 全局选项回退值

EntryConfig
├── Version        string                  // 第三方包的版本（可选）
├── Options        map[string]any          // 配置选项
├── KConfig        string                  // KConfig 配置内容
└── SelectedPreset string                  // 选中的 KConfig preset 名称
```

### Package 附加方法

`Package` 提供的额外方法（用于构建脚本）：

- `AddPatches(paths...)` — 添加 git patch 文件（相对于 SourceDir），远程包在构建前自动应用
- `SetPatches(paths...)` — 设置 git patch 文件（覆盖）
- `SetGenConfigHeader(v bool)` — 启用或禁用配置头文件自动生成
- `GenConfigHeader()` — 获取配置头文件生成开关状态
- `SetScriptDir(dir)` — 设置构建脚本目录
- `SetOutputDir(dir)` — 设置输出目录
- `SetCfgVals(vals)` — 设置配置值
- `SelectVersionMulti(constraints)` — 多约束版本选择
- `SelectedPreset()` — 返回已选中的 KConfig preset 名称
- `ApplyKConfigPatches(configPath, patches)` — 对 `.config` 文件应用字符串替换补丁

源码：`pkg/api/package.go`

## 关键文件位置

| 组件 | 文件路径 | 职责 |
|------|----------|------|
| API 定义 | `pkg/api/` | 公共 API，构建脚本可导入 |
| 插件系统 | `pkg/plugin/` | 扩展插件管理、编译、加载 |
| 构建脚本系统 | `pkg/buildscript/` | 扫描、编译、加载构建脚本 |
| 依赖解析 | `pkg/resolver/` | 依赖图解析、拓扑排序 |
| 构建系统 | `pkg/build/` | 编译、链接、调度、安装 |
| 包管理 | `pkg/repo/` | 仓库管理、源码下载、安装、native 仓库 |
| 工具链 | `pkg/toolchain/` | GCC/Clang 抽象 |
| 配置 | `pkg/config/` | 配置文件读写 |
| 日志 | `pkg/log/` | 日志输出 |
| TUI | `pkg/tui/` | 终端界面 |
| 版本 | `pkg/version/` | 版本信息 |
| CLI | `cmd/vmake/` | 命令行入口 |
| JSON I/O | `internal/jsonio/` | JSON 序列化工具 |
| 命令执行 | `internal/exec/` | OS 命令执行 |
| 文件匹配 | `internal/glob/` | Glob 模式匹配 |
| 文件系统 | `internal/fs/` | 文件/目录操作工具 |
| Git 仓库 | `internal/gitstore/` | 通用 Git 仓库管理（Add/Remove/List） |
| Go 编译 | `internal/gocompile/` | Go 插件编译工具 |

## KConfig 系统

VMake 内置 KConfig 配置管理，用于 Linux 内核、U-Boot 等 Kconfig-based 项目的配置流程。

### KConfigEntry

```go
type KConfigEntry struct {
    name, description, configPath, srcDir, menuconfigCmd string
    presets        []string
    defaultPreset  string
    selectedPreset string
    patchValues    map[string]string
}
```

流式 API：

- `SetDescription(desc)` — 设置描述
- `SetConfigPath(path)` — 设置 .config 路径
- `SetSrcDir(dir)` — 设置源码目录
- `SetMenuconfigCmd(cmd)` — 设置 menuconfig 命令
- `AddPreset(name)` — 添加 preset（defconfig 文件名）
- `SetDefault(presetName)` — 设置默认 preset
- `SetSelectedPreset(name)` — 设置选中 preset
- `PatchKConfig(patches)` — 设置 post-defconfig 补丁（`map[string]string`）

### 声明与配置

在 `OnConfig` 中通过 `Package.AddKConfig(name)` 声明 KConfig 条目，TUI 会列出可用 preset 供用户选择。

### EnsureConfig 抽象

`Package.EnsureConfig(srcDir) bool` 是 KConfig 构建的核心抽象：

1. 检查 `.config` 是否存在且大小 > 0 → 如果有效，返回 `false`（无需重新生成）
2. 执行 `make <selectedPreset>` 生成 `.config`
3. 应用 `PatchKConfig` 中定义的 post-defconfig 补丁（字符串替换）
4. 返回 `true`（已重新生成配置）

### ApplyKConfigPatches

`Package.ApplyKConfigPatches(configPath, patches)` 是独立的导出函数，对 `.config` 文件应用字符串替换补丁：

- 读取 `.config` 文件
- 对 `patchValues` 中的每对 `key: value` 执行字符串替换
- 写回 `.config` 文件

被 `EnsureConfig` 和 `restoreKConfigFiles` 共同调用。

### restoreKConfigFiles 跳转规则

`restoreKConfigFiles` 从 `config.json` 恢复 KConfig 配置，遵循以下规则：

- `config.json` 中没有该包的 entry → 跳过（不删除 `.config`）
- `config.json` 中有 entry 但 kconfig 为空（preset 切换） → 删除 `.config`
- `config.json` 中有 kconfig 内容 → 仅在内容变化时写回（避免 mtime 更新导致 stamp 失效）
- KConfig 内容为空 → 不写入

### Stamp-Based Skip（TargetVoid）

本地包（无 `InstallDir`）使用 `.vmake_stamp` 跳过已构建目标：

- 构建完成后在 `BuildDir` 写入 `.vmake_stamp`
- 下次构建时检查 stamp 是否存在
- 通过 `SetConfigFiles()` 声明的配置文件比 stamp 新时，判定为 stale，重新构建
- 配置文件不存在也判定为 stale

### autoWireRequireDeps

`OnRequire`/`AddRequires` 仅声明包级需求，不会自动创建构建图边。`Target.AddDeps()` 是将包引用关联到具体 target 的必要步骤。

当 target 没有显式调用 `AddDeps()`，但包通过 `AddRequires` 声明了依赖时，`autoWireRequireDeps()` 会自动将依赖包的所有 target 作为当前 target 的依赖，建立构建图边。

源码：`cmd/vmake/build_cmd.go`（`autoWireRequireDeps`）

## GenRule 系统

Target 支持 `AddBinHeader(inputs ...any)` 方法，创建 `GenRuleBinHeader` 类型的 GenRule。GenRule 在构建阶段由 scheduler 处理，将输入文件作为二进制头文件嵌入到目标中。

```go
ctx.Target("app").AddBinHeader("assets/logo.bin")
```

源码：`pkg/api/genrule.go`, `pkg/api/target.go`

## 后链接步骤系统

Target 支持在链接后执行自定义后处理步骤，用于嵌入式/RTOS 场景的二进制格式转换和分析。

### PostLinkStep

```go
type PostLinkStep struct {
    Tool string   // 工具名（如 "objcopy"、"size"）
    Args []string // 参数模板（{input} / {output} 占位符）
}
```

### API 方法

- `AddPostLink(tool, args...)` — 添加自定义后链接步骤
- `AddPostLinkHex()` — 添加 `objcopy -O ihex` 生成 .hex 文件
- `AddPostLinkBin()` — 添加 `objcopy -O binary` 生成 .bin 文件
- `AddPostLinkSize()` — 添加 `size {output}` 显示段大小
- `AddPostLinkStrip()` — 添加 strip 去除调试符号

### 执行流程

在 `Scheduler.buildTarget` 中，链接完成后执行所有 `PostLinkStep`：

1. 替换 `{input}` 为链接输出路径，`{output}` 为推导的输出路径
2. 执行每个步骤的工具命令
3. `PostLinkStep` 的输出也会被 `publishTarget` 和 `installTarget` 处理

```go
ctx.Target("firmware").SetKind(api.TargetBinary).AddFiles("src/*.c")
    .AddPostLinkHex()
    .AddPostLinkSize()
```

源码：`pkg/api/target.go`, `pkg/build/scheduler.go`

## 扩展系统

VMake 支持通过 Go 插件扩展 CLI 命令。扩展仓库是包含一个或多个插件的 Git 仓库。

### 扩展仓库结构

```
~/.vmake/extensions/
└── <repo-name/>                  # 扩展仓库名
    ├── <plugin-name/>            # 插件目录（成为 vmake 子命令）
    │   ├── plugin.json           # 插件元信息
    │   └── src/main.go           # 插件入口
    ├── <toolchain-name/>         # 工具链声明
    │   └── toolchain.json        # 工具链定义
    └── assets/toolchains/        # 工具链压缩包（可选，Git LFS）
        └── *.tar.gz              # 工具链二进制包
```

### 插件加载流程

```
vmake 启动
    │
    ▼
plugin.Manager.DiscoverPlugins()  ──▶ 扫描 extensions/*/
    │
    ▼
plugin.Compile()                     ──▶ 编译 main.go → .so
    │
    ▼
plugin.Load()                        ──▶ 加载 .so，调用 Main(ctx)
    │
    ▼
ctx.AddSubCommand()               ──▶ 注册 cobra.Command
    │
    ▼
RootCmd.AddCommand(pluginCmd)     ──▶ 添加到 CLI 命令树
```

### 插件 API

```go
package main

import (
    "gitee.com/spock2300/vmake/pkg/plugin"
    "github.com/spf13/cobra"
)

func Main(ctx *plugin.Context) {
    ctx.AddSubCommand(&cobra.Command{
        Use:   "mycommand",
        Short: "Command description",
        Run:   runMyCommand,
    })
}
```

**Context 方法**（`pkg/plugin/api.go`）：
- `AddSubCommand(cmd)`: 注册子命令
- `RegisterToolchain(name, tc)`: 注册工具链
- `GetToolchains()`: 获取已注册工具链
- `SetOnMissing(toolchainName, fn)`: 为指定工具链设置缺失回调（用于自动下载）
- `AddGlobalCFlags(flags...)`: 添加全局 C 编译标志
- `AddGlobalCxxFlags(flags...)`: 添加全局 C++ 编译标志
- `AddGlobalLdFlags(flags...)`: 添加全局链接标志
- `RegisterToolchainsFromRepo()`: 扫描仓库子目录中的 `toolchain.json` 并注册工具链
- `LoadToolchainDef()`: 从插件目录加载 `toolchain.json`
- `DownloadFile(url, dest)`: 下载文件
- `ExtractToDir(archive, dest, format)`: 解压归档（支持 tar.gz/tar.xz/tar.bz2/zip）
- `RunGitLFS(repoDir, args...)`: 执行 Git LFS 命令

### 扩展工具函数

- `PluginInfoExists(pluginDir string) bool` — 检查插件目录是否包含有效插件信息
- `CompileResult` 结构体（`pkg/plugin/compiler.go`）嵌入 `gocompile.CompileResult`，额外包含：
  - `PluginDir string` — 插件目录路径
  - `PluginName string` — 插件名称

### 工具链自动下载

扩展可在子目录中放置 `toolchain.json` 来声明工具链。每个工具链一个文件。

示例 `arm-gcc/toolchain.json`：

```json
{
  "name": "arm-gcc",
  "version": "12.2.0",
  "display_name": "ARM GCC 12.2.0",
  "host": "arm-linux-gnueabihf",
  "prefix": "arm-linux-gnueabihf",
  "tools": {
    "cc": "arm-linux-gnueabihf-gcc",
    "cxx": "arm-linux-gnueabihf-g++",
    "ar": "arm-linux-gnueabihf-ar"
  },
  "default_flags": {
    "cflags": ["-mcpu=cortex-a7"],
    "cxxflags": ["-mcpu=cortex-a7"],
    "ldflags": []
  },
  "install": {
    "method": "lfs",
    "file": "arm-gcc-12.2.0.tar.gz",
    "format": "tar.gz"
  }
}
```

通过 `RegisterToolchainsFromRepo()` 扫描并注册所有子目录中的 `toolchain.json`。含 `install` 字段的工具链会自动注册按需下载回调。支持 `method: "lfs"`（Git LFS）和 `method: "http"` 两种下载方式。

源码：`pkg/plugin/`, `cmd/vmake/ext_cmd.go`, `pkg/toolchain/manifest.go`

## 共享基础设施

### gitstore.Store (`internal/gitstore/gitstore.go`)

`RepoManager`（`pkg/repo/manager.go`）和 `plugin.Manager`（`pkg/plugin/manager.go`）都嵌入 `*gitstore.Store`，复用 Git 仓库的增删查操作：

```
Store
├── baseDir  string（私有，通过 BaseDir() 访问）
├── Add(name, gitURL, clone CloneFunc) error   // git clone
├── Remove(name) error                         // 删除仓库
├── Exists(name) bool                          // 是否存在
├── Path(name) string                          // 获取路径
└── List() ([]string, error)                   // 列出所有
```

### gocompile.CompileResult (`internal/gocompile/gocompile.go`)

`buildscript.CompileResult` 和 `plugin.CompileResult` 都嵌入此基础结构：

```
CompileResult
├── Success    bool
├── Error      error
└── OutputPath string
```
