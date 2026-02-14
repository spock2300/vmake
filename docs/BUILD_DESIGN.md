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
│   └── config.json              # 全局配置（含 toolchain、mode 字段）
│
├── build.go                     # 根 package
├── build/
│   ├── plugin.so               # Go 插件
│   ├── compile_commands.json   # LSP 编译数据库
│   ├── gcc-debug/              # gcc 工具链 debug 模式
│   │   ├── cache.json
│   │   ├── objects/
│   │   │   ├── main.o
│   │   │   └── main.o.d
│   │   └── myapp
│   └── gcc-release/            # gcc 工具链 release 模式
│       ├── cache.json
│       ├── objects/
│       └── myapp
│
├── lib/
│   ├── build.go
│   └── build/
│       ├── plugin.so
│       ├── gcc-debug/
│       │   ├── cache.json
│       │   ├── objects/
│       │   └── libutils.a
│       └── gcc-release/
│
└── app/
    ├── build.go
    └── build/
        ├── plugin.so
        └── gcc-debug/
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
    Version   int                `json:"version"`    // 缓存格式版本 = 3
    Toolchain ToolchainMeta      `json:"toolchain"`  // 工具链信息
    Mode      string             `json:"mode,omitempty"` // 构建模式（debug/release）
    Sources   map[string]*Source `json:"sources"`    // 源文件路径 -> 编译信息
    mu        sync.RWMutex       `json:"-"`          // 并发锁（不序列化）
}

type ToolchainMeta struct {
    Name    string `json:"name"`      // "gcc", "arm-gcc"
    CCPath  string `json:"cc_path"`   // /usr/bin/gcc (绝对路径)
    CXXPath string `json:"cxx_path"`  // /usr/bin/g++
    Host    string `json:"host"`      // x86_64-linux-gnu
}

type Source struct {
    ModTime int64    `json:"mod_time"`  // 编译时源文件及依赖的最大 mtime
    ObjPath string   `json:"obj_path"`  // "build/gcc-debug/objects/main.o" (相对路径)
    Deps    []string `json:"deps"`      // 依赖的头文件绝对路径列表
}

// 方法
func NewBuildCache(tc *toolchain.Toolchain) *BuildCache
func LoadCache(tcName string) (*BuildCache, error)  // 从 build/{tcName}/cache.json 加载
func (c *BuildCache) Save(tcName string) error     // 保存到 build/{tcName}/cache.json
func (c *BuildCache) NeedFullRebuild(tc *toolchain.Toolchain) bool
func (c *BuildCache) NeedRebuild(sourcePath string) bool
func (c *BuildCache) GetIfValid(sourcePath string) *Source  // 返回有效的缓存或 nil
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
    SourceFiles  []string // 解析 glob 后的源文件相对路径
    AllIncludes  []string // 自身 Includes + PublicIncludes + 依赖的 PublicIncludes
    AllDefines   []string // 自身 Defines + 模式相关定义
    AllCFlags    []string // 工具链默认 + 模式标志 + 自身 CFlags
    AllCxxFlags  []string // 工具链默认 + 模式标志 + 自身 CxxFlags
    AllLdFlags   []string // 工具链默认 + 自身 LdFlags
    DepArtifacts []string // 依赖的产物路径（.a/.so）
    OutputPath   string   // 输出文件相对路径
}

// PkgInfo Package 构建信息
type PkgInfo struct {
    Dir   string      // Package 目录绝对路径
    Cache *BuildCache // 该 Package 的构建缓存
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

const CacheVersion = 3

func NewBuildCache(tc *toolchain.Toolchain) *BuildCache {
    ccPath, _ := toolchain.ResolveToolPath(tc.Tools.CC, tc.InstallPath)
    cxxPath, _ := toolchain.ResolveToolPath(tc.Tools.CXX, tc.InstallPath)
    host := tc.Host
    if host == "" {
        host = toolchain.GetToolchainHost(tc)
    }
    
    return &BuildCache{
        Version: CacheVersion,
        Toolchain: ToolchainMeta{
            Name:    tc.Name,
            CCPath:  ccPath,
            CXXPath: cxxPath,
            Host:    host,
        },
        Sources: make(map[string]*Source),
    }
}

func LoadCache(buildDir string) (*BuildCache, error) {
    path := fmt.Sprintf("build/%s/cache.json", buildDir)
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    
    var cache BuildCache
    if err := json.Unmarshal(data, &cache); err != nil {
        return nil, fmt.Errorf("failed to parse cache: %w", err)
    }
    return &cache, nil
}

func (c *BuildCache) Save(buildDir string) error {
    dir := fmt.Sprintf("build/%s", buildDir)
    if err := os.MkdirAll(dir, 0755); err != nil {
        return err
    }
    data, err := json.MarshalIndent(c, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(fmt.Sprintf("%s/cache.json", dir), data, 0644)
}

func (c *BuildCache) NeedFullRebuild(tc *toolchain.Toolchain) bool {
    ccPath, _ := toolchain.ResolveToolPath(tc.Tools.CC, tc.InstallPath)
    cxxPath, _ := toolchain.ResolveToolPath(tc.Tools.CXX, tc.InstallPath)
    
    return c.Toolchain.Name != tc.Name ||
           c.Toolchain.CCPath != ccPath ||
           c.Toolchain.CXXPath != cxxPath
}

func (c *BuildCache) NeedRebuild(sourcePath string) bool {
    return c.GetIfValid(sourcePath) == nil
}

func (c *BuildCache) GetIfValid(sourcePath string) *Source {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    src, ok := c.Sources[sourcePath]
    if !ok {
        return nil
    }
    
    // .o 不存在
    if _, err := os.Stat(src.ObjPath); os.IsNotExist(err) {
        return nil
    }
    
    // 获取源文件 mtime
    info, err := os.Stat(sourcePath)
    if err != nil {
        return nil
    }
    
    // 源文件或头文件变化（使用最大 mtime 比较）
    if info.ModTime().Unix() > src.ModTime {
        return nil
    }
    
    for _, dep := range src.Deps {
        depInfo, err := os.Stat(dep)
        if err != nil || depInfo.ModTime().Unix() > src.ModTime {
            return nil
        }
    }
    
    return &Source{
        ModTime: src.ModTime,
        ObjPath: src.ObjPath,
        Deps:    src.Deps,
    }
}

func (c *BuildCache) Update(sourcePath, objPath string, deps []string) {
    // 使用源文件和所有依赖的最大 mtime
    maxModTime := int64(0)
    
    if info, err := os.Stat(sourcePath); err == nil {
        maxModTime = info.ModTime().Unix()
    }
    
    for _, dep := range deps {
        if info, err := os.Stat(dep); err == nil {
            if t := info.ModTime().Unix(); t > maxModTime {
                maxModTime = t
            }
        }
    }
    
    c.mu.Lock()
    c.Sources[sourcePath] = &Source{
        ModTime: maxModTime,
        ObjPath: objPath,
        Deps:    deps,
    }
    c.mu.Unlock()
}

// CleanObjects 清理 objects 目录
func CleanObjects(buildDir string) error {
    return os.RemoveAll(fmt.Sprintf("build/%s/objects", buildDir))
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
    tcName    string            // 工具链名称
    mode      string            // 构建模式（debug/release）
    buildDir  string            // 构建目录名（{tcName}-{mode}）
    pkgs      map[string]*PkgInfo
    origDir   string            // 原始工作目录
    ccWriter  *CompileCommandsWriter  // compile_commands.json 生成器
}

type PkgInfo struct {
    Dir   string      // Package 目录绝对路径
    Cache *BuildCache // 该 Package 的构建缓存
}

func NewScheduler(
    graph *BuildGraph,
    tc *toolchain.Toolchain,
    pkgDirs map[string]string,  // pkgName -> 绝对路径
    mode string,                // "debug" 或 "release"
) (*Scheduler, error) {
    
    compiler, err := NewCompiler(tc)
    if err != nil {
        return nil, err
    }
    
    linker, err := NewLinker(tc)
    if err != nil {
        return nil, err
    }
    
    origDir, _ := os.Getwd()
    
    tcName := tc.Name
    if mode == "" {
        mode = api.ModeDebug
    }
    buildDir := fmt.Sprintf("%s-%s", tcName, mode)
    
    ccWriter, err := NewCompileCommandsWriter(tc)
    if err != nil {
        return nil, err
    }
    
    s := &Scheduler{
        graph:     graph,
        compiler:  compiler,
        linker:    linker,
        toolchain: tc,
        tcName:    tcName,
        mode:      mode,
        buildDir:  buildDir,
        pkgs:      make(map[string]*PkgInfo),
        origDir:   origDir,
        ccWriter:  ccWriter,
    }
    
    // 加载每个 package 的缓存
    for pkgName, pkgDir := range pkgDirs {
        if err := os.Chdir(pkgDir); err != nil {
            os.Chdir(origDir)
            return nil, fmt.Errorf("failed to chdir to %s: %w", pkgDir, err)
        }
        
        cache, err := LoadCache(buildDir)
        if err != nil {
            cache = NewBuildCache(tc)
        }
        
        // 检测工具链变化或模式变化
        if cache.NeedFullRebuild(tc) || cache.Mode != mode {
            CleanObjects(buildDir)
            cache = NewBuildCache(tc)
            cache.Mode = mode
        }
        
        s.pkgs[pkgName] = &PkgInfo{
            Dir:   pkgDir,
            Cache: cache,
        }
    }
    
    os.Chdir(origDir)
    return s, nil
}

// BuildAll 构建所有目标
func (s *Scheduler) BuildAll() error {
    for _, fullName := range s.graph.Order {
        if err := s.Build(fullName); err != nil {
            return err
        }
    }
    
    // 生成 compile_commands.json
    return s.ccWriter.Save("build/compile_commands.json")
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
    
    pkgInfo := s.pkgs[node.PkgName]
    
    // 切换到 Package 目录
    if err := os.Chdir(pkgInfo.Dir); err != nil {
        return err
    }
    defer os.Chdir(s.origDir)
    
    s.ccWriter.SetPackageDir(pkgInfo.Dir)
    
    vlog.Info("[%s]", fullName)
    
    resolved, err := s.resolveTarget(node)
    if err != nil {
        return err
    }
    
    os.MkdirAll(fmt.Sprintf("build/%s/objects", s.buildDir), 0755)
    
    numFiles := len(resolved.SourceFiles)
    if numFiles == 0 {
        return s.link(resolved, nil)
    }
    
    // 并行编译
    numWorkers := runtime.NumCPU()
    if numWorkers > numFiles {
        numWorkers = numFiles
    }
    
    jobs := make(chan string, numFiles)
    results := make(chan compileResult, numFiles)
    
    var wg sync.WaitGroup
    for i := 0; i < numWorkers; i++ {
        wg.Add(1)
        go s.compileWorker(resolved, jobs, results, &wg)
    }
    
    for _, src := range resolved.SourceFiles {
        jobs <- src
    }
    close(jobs)
    
    wg.Wait()
    close(results)
    
    objs := make([]string, 0, numFiles)
    for r := range results {
        if r.err != nil {
            return r.err
        }
        objs = append(objs, r.objPath)
    }
    
    if err := s.link(resolved, objs); err != nil {
        return err
    }
    
    return pkgInfo.Cache.Save(s.buildDir)
}

func (s *Scheduler) resolveTarget(node *BuildNode) (*ResolvedTarget, error) {
    // 获取模式相关标志
    modeFlags, modeDefines := api.GetModeFlags(s.mode)
    
    resolved := &ResolvedTarget{
        Node:        node,
        AllDefines:  append([]string{}, node.Target.Defines()...),
        AllCFlags:   append([]string{}, s.toolchain.DefaultFlags.CFlags...),
        AllCxxFlags: append([]string{}, s.toolchain.DefaultFlags.CxxFlags...),
        AllLdFlags:  append([]string{}, s.toolchain.DefaultFlags.LdFlags...),
    }
    
    // 添加模式相关标志
    resolved.AllCFlags = append(resolved.AllCFlags, modeFlags...)
    resolved.AllCxxFlags = append(resolved.AllCxxFlags, modeFlags...)
    resolved.AllDefines = append(resolved.AllDefines, modeDefines...)
    
    // 添加 Includes 和 PublicIncludes
    for _, inc := range node.Target.Includes() {
        resolved.AllIncludes = append(resolved.AllIncludes, inc)
    }
    for _, pubInc := range node.Target.PublicIncludes() {
        resolved.AllIncludes = append(resolved.AllIncludes, pubInc)
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
        
        depPkg := s.pkgs[depNode.PkgName]
        
        // 继承公开头文件（转为绝对路径）
        for _, pubInc := range depNode.Target.PublicIncludes() {
            absInc := filepath.Join(depPkg.Dir, pubInc)
            resolved.AllIncludes = append(resolved.AllIncludes, absInc)
        }
        
        // 收集依赖产物
        depOutput := filepath.Join(depPkg.Dir, s.getTargetOutputPath(depNode))
        resolved.DepArtifacts = append(resolved.DepArtifacts, depOutput)
    }
    
    // 解析 glob
    for _, pattern := range node.Target.Files() {
        files, err := glob.Match(pattern, ".")
        if err != nil {
            return nil, err
        }
        resolved.SourceFiles = append(resolved.SourceFiles, files...)
    }
    
    // 设置输出路径
    resolved.OutputPath = s.getTargetOutputPath(node)
    
    // 去重
    resolved.AllDefines = unique(resolved.AllDefines)
    resolved.AllIncludes = unique(resolved.AllIncludes)
    resolved.AllCFlags = unique(resolved.AllCFlags)
    resolved.AllCxxFlags = unique(resolved.AllCxxFlags)
    resolved.AllLdFlags = unique(resolved.AllLdFlags)
    
    return resolved, nil
}

func (s *Scheduler) getTargetOutputPath(node *BuildNode) string {
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
    default:
        name = node.Target.Name()
    }
    
    return filepath.Join("build", s.buildDir, name)
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
    pkgInfo := s.pkgs[resolved.Node.PkgName]
    
    objRel := fmt.Sprintf("build/%s/objects/%s.o", s.buildDir, strings.ReplaceAll(src, "/", "_"))
    
    // 检查缓存
    if cached := pkgInfo.Cache.GetIfValid(src); cached != nil {
        return cached.ObjPath, cached.Deps, nil
    }
    
    vlog.Info("  CC %s", src)
    
    // 判断语言
    lang := "c"
    if glob.IsCppFile(src) {
        lang = "cxx"
    }
    
    opts := &CompileOptions{
        Includes: resolved.AllIncludes,
        Defines:  resolved.AllDefines,
        CFlags:   resolved.AllCFlags,
        CxxFlags: resolved.AllCxxFlags,
        Language: lang,
        Mode:     s.mode,
    }
    
    // 记录到 compile_commands.json
    s.ccWriter.AddCommand(src, objRel, opts)
    
    deps, err := s.compiler.Compile(src, objRel, opts)
    if err != nil {
        return "", nil, err
    }
    
    pkgInfo.Cache.Update(src, objRel, deps)
    
    return objRel, deps, nil
}

func (s *Scheduler) link(resolved *ResolvedTarget, objs []string) error {
    // 检查是否需要重新链接
    if !s.needRelink(resolved, objs) {
        return nil
    }
    
    outputName := filepath.Base(resolved.OutputPath)
    
    switch resolved.Node.Target.Kind() {
    case api.TargetBinary:
        vlog.Info("  LINK %s", outputName)
        allObjs := append([]string{}, objs...)
        for _, artifact := range resolved.DepArtifacts {
            allObjs = append(allObjs, artifact)
        }
        links := unique(resolved.Node.Target.Links())
        return s.linker.LinkBinary(allObjs, links, resolved.AllLdFlags, resolved.OutputPath)
    case api.TargetStatic:
        vlog.Info("  AR %s", outputName)
        allObjs := append([]string{}, objs...)
        for _, artifact := range resolved.DepArtifacts {
            allObjs = append(allObjs, artifact)
        }
        return s.linker.LinkStatic(allObjs, resolved.OutputPath)
    case api.TargetShared:
        vlog.Info("  LINK %s", outputName)
        return s.linker.LinkShared(objs, resolved.AllLdFlags, resolved.OutputPath)
    case api.TargetObject:
        vlog.Info("  OBJ %s", outputName)
        if len(objs) == 1 {
            if objs[0] == resolved.OutputPath {
                return nil
            }
            return os.Rename(objs[0], resolved.OutputPath)
        }
        return fmt.Errorf("object target requires exactly one source file")
    }
    return nil
}
```

---

## 5. CLI 集成（`cmd/vmake/`）

### 5.1 更新 runBuild

```go
func runBuild(cmd *cobra.Command, args []string) {
    ctx, err := PrepareFull()
    if err != nil {
        vlog.Error("Error: %v", err)
        os.Exit(1)
    }

    if err := executeBuild(ctx); err != nil {
        vlog.Error("Error: %v", err)
        os.Exit(1)
    }
}

func executeBuild(ctx *BuildContext) error {
    // 获取全局配置值
    globalValues := make(map[string]any)
    if ctx.Config.Global != nil {
        globalValues["toolchain"] = ctx.Config.Global.Toolchain
        globalValues["mode"] = ctx.Config.Global.Mode
        for k, v := range ctx.Config.Global.Options {
            globalValues[k] = v
        }
    }

    mode := ""
    if m, ok := globalValues["mode"].(string); ok {
        mode = m
    }
    if mode == "" {
        mode = api.ModeDebug
    }

    vlog.Info("")
    vlog.Info("Executing OnBuild...")
    allTargets := make(map[string]map[string]*api.Target)

    for _, lr := range ctx.LoadResults {
        if !lr.Success {
            continue
        }

        pkgName := lr.Package.Name
        pc := config.GetPackageConfig(ctx.Config, pkgName)
        buildCtx := api.NewBuildContext(pkgName, pc.Options)
        buildCtx.SetOptions(ctx.AllOptions[pkgName])
        buildCtx.SetGlobalOptions(ctx.GlobalOptions)
        buildCtx.SetGlobalValues(globalValues)

        for _, fn := range lr.Loaded.Builder.GetBuildFuncs() {
            fn(buildCtx)
        }
        allTargets[pkgName] = buildCtx.GetTargets()
    }

    // 选择工具链
    tc, tcName, err := GetToolchain(ctx.Config)
    if err != nil {
        return err
    }
    vlog.Info("")
    vlog.Info("Using toolchain: %s, mode: %s", tcName, mode)

    // 构建依赖图
    graph, err := build.NewBuildGraph(allTargets)
    if err != nil {
        return err
    }

    vlog.Info("")
    vlog.Info("Build order:")
    for _, fullName := range graph.Order {
        vlog.Info("  - %s", fullName)
    }

    pkgDirs := GetPackageDirs(ctx.Packages)

    // 创建调度器并构建
    scheduler, err := build.NewScheduler(graph, tc, pkgDirs, mode)
    if err != nil {
        return err
    }

    vlog.Info("")
    vlog.Info("Building...")
    if err := scheduler.BuildAll(); err != nil {
        return err
    }

    vlog.Info("")
    vlog.Info("Build succeeded!")
    return nil
}
```

### 5.2 添加 clean 命令

```go
func runClean(cmd *cobra.Command, args []string) {
    // 清理所有工具链/模式组合的构建目录
    entries, err := os.ReadDir("build")
    if err != nil {
        vlog.Info("Nothing to clean")
        return
    }

    for _, entry := range entries {
        if entry.IsDir() {
            objectsDir := filepath.Join("build", entry.Name(), "objects")
            if _, err := os.Stat(objectsDir); err == nil {
                if err := os.RemoveAll(objectsDir); err != nil {
                    vlog.Error("Failed to clean %s: %v", objectsDir, err)
                    continue
                }
                vlog.Info("Cleaned %s", objectsDir)
            }
        }
    }

    vlog.Info("Clean completed!")
}
```

### 5.3 添加 rebuild 命令

```go
func runRebuild(cmd *cobra.Command, args []string) {
    runClean(cmd, args)
    runBuild(cmd, args)
}
```

### 5.4 CLI 结构

使用 cobra 库实现 CLI，命令结构如下：

```
RootCmd (vmake)
├── buildCmd (build)     # 或直接运行 vmake
├── configCmd (config)
├── cleanCmd (clean)
├── rebuildCmd (rebuild)
├── toolchainCmd (toolchain)
│   ├── init
│   ├── list
│   └── show
└── versionCmd (version)
```

全局选项：
- `-v, --verbose`: 详细输出
- `-V, --very-verbose`: 非常详细输出
- `-q, --quiet`: 安静模式

---

## 6. 文件清单

| 文件 | 职责 |
|------|------|
| `internal/glob/glob.go` | Glob 模式匹配（`*.c`, `**/*.cpp`），IsCppFile/IsCFile |
| `pkg/build/graph.go` | 依赖图构建、拓扑排序、循环检测 |
| `pkg/build/compiler.go` | 编译器封装，`-MMD -MP` 生成 .d 文件 |
| `pkg/build/linker.go` | 链接器封装 |
| `pkg/build/cache.go` | 缓存管理，工具链感知，模式感知，头文件依赖 |
| `pkg/build/scheduler.go` | 构建调度，并行编译，整合所有组件 |
| `pkg/build/compile_commands.go` | compile_commands.json 生成 |
| `cmd/vmake/root.go` | CLI 入口，PrepareBase/PrepareFull |
| `cmd/vmake/build_cmd.go` | build 命令实现 |
| `cmd/vmake/clean.go` | clean 命令实现 |
| `cmd/vmake/rebuild.go` | rebuild 命令实现 |

---

## 7. 实现状态

| 步骤 | 内容 | 状态 |
|------|------|------|
| 1 | `internal/glob/glob.go` | ✅ 完成 |
| 2 | `pkg/build/graph.go` | ✅ 完成 |
| 3 | `pkg/build/cache.go` | ✅ 完成 |
| 4 | `pkg/build/compiler.go` | ✅ 完成 |
| 5 | `pkg/build/linker.go` | ✅ 完成 |
| 6 | `pkg/build/scheduler.go` | ✅ 完成 |
| 7 | `pkg/build/compile_commands.go` | ✅ 完成 |
| 8 | `cmd/vmake/` CLI 更新 | ✅ 完成 |
| 9 | 全局选项支持 | ✅ 完成 |
| 10 | 模式支持（debug/release） | ✅ 完成 |

---

## 8. 测试计划

### 8.1 单元测试

```go
// internal/glob/glob_test.go
func TestMatch_SingleStar(t *testing.T)
func TestMatch_DoubleStar(t *testing.T)
func TestIsCppFile(t *testing.T)

// pkg/build/graph_test.go
func TestBuildGraph_NoDeps(t *testing.T)
func TestBuildGraph_WithDeps(t *testing.T)
func TestBuildGraph_Circular(t *testing.T)

// pkg/build/cache_test.go
func TestCache_ToolchainChange(t *testing.T)
func TestCache_ModeChange(t *testing.T)
func TestCache_HeaderChange(t *testing.T)
```

### 8.2 集成测试

```bash
# 1. 简单 C 项目
cd test_data/01_simple_c
vmake build
./build/gcc-debug/hello

# 2. 多模块项目
cd test_data/04_multi_module
vmake build
./app/build/gcc-debug/app

# 3. 增量编译
touch lib/utils.c
vmake build  # 只重编译 utils.c

# 4. 工具链切换
vmake config  # 选择不同的工具链或模式
vmake build   # 全量重建

# 5. clean/rebuild
vmake clean
vmake rebuild

# 6. compile_commands.json
ls build/compile_commands.json
```

---

## 9. 数据流图

```
┌─────────────────────────────────────────────────────────────────┐
│                          runBuild()                              │
├─────────────────────────────────────────────────────────────────┤
│  1. PrepareFull()                                                │
│     ├── Scan build.go files                                      │
│     ├── Compile plugins                                          │
│     ├── Load plugins                                             │
│     ├── Execute OnConfig (collect options)                       │
│     └── Merge global options                                     │
│                                                                  │
│  2. executeBuild()                                               │
│     ├── Get global values (toolchain, mode, options)             │
│     ├── Execute OnBuild (collect targets)                        │
│     ├── Select toolchain                                         │
│     ├── BuildGraph(targets)                                      │
│     │   ├── Parse Deps (resolve "pkg:target" format)             │
│     │   ├── Build nodes                                          │
│     │   └── Topological sort → Order                             │
│     │                                                            │
│     ├── NewScheduler(graph, toolchain, pkgDirs, mode)            │
│     │   ├── Load cache per package (build/{tc}-{mode}/cache.json)│
│     │   ├── Check toolchain/mode change → full rebuild           │
│     │   └── Init Compiler & Linker                               │
│     │                                                            │
│     └── scheduler.BuildAll()                                     │
│         └── For each target in Order:                            │
│             ├── resolveTarget()                                  │
│             │   ├── Add mode flags (GetModeFlags)                │
│             │   ├── Merge includes (self + deps' PublicIncludes) │
│             │   ├── Resolve glob → source files                  │
│             │   └── Collect dep artifacts (.a/.so)               │
│             │                                                    │
│             ├── Parallel compile (workers = NumCPU)              │
│             │   ├── cache.GetIfValid()?                          │
│             │   │   ├── Hit → return cached .o                  │
│             │   │   └── Miss → Compiler.Compile() with -MMD -MP │
│             │   │             → parse .d file → deps             │
│             │   │             → cache.Update()                   │
│             │   └── ccWriter.AddCommand() for compile_commands  │
│             │                                                    │
│             └── Linker.Link*(objs, output)                       │
│                 └── Binary: gcc -o ...                           │
│                 └── Static: ar rcs ...                           │
│                 └── Shared: gcc -shared ...                      │
│                                                                  │
│  3. Save compile_commands.json                                   │
│  4. Save cache per package                                       │
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

## 11. 工具链和模式变化检测

```
┌──────────────────────────────────────────────────────────────┐
│                  NeedFullRebuild(toolchain, mode)            │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  缓存中的工具链信息：                                         │
│  {                                                           │
│    "version": 3,                                             │
│    "toolchain": {                                            │
│      "name": "gcc",                                          │
│      "cc_path": "/usr/bin/gcc",                              │
│      "cxx_path": "/usr/bin/g++",                             │
│      "host": "x86_64-linux-gnu"                              │
│    },                                                        │
│    "mode": "debug"                                           │
│  }                                                           │
│                                                              │
│  当前选择的工具链和模式：                                     │
│  {                                                           │
│    "toolchain": {                                            │
│      "name": "arm-gcc",          ← 变化！                    │
│      "cc_path": "/opt/arm/bin/arm-gcc",                      │
│      ...                                                     │
│    },                                                        │
│    "mode": "release"             ← 变化！                    │
│  }                                                           │
│                                                              │
│  检测条件（任一满足则触发全量重建）：                          │
│  1. toolchain.name 不同                                      │
│  2. toolchain.cc_path 不同（编译器升级/更换）                │
│  3. toolchain.cxx_path 不同                                  │
│  4. mode 不同（debug ↔ release）                             │
│                                                              │
│  全量重建操作：                                               │
│  1. 删除 build/{tc}-{mode}/objects/ 目录                     │
│  2. 重置缓存                                                 │
│  3. 重新编译所有源文件                                       │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```
