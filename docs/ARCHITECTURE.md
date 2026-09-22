# VMake 架构文档

## 运行时执行流程

vmake build 执行三个阶段：

```
[--manifest: importManifestIntoLock 将清单版本锁定写入 vmake.lock]
    │
Phase 1: OnRequire
    扫描 build.go → 解释构建脚本 → 加载构建脚本 → 收集依赖
    │
Phase 2: 配置准备 (pipeline.Configure → runConfigPhase)
    ├── UpdateOrder — 更新拓扑排序
    └── OnConfig — 执行 OnConfig 回调 → 收集 Option 定义 → 运行 OnApply 回调
        → 合并全局选项 (api.MergeGlobalOptions)
    │
Phase 3: OnBuild (pipeline.RunBuild)
    ├── resolveBuildConfig — 解析构建配置（模式、工具链）
    ├── filterAndCollectNeeded — 用真实配置重新执行 OnRequire（Resolver.FilterDeps
    │   替换节点.Deps）→ UpdateOrder → BFS 收集 needed
    ├── resolveAllPackageDirs — 解析所有包目录
    ├── prepareAllPackages — 下载远程包源码、克隆本地 Git 源码、设置子包目录
    ├── applyPatches — 本地包就地应用（git apply --3way，已应用自动跳过）；
    │   远程包走内容寻址的 EnsurePatched 克隆
    ├── restoreKConfigFiles — 恢复 KConfig 配置
    ├── executeOnBuild — 执行 OnBuild 回调 → 生成 Target
    ├── NewBuildGraph — 构建依赖图 → 拓扑排序
    ├── BuildPipeline.Run — 统一编排调度器
    │   └── NewScheduler → scheduler.BuildAll()
    │       └── ForEachDefault:
    │           ├── resolveTarget（解析 include/define/flag/dep/genrule）
    │           ├── runGenRules（二进制头文件生成等）
    │           ├── generateConfigHeader（可选配置头文件）
    │           ├── compile（compileAll 并行编译源文件）
    │           ├── link（realizeTarget：prebuilt symlink / 链接 / void BuildFunc）
    │           ├── postLink（objcopy/size/strip 等后处理）
    │           └── publishTarget（发布产物到 InstallDir）
    ├── 生成 compile_commands.json
    │
    [构建完成后：vmake test 以 IncludeTests=true 构建，再执行测试二进制]
    │
(Optional) Install
    清理安装前缀 → 执行 OnInstall 回调 → dry-run OnBuild 收集安装项
    → ArtifactInstaller.InstallAll → 安装目标产物 → 生成 manifest.json
```

### Clean Pipeline

```
vmake clean
│
├── Phase 1-2: 同 build（OnRequire → OnConfig）
│
├── Phase 3: OnClean
│   └── 对每个包执行 OnClean 回调（自定义清理逻辑，如 make clean）
│
└── Directory Cleanup
    └── 删除本地包的构建产物目录（build/<key>/）
    
当 resolveToConfigBestEffort 配置解析失败时，降级为扫描目录并清理构建产物（不执行 OnClean 回调）
```

### Build Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--toolchain` | | 覆盖工具链 |
| `--mode` | | 覆盖构建模式（debug/release） |
| `--install` | `-i` | 构建后安装 |
| `--prefix` | `-p` | 安装前缀（默认: `./install/`） |
| `--install-type` | | 安装类型: `runtime`（默认）或 `sdk` |
| `--manifest` | | 从 manifest 文件锁定版本 |
| `--tests` | | 包含测试目标 |
| `--jobs` | `-j` | 并行度：包级并行 + 每 target 编译作业数（0 = NumCPU，1 = 串行） |
| `--keep-going` | `-k` | 某个 target 失败后继续构建其余独立 target |

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
Scan(root)            LoadBuildScript              Resolve
递归扫描 build.go     yaegi 解释执行构建脚本        解析依赖树
    │                       │                        │
    ▼                       ▼                        ▼
[]Source              *api.Package              Graph
                      ├─ Options                 ├─ Order []
                      ├─ requires                └─ Packages map
                      └─ buildFuncs                       └─ *PackageNode
```

1. `buildscript.Scan(root)` 递归扫描 `build.go`，返回 `[]buildscript.Source`
2. `buildscript.LoadBuildScript(src)` 用 yaegi 解释器加载所有 `.go` 文件（`MergeGoSources` 源码合并 → `Eval` → 查找 `Main` → 调用 `Main(*api.Package)`)
3. `resolver.Resolver` 递归解析依赖，生成 `Graph`（拓扑排序）
4. `Resolver.ResolveAll` 在 Phase 1 内完成全部依赖解析（本地 + 远程注册包 + native）；远程包源码的下载物化延迟到 Phase 3 `prepareAllPackages`（`EnsureVersion`）

远程包在 Phase 3 源码下载后、构建前自动应用 git patch：本地包在 SrcDir 就地应用（`git apply --3way`，已应用的 patch 会被跳过），远程包走内容寻址的 `EnsurePatched` 克隆（`<versionDir>/patched/<patchHash>/src`，不可变版本目录不被修改）。

源码：`pkg/buildscript/scanner.go`, `yaegi_loader.go`, `pkg/resolver/resolver.go`

### Phase 2: 配置收集

```
OnConfig 回调 ──▶ 收集 Option 定义 ──▶ 合并全局选项
```

1. 执行所有 `OnConfig` 回调，收集 `Option` 定义
2. `api.MergeGlobalOptions` 合并全局选项定义（内置 `mode` + `toolchain` + 用户定义）；`ConfigAccessor.MergeGlobals` 将全局定义与值合并进脚本上下文作为回退（不覆盖已有值）
3. `OnApply` 回调按选项名排序执行；配置值在后续 TUI 或 CLI flag 中加载

源码：`cmd/vmake/root.go`, `pkg/api/accessor.go`

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
2. **filterAndCollectNeeded** — 用真实配置重新执行 OnRequire（`Resolver.FilterDeps` 替换节点.Deps）→ `UpdateOrder` → 从 `IsLocal()` 根节点 BFS 遍历，过滤需要构建的包
3. **resolveAllPackageDirs** — 解析所有包的 SourceDir/BuildDir/InstallDir
4. **prepareAllPackages** — 下载远程包源码、克隆本地 Git 源码、设置子包目录
5. **writeLockfile** — 写入 `.vmake/vmake.lock`（锁定解析到的版本与 commit）
6. **applyPatches** — 本地包就地应用 git patch（`git apply --3way`，已应用自动跳过）；远程包走内容寻址的 `EnsurePatched` 克隆
7. **restoreKConfigFiles** — 从 config.json 恢复 KConfig 配置（详见 KConfig 章节）
8. **executeOnBuild** — 执行所有 `OnBuild` 回调，生成 `map[string]*Target`
9. **build.NewBuildGraph** — 构建依赖图，`BuildGraph` 展开包级依赖为 target 级传递闭包
10. **build.NewBuildPipeline** — 创建 `BuildPipeline`（封装图、工具链、包目录、模式、选项、调度器）
11. **pipeline.Run()** → `NewScheduler(graph, toolchain, pkgDirs, mode, options)` → `BuildAll()`:
    - `ForEachDefault(includeTests, fn)` 按拓扑顺序构建每个默认 Target
    - 每个 Target: resolveTarget → runGenRules → generateConfigHeader → compile（`compileAll`）→ link（`realizeTarget`：prebuilt symlink / 链接 / void BuildFunc）→ postLink → publishTarget
    - 并行编译源文件（`compileAll` 工作池，`--jobs` 控制编译作业数）
12. **生成 compile_commands.json**（通过 `CompileCommandsWriter`）

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
├── Graph        *BuildGraph
├── Toolchain    *toolchain.Toolchain
├── PkgDirs      map[string]*api.PkgDirs
├── Mode         string
├── Options      map[string]map[string]any
├── Packages     map[string]*api.Package
├── RootDir      string
├── IncludeTests bool
├── PkgKeyExtra  map[string]string
├── PkgLockDir   string
├── NumWorkers   int
├── ParallelPkgs int
└── KeepGoing    bool
```

源码：`pkg/build/scheduler.go`, `pkg/build/graph.go`, `pkg/build/pipeline.go`, `pkg/build/stamp.go`

## 第三方包流程

```
OnRequire          Resolver            SourceManager       Scheduler
声明依赖           解析依赖树           下载源码            构建安装
    │                 │                    │                  │
    ▼                 ▼                    ▼                  ▼
AddRequires      Graph                ~/.vmake/cache/      TargetVoid.BuildFunc()
"official/zlib"  ├─ Order []          <repo>/<pkg>/        → CMakeConfigure
                   └─ Packages map    <version>/src/       → CMakeBuild
                    └─ *PackageNode   (vmake_deps/ 为符号链接) → CMakeInstall
```

1. `OnRequire` 回调调用 `AddRequires("official/zlib >=1.2")`
2. `Resolver` 在 `repos/` 中查找包定义，递归解析依赖
3. `SourceManager.EnsureVersion` 下载源码到全局内容寻址缓存 `~/.vmake/cache/<repo>/<pkg>/<version>/src/`（临时 clone + checkout + 原子重命名，不可变），并在 `vmake_deps/<repo>/<pkg>/src`、`out` 建立符号链接
4. `Scheduler` 按拓扑顺序构建所有目标，包括 `TargetVoid` 目标

对于 `TargetVoid` 类型的目标（第三方包），Scheduler 调用 `Target.BuildFunc()` 并传入 `*api.Package`，执行 CMake/Autotools 等构建命令。

源码：`pkg/resolver/resolver.go`, `pkg/repo/source.go`, `pkg/build/scheduler.go`

## Native 仓库流程

Native 仓库是 VMake 原生的包生态系统，用于跨项目共享包。每个包是一个独立的 Git 仓库，`build.go` 位于仓库根目录。

```
OnRequire            Resolver.findNativeSource          Phase 1               Scheduler
声明依赖             解析 native 源                      yaegi 加载 build.go   构建
    │                      │                                  │                  │
    ▼                      ▼                                  ▼                  ▼
AddRequires          1. 检查 registry 仓库（未找到）      LoadBuildScript      同本地包
"myorg/lib >=1.0"    2. 识别 native 仓库                   解释执行
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
| **版本选择时机** | Phase 1（build.go 加载后，按约束 `SelectVersionMulti`） | Phase 1（build.go 编译前 — 需先 clone） |
| **添加命令** | `vmake repo add name url` | `vmake repo add --native name "https://..../{name}.git"` |
| **更新** | `vmake repo update name` | `vmake pkg update repo/name` |
| **搜索** | 列出仓库中所有包 | 仅显示已缓存的包 |

### Native 源码解析流程 (`findNativeSource`)

1. `findSource` 先检查 registry 仓库（`FindPackageGo`），未找到再检查 native
2. 解析 URL 模板（`{name}` → 包名，`repo.ResolveNativeURL`）
3. 有锁定版本且已缓存时直接 `EnsureVersion`；否则 `sourceMgr.EnsureRefsClone` 维护用于 tag 列表的 refs 克隆（clone/fetch）
4. `repo.ListTags(refsDir)` → `repo.FilterValidVersions`（过滤有效 semver）
5. `selectNativeVersion`（config.json pin → vmake.lock → `repo.SelectNativeVersion` 按约束选择最高匹配版本）
6. `sourceMgr.EnsureVersion` 物化选中版本的不可变 checkout（临时 clone + 原子重命名到全局缓存版本目录）
7. 在版本目录根查找 `build.go`
8. 创建 `PackageNode`，注册到 `graph.Packages`（`WithNative` 写入 `Native *NativePackageInfo`）
9. 仍在 Phase 1：`PreparePackage` 用 yaegi 加载 build.go，随后 `recurseDeps` 继续解析依赖

### PackageNode Native 字段

```go
type PackageNode struct {
    ID          string
    Source      *buildscript.Source
    Pkg         *api.Package
    Deps        []string
    Native      *NativePackageInfo
    Constraints []string
}

type NativePackageInfo struct {
    GitURL   string            // 解析后的 git URL
    Versions map[string]string // version_string → git_tag
    Selected string            // 选中的版本号
    Commit   string            // 选中版本对应的 commit
}
```

源码：`pkg/resolver/resolver.go`, `pkg/repo/native.go`, `pkg/repo/manager.go`

## CLI 命令树

```
vmake (RootCmd)
├── build          # 构建项目
├── clean          # 执行 OnClean 钩子后清理构建产物
├── rebuild        # 完全重新构建
├── distclean      # 深度清理（删除所有构建产物、vmake_deps/、install/）
├── doctor         # 检测 build.go 中的常见问题（如缺少 AddDeps）
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
│   ├── update     # 更新仓库（native 仓库提示使用 pkg update）
│   ├── trust      # 信任仓库的 build.go 脚本
│   └── untrust    # 撤销仓库信任
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
├── lock           # vmake.lock 管理（.vmake/vmake.lock）
│   ├── update     # 重新解析并更新锁定版本
│   └── show       # 显示锁定版本
├── query          # 依赖树查询
│   ├── targets    # 列出构建 target（不构建）
│   └── config     # 显示某包生效的选项值与生成的宏定义
├── manifest       # 安装清单管理
│   ├── show       # 显示清单内容
│   └── checkout   # 按记录版本 checkout
├── skill          # AI skill 管理
│   ├── install    # 安装 AI skill
│   ├── uninstall  # 卸载 AI skill
│   └── path       # 显示安装路径
├── init-editor    # 生成 build.go 编辑器支持文件（gopls 用的 go.mod）
├── completion [shell] # 生成 shell 补全脚本（bash/zsh/fish/powershell 作为参数）
│   └── install    # 自动安装补全到你的 shell 配置
├── test           # 构建并运行测试目标
├── check-symbols  # 扫描构建产物，检测符号泄漏和版本脚本违规
└── <plugin>       # 扩展插件提供的命令
    └── ...        # 插件自定义子命令
```

全局选项：`-v` (verbose), `-V` (very verbose), `-q` (quiet), `-y` (assume yes for interactive prompts)

源码：`cmd/vmake/`（`distclean.go`, `completion.go`）

## 统一依赖系统

VMake 使用 `AddDeps` 统一管理所有依赖类型：

| 类型 | 示例 | 识别规则 |
|------|------|---------|
| 同包 target | `AddDeps("utils")` | 不含 `:` 和 `/` |
| 跨包 target | `AddDeps("lib:utils")` | 含 `:`，指定具体 target |
| 通配依赖 | `AddDeps("lib:*")` 或 `AddDeps("official/zlib:*")` | `:*` 结尾，展开为该包所有 target + 传递依赖 |
| 第三方包 | `AddDeps("official/zlib")` | 含 `/`，展开为该包所有 target + 传递依赖 |

`pkg:target` 中不含 `/` 的 pkg 部分会先按当前（子）包路径相对解析（`ResolveSubPackageName`），兄弟子包之间可用短名。非法引用（空引用、含空白、多个 `:`、`:` 前后段为空、包路径首/尾 `/` 或 `//`）在声明时即 fatal（`ParseDepRef`）；target/包不存在或循环依赖在构建图时报错。

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
├── Native      *NativePackageInfo
└── Constraints []string          // 版本约束列表（如 [">=1.2", "<2.0"]）

NativePackageInfo
├── GitURL   string            // native 仓库：解析后的 git URL
├── Versions map[string]string // native 仓库：version_string → git_tag
├── Selected string            // native 仓库：选中的版本号
└── Commit   string            // native 仓库：选中版本对应的 commit
```

### buildscript.Source (`pkg/buildscript/source.go`)

```
Source
├── Path   string          // build.go 文件路径
├── Name   string          // 包名（如 "official/zlib"）
├── Dir    string          // 包目录
├── Origin api.SourceOrigin // SourceLocal 或 SourceRemote
└── Repo   string          // 脚本来源的远程仓库名（如 "official"），本地脚本为空
```

### BuildGraph (`pkg/build/graph.go`)

```
BuildGraph
├── Nodes   map[string]*BuildNode              // "pkg:target" → Node
├── Order   []string                           // 拓扑排序结果
└── PkgMeta map[string]PkgBuildMeta            // 包级元数据（展开包引用用）

BuildNode
├── FullName string                          // "pkg:target"
├── PkgName  string
├── Target   *api.Target
└── Deps     []string                        // 统一依赖列表（含展开的第三方包 target）
```

`BuildGraph` 提供辅助方法：
- `GetNode(name) (*BuildNode, error)` — 按 `pkg:target` 全名查找节点
- `ForEachDefault(includeTests bool, fn func(node *BuildNode) error) error` — 遍历所有默认目标（`includeTests` 控制 `SetTest` 目标是否包含）

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

- `AddPatches(paths...)` — 添加 git patch 文件（相对于构建脚本目录 ScriptDir），构建前自动应用
- `SetPatches(paths...)` — 设置 git patch 文件（覆盖）
- `SetGenConfigHeader(v bool)` — 启用或禁用配置头文件自动生成
- `GenConfigHeader()` — 获取配置头文件生成开关状态
- `SetScriptDir(dir)` — 设置构建脚本目录
- `SetOutputDir(dir)` — 设置输出目录
- `SetCfgVals(vals)` — 设置配置值
- `SelectVersionMulti(constraints)` — 多约束版本选择
- `SelectedPreset()` — 返回已选中的 KConfig preset 名称
- `ApplyKConfigPatches(configPath, patches)` — 对 `.config` 文件应用按行替换的补丁

源码：`pkg/api/package.go`

## 关键文件位置

| 组件 | 文件路径 | 职责 |
|------|----------|------|
| API 定义 | `pkg/api/` | 公共 API，构建脚本可导入 |
| 插件系统 | `pkg/plugin/` | 扩展插件管理、解释、加载 |
| 构建脚本系统 | `pkg/buildscript/` | 扫描、解释、加载构建脚本 |
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
| Go 源码合并 | `internal/gosrc/` | 多文件 Go 源码合并（buildscript + plugin 共用） |
| Yaegi 符号 | `internal/yaegisym/` | cobra/pflag 的 yaegi 符号表（`go generate` 生成） |
| Yaegi 基类 | `internal/yaegibase/` | yaegi 解释器初始化 helper（stdlib + unrestricted） |

## KConfig 系统

VMake 内置 KConfig 配置管理，用于 Linux 内核、U-Boot 等 Kconfig-based 项目的配置流程。

### KConfigEntry

```go
type KConfigEntry struct {
    name, description, configPath, srcDir, menuconfigCmd string
    menuconfigArgs []string
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
- `SetMenuconfigCmd(program, args...)` — 分开设置 menuconfig 程序和参数，在 SrcDir 执行；preset 使用所选工具链的 make
- `AddPreset(name)` — 添加 preset（defconfig 文件名）
- `SetDefaultPreset(presetName)` — 设置默认 preset
- `SelectPreset(name)` — 设置选中 preset
- `SetKConfigPatches(patches)` — 设置 post-defconfig 补丁（`map[string]string`）

### 声明与配置

在 `OnConfig` 中通过 `Package.AddKConfig(name)` 声明 KConfig 条目，TUI 会列出可用 preset 供用户选择。

### EnsureConfig 抽象

`Package.EnsureConfig(srcDir) bool` 是 KConfig 构建的核心抽象：

1. 检查 `.config` 是否存在且大小 > 0 → 如果有效，返回 `false`（无需重新生成）
2. 执行 `make <selectedPreset>` 生成 `.config`
3. 应用 `SetKConfigPatches` 中定义的 post-defconfig 补丁（按行前缀匹配替换）
4. 返回 `true`（已重新生成配置）

### ApplyKConfigPatches

`Package.ApplyKConfigPatches(configPath, patches)` 是独立的导出函数，对 `.config` 文件应用按行替换的补丁：

- 读取 `.config` 文件
- 对每一行，按 `patchValues` 中各 key 做行前缀匹配（key 含 `=` 时直接前缀匹配，否则按 `key=` 匹配），命中则整行替换为对应的 value
- 写回 `.config` 文件

被 `EnsureConfig` 和 `restoreKConfigFiles` 共同调用。

### restoreKConfigFiles 跳转规则

`restoreKConfigFiles` 从 `config.json` 恢复 KConfig 配置，遵循以下规则：

- `config.json` 中没有该包的 entry → 跳过（不删除 `.config`）
- `config.json` 中有 entry 但 kconfig 为空（preset 切换） → 删除 `.config`
- `config.json` 中有 kconfig 内容 → 仅在内容变化时写回（避免 mtime 更新导致 stamp 失效）
- KConfig 内容为空 → 不写入

### Stamp-Based Skip（TargetVoid）

无 `InstallDir` 且带 `BuildFunc` 的 `TargetVoid` 目标使用 `.vmake_stamp` 跳过重复构建：

- 构建完成后在 `BuildDir` 写入 `.vmake_stamp`（JSON：`config_hash` + `source_rev`）
- 下次构建时校验 stamp 是否有效（`isVoidUpToDate`）
- 通过 `SetConfigFiles()` 声明的配置文件内容哈希与 stamp 记录不一致时，判定为 stale，重新构建（内容寻址，非 mtime；文件被删除同样改变哈希）
- `source_rev`（SrcDir 的 git HEAD）变化也判定为 stale
- 依赖产物比 stamp 新时同样重建（`depArtifactsNewer`）

### autoWireRequireDeps（v2 已移除）

历史上 vmake 提供 `autoWireRequireDeps()` 自动补全依赖边：当 target 没有显式 `AddDeps()` 但包通过 `AddRequires` 声明了依赖时，会自动将依赖包的所有 target 作为当前 target 的依赖。

v2 中已移除该 fallback（违反 No-Fallbacks 原则）。每个 target 必须显式声明 `AddDeps`。运行 `vmake doctor` 检测仍依赖旧行为的 build.go（`AddRequires` 与 `AddDeps` 不匹配等）。

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
    Args []string // 参数模板（{output} 占位符 → 链接输出路径）
}
```

### API 方法

- `AddPostLink(tool, args...)` — 添加自定义后链接步骤
- `AddPostLinkOutputs(paths...)` — 显式声明额外产物，支持 `{output}`；缺失时重新链接并运行全部步骤，安装阶段使用同一声明列表
- `AddPostLinkDeps(files...)` — 声明 post-link 步骤依赖的输入文件（SourceDir 相对路径）；任一变化（mtime 新于输出或缺失）触发 relink + 重跑全部 post-link
- `AddPostLinkHex()` — 添加 `objcopy -O ihex` 生成 .hex 文件
- `AddPostLinkBin()` — 添加 `objcopy -O binary` 生成 .bin 文件
- `AddPostLinkSize()` — 添加 `size {output}` 显示段大小
- `AddPostLinkStrip()` — 添加 `strip -o {output}.stripped {output}`，生成去除符号的 `.stripped` 副本

### 执行流程

在 `Scheduler.finalizeTarget` 中，`realizeTarget`（链接/prebuilt/void）成功后调用 `postLink`，执行所有 `PostLinkStep`：

1. 将参数中的 `{output}` 替换为链接输出路径
2. 执行每个步骤的工具命令
3. `Target.PostLinkOutputs()` 声明的输出产物会由 `installTarget`（`ArtifactInstaller`，`vmake build --install`）随主产物一起安装；不从 `PostLinkStep.Args` 推断输出

post-link 仅在实际发生 relink（`needRelink=true`）时执行。`AddPostLinkDeps` 声明的输入文件参与 `needRelink` 的 mtime 判定，使 post-link 输入（如 `--keep-global-symbols=file.sym` 中的 `.sym`）变化时能触发 relink + 重跑 post-link，避免静默跳过。

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

### 插件加载流程（扩展系统，yaegi 解释执行）

> buildscript 和扩展插件系统均使用 yaegi 解释器动态加载 Go 源码，不再编译 `.so`。

```
vmake 启动
    │
    ▼
plugin.Manager.DiscoverPlugins()  ──▶ 扫描 extensions/*/
    │
    ▼
plugin.Load()                     ──▶ 合并解释插件入口目录的全部 .go 文件，查找 Main func
    │
    ▼
plugin.RunMain(loaded, ctx)       ──▶ 调用插件 Main(ctx)（相对路径经 scriptfs 解析到入口目录）
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
    "github.com/spock2300/vmake/pkg/plugin"
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
- `RegisterToolchain(name, tc) error`: 注册工具链，重名或名称非法时返回错误
- `GetToolchains()`: 获取已注册工具链
- `SetOnMissing(toolchainName, fn)`: 为指定工具链设置缺失回调（用于自定义安装）
- `AddGlobalCFlags(flags...)`: 添加全局 C 编译标志
- `AddGlobalCxxFlags(flags...)`: 添加全局 C++ 编译标志
- `AddGlobalLdFlags(flags...)`: 添加全局链接标志
- `DownloadFile(url, dest)`: 下载文件
- `ExtractToDir(archive, dest, format)`: 解压归档（支持 tar.gz/tar.xz/tar.bz2/zip）
- `RunGitLFS(repoDir, args...)`: 执行 Git LFS 命令

插件不参与编译过程：没有编译回调，也不向 `build.go` 暴露 API。插件收集的全局标志在 `Main` 返回后统一发布到 `toolchain.Manager`，单个插件失败不影响其它插件。

### 扩展工具函数

- `PluginInfoExists(pluginDir string) bool` — 检查插件目录是否包含有效插件信息
- `LoadPluginInfo(pluginDir string) (*Info, error)` — 从插件目录加载 `plugin.json`

### 工具链自动下载

扩展可在子目录中放置 `toolchain.json` 来声明工具链。每个工具链一个文件。工具链只描述使用哪些程序，不描述目标 CPU/ABI，也不携带项目默认编译选项。

示例 `arm-gcc/toolchain.json`：

```json
{
  "name": "arm-gcc",
  "version": "12.2.0",
  "display_name": "ARM GCC 12.2.0",
  "prefix": "arm-linux-gnueabihf-",
  "tools": {
    "cc": "arm-linux-gnueabihf-gcc",
    "cxx": "arm-linux-gnueabihf-g++",
    "ar": "arm-linux-gnueabihf-ar",
    "ld": "arm-linux-gnueabihf-gcc"
  },
  "installations": {
    "linux/amd64": {
      "method": "lfs",
      "file": "arm-gcc-12.2.0.tar.gz",
      "format": "tar.gz",
      "root_dir": "arm-gcc-12.2.0"
    }
  }
}
```

`toolchain.Manager.RegisterRepo(repoDir, toolchainsDir)`（`pkg/toolchain/discovery.go`）扫描并注册所有子目录中的 `toolchain.json`，不需要插件参与。含当前宿主 `installations` 条目的工具链自动注册按需安装回调，由 `toolchain.Install`（`pkg/toolchain/install.go`）加锁、校验 SHA256、暂存解压后重命名发布。支持 `method: "lfs"`（Git LFS）和 `method: "http"` 两种下载方式。

`target_os`、`target_triple`、`default_flags` 写进 `toolchain.json` 会被拒绝，它们属于项目配置：`api.Platform` 由全局选项 `target_os` / `target_triple` 解析（`pkg/pipeline/platform.go`），默认编译选项由 `api.DefaultBuildFlags`（`pkg/api/default_flags.go`）在内置 `host` 工具链下提供。

源码：`pkg/plugin/`, `cmd/vmake/ext_cmd.go`, `pkg/toolchain/manifest.go`, `pkg/toolchain/discovery.go`, `pkg/toolchain/install.go`

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

### gosrc.MergeGoSources (`internal/gosrc/merge.go`)

`buildscript.LoadBuildScript`（`pkg/buildscript/yaegi_loader.go`）和 `plugin.Load`（`pkg/plugin/loader.go`）共用此函数合并多文件 Go 源码，供 yaegi 解释执行。

### yaegibase.New (`internal/yaegibase/base.go`)

创建预配置的 yaegi 解释器（注册 stdlib + unrestricted 符号），buildscript 和 plugin loader 各自追加专用符号后使用。
