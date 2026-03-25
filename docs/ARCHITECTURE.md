# VMake 架构文档

## 运行时执行流程

vmake build 执行三个阶段：

```
Phase 1: OnRequire
    扫描 build.go → 编译构建脚本 → 加载构建脚本 → 收集依赖
    |
Phase 2: OnConfig
    执行 OnConfig 回调 → 收集 Option 定义 → 加载已保存配置
    |
Phase 3: OnBuild
    执行 OnBuild 回调 → 生成 Target → 构建依赖图 → 编译/链接
```

### Phase 1: 构建脚本扫描与依赖解析

```
Scan(root)          Compile             Load              Resolve
递归扫描 build.go   编译为 .so          加载构建脚本      解析依赖树
    │                   │                  │                 │
    ▼                   ▼                  ▼                 ▼
[]Source           build.so         LoadedScript      DependencyGraph
                                     ├─ pkg *Package   ├─ Order []
                                     └─ Source          └─ Packages map
```

1. `buildscript.Scan(root)` 递归扫描 `build.go`，返回 `[]Source`
2. `buildscript.Compiler` 编译为 `.so`，缓存在 `cache/buildscripts/`
3. `buildscript.Loader` 加载 `.so`，调用 `Main(*api.Package)` 获取 `*api.Package`
4. `resolver.Resolver` 递归解析依赖，生成 `Graph`（拓扑排序）

源码：`pkg/buildscript/scanner.go`, `compiler.go`, `loader.go`, `pkg/resolver/resolver.go`

### Phase 2: 配置收集

```
OnConfig 回调 ──▶ 收集 Option 定义 ──▶ 合并全局选项 ──▶ 加载 .vmake/config.json
```

1. 执行所有 `OnConfig` 回调，收集 `Option` 定义
2. `MergeGlobalOptions` 合并全局选项（内置 `mode` + `toolchain` + 用户定义）
3. 从 `config.json` 加载已保存的配置值

源码：`cmd/vmake/root.go:141-187`, `pkg/api/global.go`

### Phase 3: 构建执行

```
OnBuild 回调 ──▶ 生成 Target ──▶ BuildGraph ──▶ Scheduler.BuildAll()
                                                   │
                                                   ▼
                                              for each target:
                                                resolveTarget ──▶ compile ──▶ link
```

1. 执行所有 `OnBuild` 回调，生成 `map[string]*Target`
2. `build.NewBuildGraph` 构建依赖图，拓扑排序
3. `build.NewScheduler` 初始化编译器、链接器、加载缓存
4. `Scheduler.BuildAll` 按拓扑顺序构建每个 Target

源码：`pkg/build/scheduler.go`, `pkg/build/graph.go`

## 第三方包流程

```
OnRequire          Resolver            SourceManager       Scheduler
声明依赖           解析依赖树           下载源码            构建安装
    │                 │                    │                  │
    ▼                 ▼                    ▼                  ▼
AddRequires      DependencyGraph    cache/<repo>/<pkg>/  TargetVoid.BuildFunc()
"official/zlib"  ├─ Order []        repo/                → CMakeConfigure
                 └─ Packages map                           → CMakeBuild
                                                            → CMakeInstall
```

1. `OnRequire` 回调调用 `AddRequires("official/zlib >=1.2")`
2. `Resolver` 在 `repos/` 中查找包定义，递归解析依赖
3. `SourceManager.EnsureSource` 通过 git clone 下载源码到 `cache/<repo>/<pkg>/repo/`
4. `Scheduler` 按拓扑顺序构建所有目标，包括 `TargetVoid` 目标

对于 `TargetVoid` 类型的目标（第三方包），Scheduler 调用 `Target.BuildFunc()` 并传入 `*api.Package`，执行 CMake/Autotools 等构建命令。

源码：`pkg/resolver/resolver.go`, `pkg/repo/source.go`, `pkg/build/scheduler.go`

## CLI 命令树

```
vmake (RootCmd)
├── build          # 构建项目
├── clean          # 清理构建产物
├── rebuild        # 完全重新构建
├── config         # TUI 配置界面
├── update         # 更新项目
├── toolchain      # 工具链管理
│   ├── init       # 生成配置模板
│   ├── list       # 列出工具链
│   └── show       # 显示详情
├── repo           # 包仓库管理
│   ├── add        # 添加仓库
│   ├── remove     # 删除仓库
│   ├── list       # 列出仓库
│   └── update     # 更新仓库
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
│   └── tag        # Git 标签操作
├── doc            # 文档查看（AI agents）
├── version        # 版本信息
└── <plugin>       # 扩展插件提供的命令
    └── ...        # 插件自定义子命令
```

全局选项：`-v` (verbose), `-V` (very verbose), `-q` (quiet)

源码：`cmd/vmake/`

## 核心数据结构

### DependencyGraph (`pkg/repo/resolver.go`)

```
DependencyGraph
├── Order    []string                        // 拓扑排序后的包名列表
└── Packages map[string]*ResolvedPackage

ResolvedPackage
├── Name       string
├── Constraint string
├── Options    map[string]any
├── Definition *api.Package
├── Source     *buildscript.Source
├── Deps       []string
└── Deferred   bool
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
└── Deps     []string
```

### ConfigFile (`pkg/config/store.go`)

```
ConfigFile
├── Version  string
├── Global   *GlobalConfig
└── Entries  map[string]*EntryConfig

EntryConfig
├── Version  string                  // 第三方包的版本（可选）
└── Options  map[string]any          // 配置选项
```

## 关键文件位置

| 组件 | 文件路径 | 职责 |
|------|----------|------|
| API 定义 | `pkg/api/` | 公共 API，构建脚本可导入 |
| 构建脚本系统 | `pkg/buildscript/` | 扫描、编译、加载构建脚本 |
| 依赖解析 | `pkg/resolver/` | 依赖图解析、拓扑排序 |
| 构建系统 | `pkg/build/` | 编译、链接、调度 |
| 包管理 | `pkg/repo/` | 仓库管理、源码下载、安装 |
| 工具链 | `pkg/toolchain/` | GCC/Clang 抽象 |
| 配置 | `pkg/config/` | 配置文件读写 |
| 日志 | `pkg/log/` | 日志输出 |
| TUI | `pkg/tui/` | 终端界面 |
| CLI | `cmd/vmake/` | 命令行入口 |
| JSON I/O | `internal/jsonio/` | JSON 序列化工具 |
| 命令执行 | `internal/exec/` | OS 命令执行 |
| 文件匹配 | `internal/glob/` | Glob 模式匹配 |

## 扩展系统

VMake 支持通过 Go 插件扩展 CLI 命令。扩展仓库是包含一个或多个插件的 Git 仓库。

### 扩展仓库结构

```
~/.vmake/extensions/
└── <repo-name>/                  # 扩展仓库名
    ├── <plugin-name>/            # 插件目录（成为 vmake 子命令）
    │   ├── plugin.json           # 插件元信息
    │   └── src/main.go           # 插件入口
    └── assets/
        └── toolchains/           # 工具链资源（可选）
            ├── manifest.json     # 工具链清单
            └── *.tar.gz          # 工具链压缩包（Git LFS）
```

### 插件加载流程

```
vmake 启动
    │
    ▼
plugin.Manager.DiscoverPlugins()  ──▶ 扫描 extensions/*/
    │
    ▼
plugin.Compiler.Compile()         ──▶ 编译 main.go → .so
    │
    ▼
plugin.Loader.Load()              ──▶ 加载 .so，调用 Main(ctx)
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
- `SetOnMissing(fn)`: 设置工具链缺失时的回调（用于自动下载）
- `DownloadFile(url, dest)`: 下载文件
- `ExtractArchive(archive, dest)`: 解压归档

### 工具链自动下载

扩展可提供工具链资源，通过 `manifest.json` 声明：

```json
{
  "toolchains": [
    {
      "name": "aarch64-linux-gnu",
      "version": "13.2.0",
      "host": "x86_64-linux-gnu",
      "prefix": "aarch64-linux-gnu",
      "file": "aarch64-linux-gnu-13.2.0.tar.gz",
      "cflags": ["-O2"],
      "cxxflags": ["-O2"],
      "ldflags": []
    }
  ]
}
```

当用户选择未安装的工具链时，`SetOnMissing` 回调被触发，自动从扩展仓库下载并安装。

源码：`pkg/plugin/`, `cmd/vmake/ext_cmd.go`
