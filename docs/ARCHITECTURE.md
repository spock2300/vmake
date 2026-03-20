# VMake 架构文档

## 1. 整体运行流程

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              vmake build                                     │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  Phase 1: 扫描与依赖解析                                                      │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                   │
│  │ Scan(root)   │───▶│ Compile      │───▶│ Load         │                   │
│  │ 扫描build.go │    │ 编译为.so    │    │ 加载插件     │                   │
│  └──────────────┘    └──────────────┘    └──────────────┘                   │
│         │                   │                   │                           │
│         ▼                   ▼                   ▼                           │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                   │
│  │ []Source     │    │CompileResult │    │LoadedPlugin  │                   │
│  │ 编译元数据   │    │ PluginPath   │    │ ExtractPkg() │                   │
│  └──────────────┘    └──────────────┘    └──────┬───────┘                   │
│                                                   │                          │
│                                                   ▼                          │
│                                          ┌──────────────┐                   │
│                                          │ *api.Package │                   │
│                                          │ GetRequires()│                   │
│                                          └──────┬───────┘                   │
│                                                   │                          │
│                                                   ▼                          │
│                                          ┌──────────────┐                   │
│                                          │ Resolver     │                   │
│                                          │ 依赖解析     │                   │
│                                          └──────┬───────┘                   │
│                                                   │                          │
│                                                   ▼                          │
│                                          ┌──────────────┐                   │
│                                          │DependencyGraph│                  │
│                                          │ Packages[]   │                   │
│                                          │ Order[]      │                   │
│                                          └──────────────┘                   │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  Phase 2: 配置收集                                                           │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                   │
│  │ OnConfig()   │───▶│ Option定义   │───▶│ 加载配置文件 │                   │
│  │ 执行回调     │    │ 收集选项     │    │ .vmake/config│                   │
│  └──────────────┘    └──────────────┘    └──────────────┘                   │
│                                                   │                          │
│                                                   ▼                          │
│                                          ┌──────────────┐                   │
│                                          │ ConfigFile   │                   │
│                                          │ Global       │                   │
│                                          │ Packages{}   │                   │
│                                          │ Requires{}   │                   │
│                                          └──────────────┘                   │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  Phase 3: 构建执行                                                           │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                   │
│  │ OnBuild()    │───▶│ Target定义   │───▶│ BuildGraph   │                   │
│  │ 执行回调     │    │ 生成目标     │    │ 拓扑排序     │                   │
│  └──────────────┘    └──────────────┘    └──────────────┘                   │
│                                                   │                          │
│                                                   ▼                          │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                         Scheduler.BuildAll()                          │   │
│  │  for each target in Order:                                            │   │
│  │    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐             │   │
│  │    │resolveTarget│───▶│ compile     │───▶│ link        │             │   │
│  │    │ 解析依赖    │    │ 并行编译    │    │ 链接输出    │             │   │
│  │    └─────────────┘    └─────────────┘    └─────────────┘             │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 2. 核心数据结构

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                 Builder                                      │
│  build.go 的核心容器，存储所有回调函数                                         │
├─────────────────────────────────────────────────────────────────────────────┤
│  configFuncs  []ConfigFunc     // OnConfig 注册的回调                        │
│  buildFuncs   []BuildFunc      // OnBuild 注册的回调                         │
│  installFuncs []InstallFunc    // OnInstall 注册的回调                       │
│  requireFuncs []RequireFunc    // OnRequire 注册的回调                       │
│  packageFunc  PackageFunc      // OnPackage 注册的回调                       │
└─────────────────────────────────────────────────────────────────────────────┘
                    │
        ┌───────────┼───────────┬───────────────┐
        ▼           ▼           ▼               ▼
┌───────────┐ ┌───────────┐ ┌───────────┐ ┌───────────────┐
│ConfigCtx  │ │ BuildCtx  │ │PackageCtx │ │ RequireCtx    │
├───────────┤ ├───────────┤ ├───────────┤ ├───────────────┤
│options    │ │targets    │ │gitURLs    │ │requires       │
│cfgVals    │ │cfgVals    │ │versions   │ │  []RequireInfo│
│pkgName    │ │pkgName    │ │options    │ └───────────────┘
└───────────┘ │globalVals │ │sourceDir  │
              │globalOpts │ │buildDir   │
              │installItems│ │installDir │
              └───────────┘ │toolchain  │
                            │deps       │
                            └───────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                          插件编译与加载流程                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│  Source (pkg/plugin)          LoadedPlugin (pkg/plugin)                      │
│  ┌─────────────────┐          ┌─────────────────┐                           │
│  │ Path            │ Compile  │ Source          │                           │
│  │ Name            │─────────▶│ Plugin *plugin  │                           │
│  │ Dir             │          │ pkg *api.Package│                           │
│  │ OutputDir       │          └────────┬────────┘                           │
│  │ Origin          │                   │                                    │
│  │ Force           │          ExtractPackage()                              │
│  └─────────────────┘                   │                                    │
│                                        ▼                                    │
│                               ┌─────────────────┐                           │
│                               │ *api.Package    │                           │
│                               │ - gitURLs       │                           │
│                               │ - versions      │                           │
│                               │ - options       │                           │
│                               │ - buildFunc     │                           │
│                               │ - configFuncs   │                           │
│                               └─────────────────┘                           │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 3. 包管理数据流

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            第三方包处理流程                                   │
└─────────────────────────────────────────────────────────────────────────────┘

  用户代码                      VMake内部                      文件系统
  ────────                     ─────────                     ─────────
     │                              │                            │
     │  b.OnRequire(func(ctx) {     │                            │
     │    ctx.AddRequires(          │                            │
     │      "official/zlib")        │                            │
     │    )                         │                            │
     │  })                          │                            │
     │─────────────────────────────▶│                            │
     │                              │                            │
     │                              │  ┌──────────────────┐      │
     │                              │  │ Resolver         │      │
     │                              │  │ 解析依赖树       │      │
     │                              │  └────────┬─────────┘      │
     │                              │           │                │
     │                              │           ▼                │
     │                              │  ┌──────────────────┐      │
     │                              │  │ RepoManager      │      │
     │                              │  │ FindPackageGo()  │◀─────│ repos/official/packages/z/zlib/build.go
      │                              │  └────────┬─────────┘      │
      │                              │           │                │
      │                              │           ▼                │
      │                              │  ┌──────────────────┐      │
      │                              │  │ Compile(Source)  │      │
      │                              │  │ 编译build.go     │      │
      │                              │  └────────┬─────────┘      │
      │                              │           │                │
      │                              │           ▼                │
      │                              │  ┌──────────────────┐      │
      │                              │  │ Load(.so)        │      │
      │                              │  │ 加载plugin.so    │      │
      │                              │  └────────┬─────────┘      │
      │                              │           │                │
      │                              │           ▼                │
      │                              │  ┌──────────────────┐      │
      │                              │  │ ExtractPackage() │      │
      │                              │  │ 提取api.Package  │      │
      │                              │  │ 1. 调用Main()    │      │
      │                              │  │ 2. 收集回调函数  │      │
      │                              │  │ 3. 构建Package   │      │
      │                              │  └────────┬─────────┘      │
      │                              │           │                │
      │                              │           ▼                │
     │                              │  ┌──────────────────┐      │
     │                              │  │ SourceManager    │      │
     │                              │  │ EnsureSource()   │─────▶│ sources/official/zlib/repo/
     │                              │  │ 1. git clone     │      │
     │                              │  │ 2. git checkout  │      │
     │                              │  └────────┬─────────┘      │
     │                              │           │                │
     │                              │           ▼                │
     │                              │  ┌──────────────────┐      │
     │                              │  │ Installer        │      │
     │                              │  │ doInstall()      │─────▶│ packages/official/zlib/1.2.13/{hash}/
     │                              │  │ 1. cmake/config  │      │   ├── build/
     │                              │  │ 2. make          │      │   └── install/
     │                              │  │ 3. make install  │      │       ├── include/
     │                              │  └────────┬─────────┘      │       └── lib/
     │                              │           │                │
     │                              │           ▼                │
     │                              │  ┌──────────────────┐      │
     │                              │  │InstalledPackage  │      │
     │                              │  │ Name/Version     │      │
     │                              │  │ IncludeDir       │      │
     │                              │  │ LibDir           │      │
     │                              │  │ Libs             │      │
     │                              │  └──────────────────┘      │
     │                              │                            │
```

## 4. 依赖图结构

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          DependencyGraph (pkg/repo/resolver.go)              │
├─────────────────────────────────────────────────────────────────────────────┤
│  Order        []string                // 拓扑排序后的包名列表                  │
│  Packages     map[string]*ResolvedPackage                                       │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                          ResolvedPackage                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│  Name       string                                                          │
│  Constraint string               // 版本约束                                  │
│  Options    map[string]any       // 配置选项                                  │
│  Definition *api.Package         // 包定义(元数据)                            │
│  Source     *plugin.Source       // 编译元数据(本地/远程)                     │
│  Deps       []string             // 依赖列表                                  │
└─────────────────────────────────────────────────────────────────────────────┘

示例: curl 依赖 mbedtls, mbedtls 依赖 无

  DependencyGraph.Order = ["official/mbedtls", "official/curl"]
  
  Packages["official/curl"] = {
    Name: "official/curl",
    Deps: ["official/mbedtls"],
    Definition: &api.Package{gitURLs, versions, buildFunc...}
  }
  
  Packages["official/mbedtls"] = {
    Name: "official/mbedtls", 
    Deps: [],
    Definition: &api.Package{...}
  }
```

## 5. 构建图结构

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          BuildGraph (pkg/build/graph.go)                     │
├─────────────────────────────────────────────────────────────────────────────┤
│  Nodes  map[string]*BuildNode    // "pkgName:targetName" → Node              │
│  Order  []string                 // 拓扑排序后的target列表                     │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                          BuildNode                                           │
├─────────────────────────────────────────────────────────────────────────────┤
│  FullName  string            // "mypkg:utils"                                │
│  PkgName   string            // "mypkg"                                      │
│  Target    *api.Target       // 目标定义                                      │
│  Deps      []string          // ["mypkg:core", "other:lib"]                  │
└─────────────────────────────────────────────────────────────────────────────┘

示例:

  BuildGraph.Order = ["app:utils", "app:core", "app:main"]
  
  Nodes["app:main"] = {
    FullName: "app:main",
    PkgName: "app",
    Target: {kind: binary, files: ["main.c"], deps: ["core", "utils"]},
    Deps: ["app:core", "app:utils"]
  }
  
  Nodes["app:core"] = {
    FullName: "app:core",
    Target: {kind: static, files: ["core.c"], deps: ["utils"]},
    Deps: ["app:utils"]
  }
  
  Nodes["app:utils"] = {
    FullName: "app:utils",
    Target: {kind: static, files: ["utils.c"]},
    Deps: []
  }
```

## 6. Target 定义与编译流程

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Target (pkg/api/target.go)                          │
├─────────────────────────────────────────────────────────────────────────────┤
│  name           string          // 目标名                                     │
│  kind           TargetKind      // binary/static/shared/object/void         │
│  files          []string        // 源文件 ["src/*.c"]                        │
│  includes       []string        // 私有头文件路径                             │
│  publicIncludes []string        // 公开头文件路径(导出给依赖方)                │
│  defines        []string        // 预定义宏                                   │
│  links          []string        // 链接库                                     │
│  deps           []string        // 目标依赖                                   │
│  cflags         []string        // C编译标志                                  │
│  cxxflags       []string        // C++编译标志                                │
│  ldflags        []string        // 链接标志                                   │
│  packages       []string        // 第三方包依赖                               │
│  buildFunc      func() error    // 自定义构建函数(TargetVoid用)              │
└─────────────────────────────────────────────────────────────────────────────┘

                              编译流程
                              ────────
                              
  Target                    Scheduler                  Compiler/Linker
  ──────                    ─────────                  ───────────────
     │                           │                           │
     │ files=["a.c","b.c"]       │                           │
     │ kind=static               │                           │
     │──────────────────────────▶│                           │
     │                           │                           │
     │                           │ resolveTarget()           │
     │                           │ - 合并includes/defines    │
     │                           │ - 解析package依赖         │
     │                           │ - glob展开源文件          │
     │                           │                           │
     │                           │ compileSource(a.c)       │
     │                           │──────────────────────────▶│ cc -c a.c -o a.o
     │                           │                           │
     │                           │ compileSource(b.c)       │
     │                           │──────────────────────────▶│ cc -c b.c -o b.o
     │                           │                           │
     │                           │ link()                    │
     │                           │──────────────────────────▶│ ar rcs libfoo.a a.o b.o
     │                           │                           │
```

## 7. 配置系统

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     ConfigFile (.vmake/config.json)                          │
├─────────────────────────────────────────────────────────────────────────────┤
│  version  string                                                            │
│  global   *GlobalConfig                                                     │
│  packages map[string]*PackageConfig    // 本地包配置                         │
│  requires map[string]*RequireConfig    // 第三方包配置                       │
└─────────────────────────────────────────────────────────────────────────────┘
        │               │               │
        ▼               ▼               ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│ GlobalConfig │ │PackageConfig │ │RequireConfig │
├──────────────┤ ├──────────────┤ ├──────────────┤
│ toolchain    │ │ options      │ │ version      │
│ mode         │ │  map[string] │ │ options      │
│ options      │ │    any       │ │  map[string] │
│  map[string] │ └──────────────┘ │    any       │
│    any       │                  └──────────────┘
└──────────────┘

示例配置文件:
{
  "version": "1",
  "global": {
    "toolchain": "gcc",
    "mode": "debug",
    "options": {}
  },
  "packages": {
    "app": {
      "options": {
        "ssl": true
      }
    }
  },
  "requires": {
    "official/zlib": {
      "version": "1.2.13",
      "options": {}
    }
  }
}
```

## 8. 关键文件位置

| 组件 | 文件路径 | 职责 |
|------|----------|------|
| **API定义** | `pkg/api/` | 公共API，插件可导入 |
| Builder | `pkg/api/builder.go` | 回调函数容器 |
| Target | `pkg/api/target.go` | 构建目标定义 |
| Package | `pkg/api/package.go` | 包元数据定义 |
| Context | `pkg/api/context.go` | ConfigContext, BuildContext, PackageContext |
| **插件系统** | `pkg/plugin/` | 插件管理 |
| Source | `pkg/plugin/source.go` | 编译元数据(Path, Name, Dir, Origin) |
| Scanner | `pkg/plugin/scanner.go` | 扫描build.go文件，返回[]Source |
| Compiler | `pkg/plugin/compiler.go` | 编译build.go为plugin.so |
| Loader | `pkg/plugin/loader.go` | 加载plugin.so，返回LoadedPlugin |
| Extractor | `pkg/plugin/extractor.go` | 从LoadedPlugin提取api.Package |
| **包管理** | `pkg/repo/` | 第三方包管理 |
| Resolver | `pkg/repo/resolver.go` | 依赖解析，拓扑排序 |
| Manager | `pkg/repo/manager.go` | 仓库管理 |
| Source | `pkg/repo/source.go` | 源码管理 |
| Installer | `pkg/repo/installer.go` | 包安装 |
| **构建系统** | `pkg/build/` | 构建执行 |
| Graph | `pkg/build/graph.go` | 构建图 |
| Scheduler | `pkg/build/scheduler.go` | 调度器，并行编译 |
| Compiler | `pkg/build/compiler.go` | C/C++编译 |
| Linker | `pkg/build/linker.go` | 链接 |

## 9. 目录结构

```
~/.vmake/
├── repos/
│   └── official/              # 仓库索引
│       └── packages/
│           ├── z/zlib/
│           ├── m/mbedtls/
│           └── ...
├── sources/
│   └── official/
│       └── zlib/
│           └── repo/          # git clone 的源码
├── packages/
│   └── official/
│       └── zlib/
│           └── 1.2.13/
│               └── {cache-hash}/
│                   ├── build/
│                   └── install/
│                       ├── include/
│                       └── lib/
└── cache/
    └── plugins/               # 编译的插件缓存

项目目录/
├── build.go                   # 本地包定义
├── .vmake/
│   └── config.json           # 项目配置
└── build/
    ├── gcc-debug/            # 构建输出
    │   ├── objects/
    │   └── libxxx.a
    └── compile_commands.json
```
