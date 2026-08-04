# VMake 架构修改详细计划

基于架构师最佳判断，本计划采用**风险与收益最平衡的方案组合**：覆盖 P0+P1 核心架构债务 + 快速清理类，Chdir 采用两阶段策略，Fatal 改用 panic+recover 方案，Resolver 双快照。

计划分为 7 个阶段，按依赖顺序排列，每个阶段可独立验证提交。

---

## 阶段 0：Vestigial 清理 + 文档同步（低风险热身，~0.5 天）

**目标**：消除代码与 AGENTS.md 的不一致，降低后续重构的认知负担。

### 改动
1. **`pkg/resolver/resolver.go`**：
   - 删除 `ResolveDeferred()` 方法（L136-138）
   - 删除 `PackageNode.Deferred` 字段（L28）及所有引用
   - 删除 `root.go:214` 的 `(deferred)` 日志分支
2. **`cmd/vmake/root.go`**：
   - 删除 `runConfigurePhase` 中 `ResolveDeferred()` 调用（L111）
   - 保留 `UpdateOrder()` 调用（L112）
3. **`AGENTS.md`**：
   - 更新 Runtime Execution Flow 图示，移除 "Phase 2a: ResolveDeferred"
   - 标注 FilterDeps 实际位置（build phase 内）

### 验证
```bash
go vet ./cmd/... ./pkg/... ./internal/...
go test ./cmd/vmake/... ./pkg/... ./internal/...
for d in test_data/0[1-9]_*/ test_data/1[0-9]_*/ test_data/2[0-4]_*/; do (cd "$d" && ../../vmake build) || break; done
cd test_linux/17_firmware && ../../vmake build
```

---

## 阶段 1：修复 internal/exec → pkg/log 反向依赖（P0-2，~0.5 天）

**目标**：恢复 `internal/` 作为底层基底的层级契约，杜绝唯一一处反向依赖。

### 设计
引入 `internal/exec` 内部的 `Logger` 接口，由 `pkg/log` 适配注入，而非让 `internal/exec` 反向 import `pkg/log`。

### 改动
1. **`internal/exec/exec.go`**：
   - 删除 `vlog "github.com/spock2300/vmake/pkg/log"` import（L13）
   - 新增：
     ```go
     type Logger interface {
         Debug(format string, args ...any)
         Error(format string, args ...any)
         Fatal(format string, args ...any)
     }
     var logger Logger = noopLogger{}
     func SetLogger(l Logger) { logger = l }
     type noopLogger struct{}
     func (noopLogger) Debug(string, ...any) {}
     func (noopLogger) Error(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }
     func (noopLogger) Fatal(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...); os.Exit(1) }
     ```
   - 把 `RunWithOptions` 中的 `vlog.Debug(...)` 改为 `logger.Debug(...)`
   - 把 `RunFatal` 中的 `vlog.Fatal(...)` 改为 `logger.Fatal(...)`
2. **`pkg/log/log.go`**：确认 `Level`/方法签名已满足 `exec.Logger` 接口（`Debug`/`Error`/`Fatal` 签名一致，无需改动）
3. **`cmd/vmake/main.go`**：在 `Execute()` 最前面，`loadPlugins()` 之前，添加：
   ```go
   exec.SetLogger(vlog.DefaultLogger{})  // 或直接传 vlog
   ```
   （具体注入点：因 `pkg/log` 是包级函数，需在 `pkg/log` 暴露一个适配类型，或让 `exec.SetLogger` 接收函数闭包。推荐在 `pkg/log/log.go` 加一个 `type Adapter struct{}` 实现 `exec.Logger` 接口转发到包级函数。但为避免 `pkg/log` 反向依赖 `internal/exec`，改为在 `cmd/vmake` 写一个 3 行的 adapter。）

### 验证
```bash
grep -rn "vlog" internal/  # 应为空
go vet ./internal/...      # internal/exec 不再 import 任何 pkg/
go test ./cmd/vmake/... ./pkg/...
```

---

## 阶段 2：vlog.Fatal → panic + recover（P1，~1 天）

**目标**：让 `pkg/api` 的 9 处 Fatal 改为可被外层捕获、带上下文的错误，而非直接 `os.Exit(1)`。**保持 build.go API 完全不变**（零迁移成本）。

### 设计
`pkg/api` 内部把 `vlog.Fatal` 改为 `panic(&BuildScriptError{...})`；在 yaegi 脚本调用边界（`execFuncs` 外层、`LoadBuildScript` 外层）加 `recover()`，转为 `error` 向上传播。`recover` 捕获的是 `*BuildScriptError` 类型，其他 panic 正常传播（不掩盖真实 bug）。

### 改动
1. **`pkg/api/errors.go`**（新文件）：
   ```go
   package api
   import "fmt"
   type BuildScriptError struct {
       Package string
       Op      string
       Err     error
   }
   func (e *BuildScriptError) Error() string { ... }
   func (e *BuildScriptError) Unwrap() error { return e.Err }
   func fatalErr(pkg, op string, format string, args ...any) {
       panic(&BuildScriptError{Package: pkg, Op: op, Err: fmt.Errorf(format, args...)})
   }
   ```
2. **`pkg/api/target.go`**：6 处 `vlog.Fatal(...)` → `fatalErr(t.pkgName, "SetXxx", ...)`
   - L187 `SetPrebuilt`、L199 `SetLinkerScript`、L207 `SetVersionScript`、L223 `SetSymbolBinding`、L230 `SetSymbolPrefix`
3. **`pkg/api/package.go`**：L165 `SetProvidedLinkerScript` → `fatalErr(p.Name(), ...)`
4. **`pkg/api/context.go`**：L107 `KConfig`、L207/L210 `BuildSubGraph`、L219 `DepOutput` → `fatalErr(...)`
5. **`pkg/api/package.go` `execFuncs`**（L326）：包装 `recover`：
   ```go
   func execFuncs[T any](dir string, pkgName string, funcs []T, fn func(T)) error {
       execInDir(dir, func() {
           for _, f := range funcs {
               func() {
                   defer func() {
                       if r := recover(); r != nil {
                           if bse, ok := r.(*BuildScriptError); ok {
                               collected = append(collected, bse)
                           } else {
                               panic(r)  // 非 BuildScriptError，重新抛出
                           }
                       }
                   }()
                   fn(f)
               }()
           }
       })
       return errors.Join(collected...)
   }
   ```
   - `ExecConfigFuncs`/`ExecBuildFuncs`/`ExecInstallFuncs`/`ExecCleanFuncs` 签名加 `error` 返回值
6. **所有 `Exec*Funcs` 调用点**（`build_phase.go:450`、`build_install.go:89`、`query_cmd.go:193`、`check_symbols_cmd.go:145`、`clean.go:215`、`root.go:237`）：检查返回的 error，用 `fatalErr` 上报
7. **`pkg/buildscript/yaegi_loader.go`**：`mainFunc(pkg)`（L63）和 `fn(pkg)`（L66）外层加 `recover()`，转 `*BuildScriptError` 为 `error` 返回

### 关键约束
- **不修改** `BuildFunc`/`ConfigFunc` 等回调类型签名（保持 `func(ctx *BuildContext)`）
- **不修改** `yaegi_symbols.go`（API 表面不变）
- 非 `*BuildScriptError` 的 panic **必须重新抛出**，避免掩盖 nil 解引用等真实 bug

### 验证
```bash
# 故意触发双设错误，验证报错带包名而非裸 os.Exit
echo 'func Main(p *api.Package) { p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Target("t").SetLinkerScript("a.ld").SetLinkerScript("b.ld")
})}' > /tmp/test_build.go
# 预期：stderr 输出 "package X: SetLinkerScript: already set"，exit 1，带上下文
go test ./pkg/api/... ./pkg/buildscript/...
```

---

## 阶段 3：Resolver 双快照化（P1，~2 天）

**目标**：让 Resolver 在 ResolveAll 后产出「扫描快照」、FilterDeps 后产出「配置快照」，构建阶段只读不可变快照，消除跨阶段可变状态。

### 设计
新增 `resolver.Snapshot` 不可变值类型。FilterDeps 不再原地改写 `node.Deps`，而是返回新快照。`RuntimeContext.DepGraph` 类型从 `*resolver.Graph` 改为 `*resolver.Snapshot`。

### 改动
1. **`pkg/resolver/snapshot.go`**（新文件）：
   ```go
   type Snapshot struct {
       Packages  map[string]PackageSnapshot   // 值类型，非指针
       Order     []string                      // 不可变（不暴露 setter）
       SubParents map[string]string
   }
   type PackageSnapshot struct {
       ID, SourceDir string
       IsLocal, IsNative bool
       Deps        []string    // FilterDeps 后的最终值
       Constraints []string
       Pkg         *api.Package  // 仍是指针（OnConfig 会 mutate，见下方说明）
       Native      *NativePackageInfo
   }
   func (s *Snapshot) GetOrder() []string { return s.Order }
   func (s *Snapshot) Package(id string) (PackageSnapshot, bool)
   ```
2. **`pkg/resolver/resolver.go`**：
   - `ResolveAll` 末尾调用 `r.snapshot()` 产出第一个快照（unfiltered Deps）
   - `FilterDeps` 签名从 `FilterDeps(id, cfg, opts) error`（原地改）改为 `FilterDeps(snap *Snapshot, id, cfg, opts) (*Snapshot, error)`（返回新快照）
   - `UpdateOrder` 返回 `[]string` 而非改写字段
3. **`cmd/vmake/build_phase.go` `filterAndCollectNeeded`**（L136-159）：
   - 遍历 Order，对每个包调用 `FilterDeps` 累积成新快照
   - 全部过滤完后产出最终 `*Snapshot`，赋值给 `ctx.DepGraph`
4. **`cmd/vmake/root.go`**：
   - `RuntimeContext.DepGraph` 类型 `*resolver.Graph` → `*resolver.Snapshot`
   - `runRequirePhase` 末尾存储扫描快照
5. **所有读站点批量替换**（约 50+ 处，主要集中在）：
   - `ctx.DepGraph.Packages[name]` → `ctx.DepGraph.Package(name)`（返回值类型）
   - `ctx.Resolver.GetOrder()` → `ctx.DepGraph.Order`
   - `ctx.Resolver.SubParents()` → `ctx.DepGraph.SubParents`
   - `node.Pkg`/`node.Source`/`node.IsLocal()` 访问改为通过 `PackageSnapshot` 字段
   - 涉及文件：`build_phase.go`、`build_exec.go`、`build_config.go`、`build_install.go`、`config.go`、`clean.go`、`query_cmd.go`、`check_symbols_cmd.go`、`root.go`

### 关于 `node.Pkg` 的 `*api.Package` 仍可变
- 快照冻结的是**图结构**（Deps/Order），`*api.Package` 实例本身在 OnConfig/OnBuild 阶段仍被 mutate（SetSrcDir/SetToolchain 等）。这是合理的——包的**配置状态**本就是阶段推进的产物。
- 真正消除的是「Deps 在 FilterDeps 阶段被悄悄覆写」「Order 被反复重算」这两类**图结构可变性**。

### 验证
```bash
go test ./pkg/resolver/... ./cmd/vmake/...
# 关键测试：filter_deps_test.go 必须通过
# 集成测试全套
```

---

## 阶段 4：os.Chdir 消除 — 第一阶段（vmake 内部，P0-1，~2-3 天）

**目标**：vmake 自身 Go 代码（scheduler/compiler/glob/postLink）全部改为显式传 `Dir`，删除 `Build` 内的 `os.Chdir`。**保留** `execInDir` 给 build.go 脚本回调（第二阶段处理）。

### 设计
- `Scheduler.Build` 删除 L179-182 的 chdir
- 所有受影响的内部调用改为显式传 `SourceDir`
- `OutputPath`/`GeneratedDir` 等相对路径改为绝对路径（join SourceDir）

### 改动
1. **`pkg/build/scheduler.go`**：
   - 删除 `origDir` 字段（L75）和 `NewScheduler` 中的 `os.Getwd()`（L95）
   - 删除 `Build` 中 L179-182 的 chdir/defer
   - **`resolveTarget`**（L378+）：把 `glob.Match(pattern, ".")`（L417）改为 `glob.Match(pattern, pkgInfo.SourceDir)`
   - **`OutputPath`/`GeneratedDir`**（L58,62）：当基础路径是 `"."` 时，join `pkgInfo.SourceDir`
   - **`compileSource`**（L473+）：`s.compiler.Compile` 调用改为传入 `SourceDir`；`compiler.go:54` 的 `iexec.Run(compiler, args...)` 改为 `iexec.RunInDir(compiler, pkgInfo.SourceDir, args...)`
   - **`postLink`**（L788）：`iexec.Run(tool, args...)` 改为 `iexec.RunInDir(tool, pkgInfo.SourceDir, args...)`
   - **`realizePrebuilt`**（L549,553）：`filepath.Abs(src)` 改为 `filepath.Join(pkgInfo.SourceDir, src)`（前提是 src 是相对路径）
   - **`ccWriter.SetPackageDir`**：已传绝对路径，无需改
2. **`pkg/build/compiler.go`**：
   - `Compile` 方法接收 `workDir string` 参数（或在 opts 里）
   - L54 `iexec.Run` 改用 `RunInDir(workDir, ...)`
   - `IsSourceValid` 里的 `os.Stat` 改为 `os.Stat(filepath.Join(workDir, path))`
3. **`pkg/build/linker.go`**：`LinkBinary`/`LinkShared` 接收 `workDir`，exec 调用改用 `RunInDir`
4. **`pkg/build/subgraph.go`**：删除 L108-109 的 origDir 保存/恢复（因 scheduler 不再 chdir，这层多余）
5. **`pkg/api/context.go` `BuildContext.Exec`**（L259-265）：
   - `exec.RunFatal("", name, args...)` → `exec.RunFatal(ctx.pkg.SrcDir(), name, args...)`（显式传 SourceDir）
   - 这是从 BuildFunc 内部调用的，目前依赖 `execInDir` 已 chdir。改后即便 chdir 移除也安全。
6. **`compile_commands.json` 保存**（L164）：`filepath.Join("build", ...)` 改为 `filepath.Join(projectRoot, "build", ...)`

### 关键约束
- **不触碰** `pkg/api/exec.go` 的 `execInDir`（仍服务于脚本回调，第二阶段处理）
- **不触碰** `pkg/buildscript/yaegi_loader.go` 和 `pkg/plugin/loader.go` 的 chdir（脚本侧，第二阶段）
- 所有相对路径必须改为绝对路径，否则移除 chdir 后会指向错误位置

### 验证
```bash
# 关键：增量构建必须正确（验证 mtime/路径解析）
cd test_data/01_simple_c && ../../vmake build
touch src/main.c && ../../vmake build  # 必须只重编 main.c
# 集成测试全套 + 17_firmware
grep -rn "os.Chdir" pkg/build/  # 应只剩 subgraph 的（已删）或为零
```

---

## 阶段 5：os.Chdir 消除 — 第二阶段（脚本侧，~1-2 天，可选）

**目标**：消除 `execInDir`、yaegi_loader、plugin_loader 的 chdir，为并发铺路。

### 设计（更激进，需评估）
build.go 脚本里的相对路径语义（`os.Stat("src/foo.c")`、`p.Run("make")` 依赖 cwd）如何替代？两个选项：
- **A**：保持脚本侧 cwd 语义，但通过 yaegi 解释器的 `interp.Options{}` 注入（若 yaegi 不支持，则无法实现）
- **B**：在文档中明确「build.go 回调内 cwd == SourceDir」是一个**受保证的不变量**，由 `execInDir` 提供。承认这是脚本契约的一部分，不消除。

### 建议
**采用选项 B**，即**第二阶段不执行**。理由：
- yaegi 不原生支持「解释器级 cwd」
- 选项 A 需要拦截所有 `os.Stat`/`os.Open` 调用并重写路径，侵入性极大
- 脚本侧的 chdir 只在**回调执行期间**短暂存在，配合 `defer os.Chdir(origDir)`，单线程下完全安全
- 真正阻碍并发的 vmake 内部 chdir（阶段 4）已消除，跨 target 并发已可实现（脚本回调内部仍是串行的，这是合理约束）

**结论**：阶段 4 已足够解锁并发，阶段 5 暂不执行，在文档中记录「脚本回调 cwd 契约」。

---

## 阶段 6：Legacy Fallback 清理（P2，~1 天）

**目标**：消除与「No Fallbacks」原则自相矛盾的代码。

### 改动
1. **`cmd/vmake/build_config.go` `computeReachable`**（L18-93）：
   - 删除 `VMAKE_LEGACY_ROOT` 启发式分支（L37-77）
   - 强制要求显式 `SetRoot(true)`；若无任何包声明 root，`vlog.Fatal` 并提示「no package declared SetRoot(true)」
2. **`cmd/vmake/build_phase.go` `downloadRemoteSources`**（L246-252）：
   - 删除 `ParseBuildGo` 正则 fallback 分支
   - 当 `node.Pkg == nil && !node.Native` 时，直接返回错误「package %s has no resolved metadata; ensure build.go loads correctly」
3. **`pkg/build/compiler.go` `IsSourceValid`**（L104-138）：
   - `ParseDepFile` 错误：返回 `(false, nil, err)`，由调用方决定是否重建
   - `os.Stat(src)` 错误：区分 `os.IsNotExist`（视为需重建）与其他错误（向上传播）
4. **`pkg/build/install.go` `installTarget`**（L103）：
   - output 缺失时改为返回 `fmt.Errorf("install: target %s output missing: %s", name, path)` 而非静默跳过
   - 但 `pkgInfo` 缺失仍可跳过（这是合法情况）
5. **`copyPublicIncludes`**（L152）：stat 错误区分 NotExist（continue）与其他（return err）

### 验证
```bash
go test ./pkg/build/...
# 测试 VMAKE_LEGACY_ROOT 已移除（环境变量应无效）
# 集成测试：确认所有 test_data 仍通过（它们都应已有 SetRoot 或满足新规则）
```

---

## 阶段 7：api.Package 拆分（P1，~1-2 天，可延后）

**目标**：缓解 God Object。`Package` 当前 698 行/37 字段，混合了数据模型与命令执行。

### 设计
**不拆包**（避免 import path 变更影响所有 build.go），只在 `pkg/api` 包内**分文件**并引入轻量组合类型。

### 改动
1. **新建 `pkg/api/package_runner.go`**：
   - 把 `Run`/`RunIn`/`RunEnv`/`Make`/`CMakeConfigure`/`CMakeBuild`/`CMakeInstall`/`Configure`/`CMakeGlobalFlagsArgs`/`MergedCFlags`/`MergedCxxFlags`/`MergedLdFlags`（package.go L542-617）移过去
   - 这些方法仍接收 `*Package` receiver（不改 API）
   - 文件内可定义一个 `packageRunner` 私有 struct 持有共享的 exec 配置，但方法挂在 `*Package` 上
2. **新建 `pkg/api/package_kconfig.go`**：
   - 把 `EnsureConfig`/`ApplyKConfigPatches`/KConfig 相关方法移过去
3. **`package.go` 瘦身**：只保留 struct 定义、字段、OnRequire/OnConfig/OnBuild 回调注册、`Exec*Funcs`、基本 getter/setter
4. **`yaegi_symbols.go`**：无需改动（符号是按包导出的，文件拆分不影响）

### 收益
- `package.go` 从 698 行降到 ~350 行
- 命令执行逻辑与数据模型分离，便于未来进一步抽出 `PackageRunner` 接口

### 验证
```bash
go test ./pkg/api/...
go vet ./pkg/api/...
```

---

## flattenAny 校验（P2，~0.5 天，可并入任一阶段）

### 改动
**`pkg/api/target.go` `flattenAny`**（L382-399）：
- 遇到非 `string`/`[]string`/`nil` 类型时，记录一次 `vlog.Error("package %s: AddXxx got unsupported type %T for value %v", ...)` 并跳过
- 不改为 panic（避免破坏性），但至少让用户在 `-v` 模式下看到警告

### 验证
```bash
go test ./pkg/api/...
```

---

## 总体执行顺序与依赖

```
阶段 0 (vestigial 清理) ──┐
                          ├─→ 阶段 1 (internal/exec) ──┐
                          │                            ├─→ 阶段 2 (Fatal→panic) ──┐
                          │                            │                          ├─→ 阶段 3 (Resolver 快照)
                          │                            │                          │
                          └────────────────────────────┴──────────────────────────┴─→ 阶段 4 (os.Chdir 内部)
                                                                                       │
                                                                                       └─→ 阶段 6 (legacy fallback)
                                                                                       │
                                                                                       └─→ 阶段 7 (Package 拆分)
                                                                                       │
                                                                                       └─→ flattenAny
```

- 阶段 0-2 可在 1.5 天内完成（低风险，快速见效）
- 阶段 3 是最大单体改动（~2 天，50+ 读站点替换）
- 阶段 4 是技术含量最高（~2-3 天，路径解析需逐个验证）
- 阶段 5 暂不执行（文档记录）
- 阶段 6-7 可并行或延后

## 总工作量估算
- **核心（阶段 0-4）**：约 6-8 天
- **完整（含阶段 6-7）**：约 9-11 天
- **每个阶段独立提交、独立验证**，可随时暂停

## 不在本次计划范围
- 跨 target 并发执行（需阶段 4 完成后再单独设计 worker pool）
- `context.Context` 取消传播（独立改进）
- yaegi 符号表自动生成（独立改进）
- 阶段 5 的脚本侧 chdir 消除（文档记录为受保证契约）

## 每阶段交付前的统一验证清单
```bash
gofmt -w .
go vet ./cmd/... ./pkg/... ./internal/...
go test ./cmd/vmake/... ./pkg/... ./internal/...
# 集成测试
for d in test_data/0[1-9]_*/ test_data/1[0-9]_*/ test_data/2[0-4]_*/; do (cd "$d" && ../../vmake build) || break; done
cd test_linux/17_firmware && ../../vmake build
```
