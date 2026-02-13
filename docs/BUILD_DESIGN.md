# VMake 编译功能完整设计

---

## 1. 概述

### 1.1 目标

实现基于依赖图的 C/C++ 编译系统，支持：
- 依赖解析与拓扑排序
- 增量编译（精确头文件依赖）
- 工具链切换自动触发全量重建
- 多目标类型（binary/static/shared/object）

### 1.2 设计原则

- **每个 Package 独立**：缓存、objects、输出都在各自的 `build/` 目录
- **精确依赖追踪**：使用 GCC `-MMD` 生成 `.d` 文件
- **工具链感知**：编译器变化时自动全量重建

---

## 2. 目录结构

```
project/
├── .vmake/
│   └── config.json              # 全局配置（含 toolchain 字段）
│
├── build.go                     # 根 package
├── build/
│   ├── plugin.so               # Go 插件
│   ├── cache.json              # 编译缓存
│   ├── objects/
│   │   ├── main.o
│   │   └── main.o.d            # GCC 生成的依赖文件
│   └── myapp                   # 输出产物
│
├── lib/
│   ├── build.go
│   └── build/
│       ├── plugin.so
│       ├── cache.json
│       ├── objects/
│       │   ├── utils.o
│       │   ├── utils.o.d
│       │   ├── core.o
│       │   └── core.o.d
│       └── libutils.a
│
└── app/
    ├── build.go
    └── build/
        ├── plugin.so
        ├── cache.json
        ├── objects/
        └── app
```

---

## 3. 核心数据结构

### 3.1 BuildCache（`pkg/build/cache.go`）

```go
package build

type BuildCache struct {
    Version   int                `json:"version"`    // 缓存格式版本 = 1
    Toolchain ToolchainMeta      `json:"toolchain"`  // 工具链信息
    Sources   map[string]*Source `json:"sources"`    // 源文件路径 -> 编译信息
}

type ToolchainMeta struct {
    Name    string `json:"name"`      // "gcc", "arm-gcc"
    CCPath  string `json:"cc_path"`   // /usr/bin/gcc (绝对路径)
    CXXPath string `json:"cxx_path"`  // /usr/bin/g++
    Host    string `json:"host"`      // x86_64-linux-gnu
}

type Source struct {
    ModTime int64    `json:"mod_time"`  // 编译时源文件的 mtime
    ObjPath string   `json:"obj_path"`  // "objects/main.o" (相对 buildDir)
    Deps    []string `json:"deps"`      // 依赖的头文件绝对路径列表
}

// 方法
func NewBuildCache(tc *toolchain.Toolchain) *BuildCache
func LoadCache(buildDir string) (*BuildCache, error)
func (c *BuildCache) Save(buildDir string) error
func (c *BuildCache) NeedFullRebuild(tc *toolchain.Toolchain) bool
func (c *BuildCache) NeedRebuild(sourcePath string) bool
func (c *BuildCache) Update(sourcePath, objPath string, deps []string)
```

### 3.2 BuildGraph（`pkg/build/graph.go`）

```go
package build

type BuildGraph struct {
    Nodes map[string]*BuildNode // "pkg:target" -> node
    Order []string              // 拓扑排序结果
}

type BuildNode struct {
    FullName string       // "pkg:target" 全限定名
    PkgName  string       // package 名称
    Target   *api.Target  // 目标定义
    Deps     []string     // 依赖的全限定名列表
}

// 方法
func BuildGraph(targets map[string]map[string]*api.Target) (*BuildGraph, error)
// 返回 error 表示存在循环依赖
```

### 3.3 ResolvedTarget（`pkg/build/scheduler.go`）

```go
package build

// ResolvedTarget 解析后的目标（包含展开后的所有信息）
type ResolvedTarget struct {
    Node         *BuildNode
    BuildDir     string   // 绝对路径：/path/to/pkg/build
    SourceFiles  []string // 解析 glob 后的源文件绝对路径
    AllIncludes  []string // 自身 Includes + 依赖的 PublicIncludes
    AllDefines   []string // 自身 Defines
    AllCFlags    []string // 工具链默认 + 自身 CFlags
    AllCxxFlags  []string // 工具链默认 + 自身 CxxFlags
    AllLdFlags   []string // 工具链默认 + 自身 LdFlags
    DepArtifacts []string // 依赖的产物路径（.a/.so）
    OutputPath   string   // 输出文件绝对路径
}
```

---

## 4. 模块设计

### 4.1 Glob 模式匹配（`internal/glob/glob.go`）

```go
package glob

// Match 在 dir 目录下匹配 pattern
// 支持：*.c, **/*.cpp, src/*.c
// 返回匹配文件的绝对路径列表
func Match(pattern, dir string) ([]string, error)

// 示例：
// Match("*.c", "/project/lib") -> ["/project/lib/a.c", "/project/lib/b.c"]
// Match("**/*.cpp", "/project") -> ["/project/src/main.cpp", "/project/lib/util.cpp"]
```

### 4.2 编译器（`pkg/build/compiler.go`）

```go
package build

type Compiler struct {
    tc       *toolchain.Toolchain
    ccPath   string  // 解析后的绝对路径
    cxxPath  string  // 解析后的绝对路径
}

func NewCompiler(tc *toolchain.Toolchain) (*Compiler, error)

// Compile 编译单个源文件
// 返回 .d 文件中解析出的头文件依赖列表
func (c *Compiler) Compile(src, objPath string, opts *CompileOptions) ([]string, error)

type CompileOptions struct {
    Includes []string
    Defines  []string
    CFlags   []string   // C 专用
    CxxFlags []string   // C++ 专用
    Language string     // "c" or "cxx"，根据源文件扩展名判断
}

// 内部方法
func (c *Compiler) buildArgs(opts *CompileOptions, objPath, src string) []string
func parseDepFile(depPath string) ([]string, error)
```

**编译命令组装**：

```bash
# C 文件
gcc -c -MMD -MP -Iinclude -DDEBUG=1 -O2 -Wall -o objects/main.o src/main.c

# C++ 文件
g++ -c -MMD -MP -Iinclude -DDEBUG=1 -O2 -Wall -std=c++17 -o objects/main.o src/main.cpp
```

**.d 文件解析**：

```
# main.o.d 内容：
main.o: src/main.c include/header.h /usr/include/stdio.h

# parseDepFile 提取：["src/main.c", "include/header.h", "/usr/include/stdio.h"]
# 实际只需要头文件，跳过第一个（源文件本身）
```

### 4.3 链接器（`pkg/build/linker.go`）

```go
package build

type Linker struct {
    tc      *toolchain.Toolchain
    ccPath  string  // 用于链接（gcc/g++ 作驱动）
    arPath  string
}

func NewLinker(tc *toolchain.Toolchain) (*Linker, error)

// LinkBinary 链接可执行文件
func (l *Linker) LinkBinary(objs, libs, ldflags []string, outputPath string) error

// LinkStatic 打包静态库
func (l *Linker) LinkStatic(objs []string, outputPath string) error

// LinkShared 链接动态库
func (l *Linker) LinkShared(objs, ldflags []string, outputPath string) error
```

**链接命令**：

```bash
# Binary
gcc -o build/myapp objects/main.o -L/path -lutils -Wl,--as-needed

# Static
ar rcs build/libutils.a objects/utils.o objects/core.o
ranlib build/libutils.a  # 可选，建立索引

# Shared
gcc -shared -o build/libutils.so objects/utils.o -Wl,-soname,libutils.so
```

### 4.4 缓存管理（`pkg/build/cache.go`）

```go
package build

const CacheVersion = 1

func NewBuildCache(tc *toolchain.Toolchain) *BuildCache {
    ccPath, _ := toolchain.ResolveToolPath(tc.Tools.CC, tc.InstallPath)
    cxxPath, _ := toolchain.ResolveToolPath(tc.Tools.CXX, tc.InstallPath)
    
    return &BuildCache{
        Version: CacheVersion,
        Toolchain: ToolchainMeta{
            Name:    tc.Name,
            CCPath:  ccPath,
            CXXPath: cxxPath,
            Host:    tc.Host,
        },
        Sources: make(map[string]*Source),
    }
}

func LoadCache(buildDir string) (*BuildCache, error) {
    path := filepath.Join(buildDir, "cache.json")
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    
    var cache BuildCache
    if err := json.Unmarshal(data, &cache); err != nil {
        return nil, err
    }
    return &cache, nil
}

func (c *BuildCache) Save(buildDir string) error {
    path := filepath.Join(buildDir, "cache.json")
    data, err := json.MarshalIndent(c, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(path, data, 0644)
}

func (c *BuildCache) NeedFullRebuild(tc *toolchain.Toolchain) bool {
    ccPath, _ := toolchain.ResolveToolPath(tc.Tools.CC, tc.InstallPath)
    cxxPath, _ := toolchain.ResolveToolPath(tc.Tools.CXX, tc.InstallPath)
    
    return c.Toolchain.Name != tc.Name ||
           c.Toolchain.CCPath != ccPath ||
           c.Toolchain.CXXPath != cxxPath
}

func (c *BuildCache) NeedRebuild(sourcePath string) bool {
    src, ok := c.Sources[sourcePath]
    if !ok {
        return true
    }
    
    // .o 不存在
    if _, err := os.Stat(src.ObjPath); os.IsNotExist(err) {
        return true
    }
    
    // 获取源文件 mtime
    info, err := os.Stat(sourcePath)
    if err != nil {
        return true
    }
    srcModTime := info.ModTime().Unix()
    
    // 源文件变化
    if srcModTime > src.ModTime {
        return true
    }
    
    // 头文件变化（使用源文件 mtime 作为参考）
    for _, dep := range src.Deps {
        depInfo, err := os.Stat(dep)
        if err != nil {
            return true  // 头文件被删除
        }
        if depInfo.ModTime().Unix() > src.ModTime {
            return true
        }
    }
    
    return false
}

func (c *BuildCache) Update(sourcePath, objPath string, deps []string) {
    info, _ := os.Stat(sourcePath)
    
    c.Sources[sourcePath] = &Source{
        ModTime: info.ModTime().Unix(),
        ObjPath: objPath,
        Deps:    deps,
    }
}

// CleanObjects 清理 objects 目录
func CleanObjects(buildDir string) error {
    objectsDir := filepath.Join(buildDir, "objects")
    return os.RemoveAll(objectsDir)
}
```

### 4.5 调度器（`pkg/build/scheduler.go`）

```go
package build

type Scheduler struct {
    graph     *BuildGraph
    compiler  *Compiler
    linker    *Linker
    toolchain *toolchain.Toolchain
    
    // 每个 package 的缓存和构建目录
    pkgInfos map[string]*PkgInfo
}

type PkgInfo struct {
    BuildDir string
    Cache    *BuildCache
}

func NewScheduler(
    graph *BuildGraph,
    tc *toolchain.Toolchain,
    pkgBuildDirs map[string]string,  // pkgName -> buildDir
) (*Scheduler, error) {
    
    compiler, err := NewCompiler(tc)
    if err != nil {
        return nil, err
    }
    
    linker, err := NewLinker(tc)
    if err != nil {
        return nil, err
    }
    
    s := &Scheduler{
        graph:     graph,
        compiler:  compiler,
        linker:    linker,
        toolchain: tc,
        pkgInfos:  make(map[string]*PkgInfo),
    }
    
    // 加载每个 package 的缓存
    for pkgName, buildDir := range pkgBuildDirs {
        cache, err := LoadCache(buildDir)
        if err != nil {
            cache = NewBuildCache(tc)
        }
        
        // 检测工具链变化
        if cache.NeedFullRebuild(tc) {
            CleanObjects(buildDir)
            cache = NewBuildCache(tc)
        }
        
        s.pkgInfos[pkgName] = &PkgInfo{
            BuildDir: buildDir,
            Cache:    cache,
        }
    }
    
    return s, nil
}

// BuildAll 构建所有目标
func (s *Scheduler) BuildAll() error {
    for _, fullName := range s.graph.Order {
        if err := s.Build(fullName); err != nil {
            return err
        }
    }
    return nil
}

// Build 构建单个目标
func (s *Scheduler) Build(fullName string) error {
    node := s.graph.Nodes[fullName]
    if node == nil {
        return fmt.Errorf("target not found: %s", fullName)
    }
    
    // 跳过非默认目标
    if !node.Target.IsDefault() {
        return nil
    }
    
    // 解析目标信息
    resolved, err := s.resolveTarget(node)
    if err != nil {
        return err
    }
    
    // 确保目录存在
    os.MkdirAll(filepath.Join(resolved.BuildDir, "objects"), 0755)
    
    // 编译每个源文件
    var objs []string
    for _, src := range resolved.SourceFiles {
        objRel, deps, err := s.compileSource(resolved, src)
        if err != nil {
            return err
        }
        objs = append(objs, filepath.Join(resolved.BuildDir, objRel))
    }
    
    // 链接
    if err := s.link(resolved, objs); err != nil {
        return err
    }
    
    // 保存缓存
    pkgInfo := s.pkgInfos[node.PkgName]
    return pkgInfo.Cache.Save(pkgInfo.BuildDir)
}

func (s *Scheduler) resolveTarget(node *BuildNode) (*ResolvedTarget, error) {
    pkgInfo := s.pkgInfos[node.PkgName]
    
    resolved := &ResolvedTarget{
        Node:        node,
        BuildDir:    pkgInfo.BuildDir,
        AllIncludes: append([]string{}, node.Target.Includes()...),
        AllDefines:  append([]string{}, node.Target.Defines()...),
        AllCFlags:   append([]string{}, s.toolchain.DefaultFlags.CFlags...),
        AllCxxFlags: append([]string{}, s.toolchain.DefaultFlags.CxxFlags...),
        AllLdFlags:  append([]string{}, s.toolchain.DefaultFlags.LdFlags...),
    }
    
    // 添加用户自定义 flags
    resolved.AllCFlags = append(resolved.AllCFlags, node.Target.CFlags()...)
    resolved.AllCxxFlags = append(resolved.AllCxxFlags, node.Target.CxxFlags()...)
    resolved.AllLdFlags = append(resolved.AllLdFlags, node.Target.LdFlags()...)
    
    // 收集依赖的 PublicIncludes 和产物路径
    for _, depName := range node.Deps {
        depNode := s.graph.Nodes[depName]
        if depNode == nil {
            return nil, fmt.Errorf("dependency not found: %s", depName)
        }
        
        // 继承公开头文件
        resolved.AllIncludes = append(resolved.AllIncludes, depNode.Target.PublicIncludes()...)
        
        // 收集依赖产物
        depOutput := s.getTargetOutputPath(depNode)
        resolved.DepArtifacts = append(resolved.DepArtifacts, depOutput)
    }
    
    // 解析 glob
    srcDir := filepath.Dir(pkgInfo.BuildDir)  // build.go 所在目录
    for _, pattern := range node.Target.Files() {
        files, err := glob.Match(pattern, srcDir)
        if err != nil {
            return nil, err
        }
        resolved.SourceFiles = append(resolved.SourceFiles, files...)
    }
    
    // 设置输出路径
    resolved.OutputPath = s.getTargetOutputPath(node)
    
    return resolved, nil
}

func (s *Scheduler) getTargetOutputPath(node *BuildNode) string {
    pkgInfo := s.pkgInfos[node.PkgName]
    
    var name string
    switch node.Target.Kind() {
    case api.TargetBinary:
        name = node.Target.Name()
    case api.TargetStatic:
        name = "lib" + node.Target.Name() + ".a"
    case api.TargetShared:
        name = "lib" + node.Target.Name() + ".so"
    case api.TargetObject:
        name = node.Target.Name() + ".o"
    }
    
    return filepath.Join(pkgInfo.BuildDir, name)
}

func (s *Scheduler) compileSource(resolved *ResolvedTarget, src string) (string, []string, error) {
    pkgInfo := s.pkgInfos[resolved.Node.PkgName]
    
    // 计算对象文件路径
    srcRel, _ := filepath.Rel(filepath.Dir(resolved.BuildDir), src)
    objRel := "objects/" + strings.ReplaceAll(srcRel, "/", "_") + ".o"
    objPath := filepath.Join(resolved.BuildDir, objRel)
    
    // 检查是否需要重编译
    if !pkgInfo.Cache.NeedRebuild(src) {
        return objRel, pkgInfo.Cache.Sources[src].Deps, nil
    }
    
    // 判断语言
    lang := "c"
    if strings.HasSuffix(src, ".cpp") || strings.HasSuffix(src, ".cc") || 
       strings.HasSuffix(src, ".cxx") || strings.HasSuffix(src, ".C") {
        lang = "cxx"
    }
    
    // 编译
    opts := &CompileOptions{
        Includes: resolved.AllIncludes,
        Defines:  resolved.AllDefines,
        CFlags:   resolved.AllCFlags,
        CxxFlags: resolved.AllCxxFlags,
        Language: lang,
    }
    
    deps, err := s.compiler.Compile(src, objPath, opts)
    if err != nil {
        return "", nil, err
    }
    
    // 更新缓存
    pkgInfo.Cache.Update(src, objRel, deps)
    
    return objRel, deps, nil
}

func (s *Scheduler) link(resolved *ResolvedTarget, objs []string) error {
    switch resolved.Node.Target.Kind() {
    case api.TargetBinary:
        return s.linker.LinkBinary(objs, resolved.Node.Target.Links(), 
                                   resolved.AllLdFlags, resolved.OutputPath)
    case api.TargetStatic:
        return s.linker.LinkStatic(objs, resolved.OutputPath)
    case api.TargetShared:
        return s.linker.LinkShared(objs, resolved.AllLdFlags, resolved.OutputPath)
    case api.TargetObject:
        // 单文件目标，直接复制
        if len(objs) == 1 {
            return os.Rename(objs[0], resolved.OutputPath)
        }
        return fmt.Errorf("object target requires exactly one source file")
    }
    return nil
}
```

---

## 5. CLI 集成（`cmd/vmake/main.go`）

### 5.1 更新 runBuild

```go
func runBuild() {
    workDir, packages, loadResults, allOptions, cfg, _, err := prepareBuild()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }

    // 收集所有 Targets
    allTargets := make(map[string]map[string]*api.Target)
    for _, lr := range loadResults {
        if !lr.Success {
            continue
        }
        pkgName := lr.Package.Name
        pc := config.GetPackageConfig(cfg, pkgName)
        buildCtx := api.NewBuildContext(pkgName, pc.Options)
        buildCtx.SetOptions(allOptions[pkgName])
        
        for _, fn := range lr.Loaded.Builder.GetBuildFuncs() {
            fn(buildCtx)
        }
        allTargets[pkgName] = buildCtx.GetTargets()
    }

    // 选择工具链
    mgr := toolchain.GetManager()
    tcName := cfg.Toolchain
    if tcName == "" {
        tcName = mgr.GetDefaultToolchain()
    }
    tc, err := mgr.SelectToolchain(tcName)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Toolchain error: %v\n", err)
        os.Exit(1)
    }

    // 构建依赖图
    graph, err := build.BuildGraph(allTargets)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Dependency error: %v\n", err)
        os.Exit(1)
    }

    // 收集每个 package 的 buildDir
    pkgBuildDirs := make(map[string]string)
    for _, pkg := range packages {
        pkgBuildDirs[pkg.Name] = filepath.Join(filepath.Dir(pkg.Path), "build")
    }

    // 创建调度器并构建
    scheduler, err := build.NewScheduler(graph, tc, pkgBuildDirs)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Scheduler error: %v\n", err)
        os.Exit(1)
    }

    if err := scheduler.BuildAll(); err != nil {
        fmt.Fprintf(os.Stderr, "Build failed: %v\n", err)
        os.Exit(1)
    }

    fmt.Println("Build succeeded!")
}
```

### 5.2 添加 clean 命令

```go
func runClean() {
    workDir, packages, _, _, _, _, err := prepareBuild()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }

    for _, pkg := range packages {
        buildDir := filepath.Join(filepath.Dir(pkg.Path), "build")
        objectsDir := filepath.Join(buildDir, "objects")
        
        if _, err := os.Stat(objectsDir); err == nil {
            if err := os.RemoveAll(objectsDir); err != nil {
                fmt.Fprintf(os.Stderr, "Failed to clean %s: %v\n", pkg.Name, err)
                continue
            }
            fmt.Printf("Cleaned %s/objects/\n", pkg.Name)
        }
        
        // 同时删除 cache.json
        cachePath := filepath.Join(buildDir, "cache.json")
        os.Remove(cachePath)
    }

    fmt.Println("Clean completed!")
}
```

### 5.3 添加 rebuild 命令

```go
func runRebuild() {
    runClean()
    runBuild()
}
```

### 5.4 更新 main 函数

```go
func main() {
    if len(os.Args) < 2 {
        runBuild()
        return
    }

    cmd := os.Args[1]
    switch cmd {
    case "config":
        runConfig()
    case "build":
        runBuild()
    case "clean":
        runClean()
    case "rebuild":
        runRebuild()
    case "toolchain":
        runToolchain(os.Args[2:])
    default:
        fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
        fmt.Println("Usage: vmake [build|config|clean|rebuild|toolchain]")
        os.Exit(1)
    }
}
```

---

## 6. 文件清单

| 文件 | 职责 |
|------|------|
| `internal/glob/glob.go` | Glob 模式匹配（`*.c`, `**/*.cpp`） |
| `pkg/build/graph.go` | 依赖图构建、拓扑排序、循环检测 |
| `pkg/build/compiler.go` | 编译器封装，`-MMD -MP` 生成 .d 文件 |
| `pkg/build/linker.go` | 链接器封装 |
| `pkg/build/cache.go` | 缓存管理，工具链感知，头文件依赖 |
| `pkg/build/scheduler.go` | 构建调度，整合所有组件 |
| `cmd/vmake/main.go` | CLI 更新，添加 clean/rebuild |

---

## 7. 实现步骤

| 步骤 | 内容 | 依赖 |
|------|------|------|
| 1 | `internal/glob/glob.go` | 无 |
| 2 | `pkg/build/graph.go` | 无 |
| 3 | `pkg/build/cache.go` | 无 |
| 4 | `pkg/build/compiler.go` | 步骤 1 |
| 5 | `pkg/build/linker.go` | 无 |
| 6 | `pkg/build/scheduler.go` | 步骤 2-5 |
| 7 | `cmd/vmake/main.go` 更新 | 步骤 6 |
| 8 | 测试：`test_data/01_simple_c` | 步骤 7 |
| 9 | 测试：`test_data/04_multi_module` | 步骤 8 |

---

## 8. 测试计划

### 8.1 单元测试

```go
// internal/glob/glob_test.go
func TestMatch_SingleStar(t *testing.T)
func TestMatch_DoubleStar(t *testing.T)

// pkg/build/graph_test.go
func TestBuildGraph_NoDeps(t *testing.T)
func TestBuildGraph_WithDeps(t *testing.T)
func TestBuildGraph_Circular(t *testing.T)

// pkg/build/cache_test.go
func TestCache_ToolchainChange(t *testing.T)
func TestCache_HeaderChange(t *testing.T)
```

### 8.2 集成测试

```bash
# 1. 简单 C 项目
cd test_data/01_simple_c
vmake build
./build/hello

# 2. 多模块项目
cd test_data/04_multi_module
vmake build
./app/build/app

# 3. 增量编译
touch lib/utils.c
vmake build  # 只重编译 utils.c

# 4. 工具链切换
vmake config  # 选择 arm-gcc
vmake build   # 全量重建

# 5. clean/rebuild
vmake clean
vmake rebuild
```

---

## 9. 数据流图

```
┌─────────────────────────────────────────────────────────────────┐
│                          runBuild()                              │
├─────────────────────────────────────────────────────────────────┤
│  1. prepareBuild()                                               │
│     ├── Scan build.go files                                      │
│     ├── Compile plugins                                          │
│     ├── Load plugins                                             │
│     ├── Execute OnConfig (collect options)                       │
│     └── Execute OnBuild (collect targets)                        │
│                                                                  │
│  2. Select toolchain                                             │
│     project config → global default → builtin gcc                │
│                                                                  │
│  3. BuildGraph(targets)                                          │
│     ├── Parse Deps (resolve "pkg:target" format)                 │
│     ├── Build nodes                                              │
│     └── Topological sort → Order                                 │
│                                                                  │
│  4. NewScheduler(graph, toolchain, pkgBuildDirs)                 │
│     ├── Load cache per package                                   │
│     ├── Check toolchain change → full rebuild                    │
│     └── Init Compiler & Linker                                   │
│                                                                  │
│  5. scheduler.BuildAll()                                         │
│     └── For each target in Order:                                │
│         ├── resolveTarget()                                      │
│         │   ├── Merge includes (self + deps' PublicIncludes)     │
│         │   ├── Resolve glob → source files                      │
│         │   └── Collect dep artifacts (.a/.so)                   │
│         │                                                        │
│         ├── For each source file:                                │
│         │   ├── cache.NeedRebuild()?                             │
│         │   │   ├── Yes → Compiler.Compile() with -MMD -MP       │
│         │   │   │         → parse .d file → deps                 │
│         │   │   │         → cache.Update()                       │
│         │   │   └── No  → skip, use cached .o                   │
│         │   └── collect .o paths                                 │
│         │                                                        │
│         └── Linker.Link*(objs, output)                           │
│             └── Binary: gcc -o ...                               │
│             └── Static: ar rcs ...                               │
│             └── Shared: gcc -shared ...                          │
│                                                                  │
│  6. Save cache per package                                       │
└─────────────────────────────────────────────────────────────────┘
```

---

## 10. 增量编译判断流程

```
┌──────────────────────────────────────────────────────────────┐
│                    NeedRebuild(sourcePath)                   │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌─────────────────┐                                         │
│  │ 缓存中有记录？  │──── No ────► return true (需要编译)     │
│  └────────┬────────┘                                         │
│           │ Yes                                              │
│           ▼                                                  │
│  ┌─────────────────┐                                         │
│  │  .o 文件存在？  │──── No ────► return true (需要编译)     │
│  └────────┬────────┘                                         │
│           │ Yes                                              │
│           ▼                                                  │
│  ┌─────────────────────┐                                     │
│  │ 源文件 mtime > 缓存 │──── Yes ──► return true (需要编译) │
│  │ 记录的 mtime？      │                                     │
│  └────────┬────────────┘                                     │
│           │ No                                               │
│           ▼                                                  │
│  ┌─────────────────────────┐                                 │
│  │ For each dep in Deps:   │                                 │
│  │   头文件 mtime > 缓存   │──── Yes ──► return true        │
│  │   的 mtime？            │                                 │
│  └────────┬────────────────┘                                 │
│           │ No (所有头文件未变化)                            │
│           ▼                                                  │
│     return false (跳过编译)                                  │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

---

## 11. 工具链变化检测

```
┌──────────────────────────────────────────────────────────────┐
│                  NeedFullRebuild(toolchain)                  │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  缓存中的工具链信息：                                         │
│  {                                                           │
│    "name": "gcc",                                            │
│    "cc_path": "/usr/bin/gcc",                                │
│    "cxx_path": "/usr/bin/g++",                               │
│    "host": "x86_64-linux-gnu"                                │
│  }                                                           │
│                                                              │
│  当前选择的工具链：                                           │
│  {                                                           │
│    "name": "arm-gcc",          ← 变化！                      │
│    "cc_path": "/opt/arm/bin/arm-gcc",                        │
│    ...                                                       │
│  }                                                           │
│                                                              │
│  检测条件（任一满足则触发全量重建）：                          │
│  1. name 不同                                                │
│  2. cc_path 不同（编译器升级/更换）                          │
│  3. cxx_path 不同                                            │
│                                                              │
│  全量重建操作：                                               │
│  1. 删除 objects/ 目录                                       │
│  2. 重置缓存                                                 │
│  3. 重新编译所有源文件                                       │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```
