# 01 — 命令树可重建 + `run()` + 可恢复的致命路径

**Status:** ready-for-agent
**Type:** task
**Card:** 1a（生产侧；测试侧见 02）
**Blocked by:** 无

## 为什么

`cmd/vmake` 的唯一缝合面是进程：`main()` + 38 个 pflag 全局 + `os.Exit`。后果有两个：

1. 任何命令行为只能靠重新 spawn 测试二进制验证（211 处）。
2. `Execute()` 的两个清理（`releaseExtensionRepoLocks`、`activeCommandSession.close`）**不是 defer**，所以任何一处 `os.Exit` 都跳过它们；`check_symbols_cmd.go:89` 的 `defer session.Close()` 有三条路径永远不执行。`flock.acquire` 每 25ms 重试且无超时，所以一旦在同一进程内泄漏锁，下一次调用会永久阻塞。

## 做什么

1. `newRootCmd() *cobra.Command`：把 20 个 `func init()` 的内容搬进构造器；49 个 `var xCmd = &cobra.Command{...}` 单例改成 `newXxxCmd()`（已有 22 个是这种形态）。`loadPlugins` 的 `RootCmd.AddCommand`（`ext_cmd.go:333`）也必须发生在每次构造内，否则 `AddCommand` 无去重会累积插件命令。
2. `run(argv []string, stdout, stderr io.Writer, env runEnv) int`：`runEnv.VmakeDir` 为空时取 `~/.vmake`。入口复位 `cachedProjectDir`、`activeCommandSession`、`extensionRepoLocks`。
3. `Execute()` 缩成适配器：`os.Args`/`os.Stdout`/`os.Stderr` → `run()` → 清理 → 唯一一处 `os.Exit(code)`。
4. 致命路径改 panic：`exitWithCode` panic `*exitError{code}`；`fatalErr`/`fatalMsg`/`fatalUsage`/`fatalResolveRoot` 签名不变（108 个调用点一行不改）。`run()` 顶层 recover 后返回码。`config.go:93,101,109` 与 `doctor_cmd.go:167` 的直接 `os.Exit(1)` 改用 `exitWithCode(1)`；`doctor_cmd.go` 的 4 处 `vlog.Fatal` 改用 `fatalMsg`。

   **必须同时审计"展开时会写成功状态"的 defer。** `withReport`（`build_report.go:16-45`）的 defer 用命名返回值 `err` 调 `report.Finish(&err)` 与 `PrintSummary`；panic 展开时 `err` 仍是 `nil`，会把构建报告写成 `succeeded`。它必须在展开时 recover `*exitError`、把报告记为失败、再重新 panic。45 处生产 defer 逐条过一遍，凡是在 defer 里写状态/写文件的都要这样处理；只做恢复的（restore、Close、Release）不用改。
5. `internal/scriptcall.Recover` 对 `*exitError` 重新 panic（否则穿过脚本回调边界时会被吞成普通错误，退出码会变）。

   **落地时的具体形状**：`internal/scriptcall` 不能导入 `cmd/vmake`，所以 sentinel 用一个标记接口识别，`Recover` 在 `recover()` 后先判断再转错误：

   ```go
   // cmd/vmake
   type exitError struct{ code int }
   func (e *exitError) Error() string   { return fmt.Sprintf("exit status %d", e.code) }
   func (e *exitError) ExitCode() int   { return e.code }

   // internal/scriptcall
   type exitSignal interface{ ExitCode() int }
   func Recover(err *error) {
       value := recover()
       if value == nil { return }
       if _, ok := value.(exitSignal); ok { panic(value) }   // 让 run() 边界拿到退出码
       ...
   }
   ```

   必须重新 panic 而不是记录，否则 `fatalUsage`（退出码 2）会被改写成普通错误再变成 1。已清点：17 处 `defer scriptcall.Recover(&err)`，分布在 `pkg/build`(1)、`pkg/buildscript`(1)、`pkg/pipeline`(15)。
6. 注册幂等：`toolchain.Manager.AddGlobal*Flags` 去重；重复注册同一 toolchain 不报错；插件命令去重。理由：第二次调用必须不产生重复状态，且这是 `ext update` 后重载插件的潜伏 bug。
7. `pkg/log` 与 `internal/exec` 的级别在 `run()` 入口显式设定（`root.go:72-80` 的 switch 没有 `default`，`-q` 会遗留到下一次）。

## 验收

- `spec.md` 的 6 条通用验收。
- 新增测试：同一进程内连续调用两次 `run()`，不同 argv，断言两个退出码与两段输出互不污染；其中一次运行 `-q`，另一次不传，断言第二次日志正常。
- 新增测试：`run()` 返回后 `cachedProjectDir`、`activeCommandSession` 为空。
- 新增测试：`fatalErr` 在带 `defer` 的函数里被调用时，`defer` 会执行（钉住第 4 条）。
- 快照零漂移是"生产行为未变"的硬证据。

## 涉及文件

`cmd/vmake/root.go`、`main.go`、`helpers.go`、`session.go`、`paths.go`、`logging.go`、`completion.go`、`version.go`、`ext_cmd.go`、`config.go`、`doctor_cmd.go`，以及承载 49 个单例与 20 个 `init()` 的全部命令文件。
`internal/scriptcall/`、`pkg/toolchain/manager.go`、`pkg/plugin/`。

## 被替代的结构

- `RootCmd` 包级单例 + 20 个 `init()`。
- `Execute()` 里的顺序清理语句（改为 `run()` 的 defer/顺序清理 + 单一 `os.Exit`）。
- 12 个 `os.Exit` 位置中的 11 个。

## 已完成的开工审计（2026-11-25）

`cmd/vmake` 生产代码共 **45 处 `defer`**，按"panic 展开时是否会写错状态"分类：

| 类别 | 数量 | 结论 |
|---|---|---|
| `defer closeProject(ctx)` | 14 | 安全（只释放 resolver） |
| `defer <session>.Close()` | 8 | 安全 |
| `defer restoreStdout/Stderr/restoreScope` | 6 | 安全（幂等恢复） |
| `defer lock.Release()` | 9 | 安全 |
| `defer cancel()` / `close(done)` / `file.Close()` / `f.Close()` | 9 | 安全 |
| **`build_report.go:35` 的 `withReport` defer** | **1** | **不安全**，见下 |

结论：45 处里只有 `withReport` 需要在展开时改状态；其余 44 处天然幂等。这把第 4 条的审计面从"45 处逐条过"缩到"1 处必须改 + 44 处确认"。

`withReport` 的修法：defer 里先 `recover()`，若是退出信号则把 `report.Finish(&failedErr)` 记为失败、`PrintSummary` 打印失败摘要、再 `panic(value)` 继续展开。不做这一步，`vmake build` 期间任何 `fatalErr` 都会让 `build/vmake-build.json` 写成 `succeeded`。

17 处 `defer scriptcall.Recover(&err)` 的重新 panic 用第 5 条的标记接口，不需要改签名。

## 实现记录（1a）

偏离原计划一处：**没有把 49 个 `*cobra.Command` 单例改成构造器**。改为"每次调用重置整棵树再重建"——`resetCommandTree` 递归 `ResetFlags` + `ResetCommands`，`installCommands()` 重新挂命令、重新注册 flag。理由：

- pflag 在注册时把默认值写回指针，所以重建即重置 38 个全局；
- 13 个 flag 变量是构造器里的**函数局部**变量（`jsonOut` 出现 6 次、`locked`/`offline` 各 4 次、`setFlags` 与包级 `setFlags` 重名），提成包级会撞名，而重建会自然重建它们的闭包；
- 单例保持不变，`main.go` 的 `current == extCmd`、`shells` 里捕获 `RootCmd` 的闭包都不用动，diff 小一个数量级。

实现落点：

- `cmd/vmake/tree.go`（新增）：`runEnv`、`run()`、`prepareRoot()`、`resetCommandTree()`、`installCommands()` 及各 `installXxxCommand()`、`executeRoot()`、`exitCodeFor()`、`resetProcessState()`、`pipeStdout/Stderr`。
- 18 个命令文件的 `func init()` 移除，内容并入 `installCommands()`；`root.go` 的 `init()` 保留 `defaultVmakeDir` 并在末尾调用 `installCommands()`，以便走 `RootCmd` 的既有测试（`generateCLIRef`、`validateSkillCLICommand`）仍看到完整树。
- 插件加载从 `main()` 移入 `prepareRoot`，由 `runEnv.LoadPlugins` 控制：`Execute()` 置真，测试默认关闭，否则进程内测试会去读真实的 `~/.vmake`。`Execute()` 同时把当前生效的 `vmakeDir` 传进 `runEnv`，保证"调用前覆盖 `vmakeDir`"这一既有注入点不被复位掉（`TestTestCommandNoTargetsSucceeds` 抓到了这一点）。
- `resetCommandTree` 必须递归到 `ResetCommands`：只重置 `RootCmd` 的子命令时，`configCmd` 一类的旧子命令会留在原地（flag 已被清空），再次 `AddCommand` 只是追加，`Find` 命中的是没有 flag 的旧对象——`TestConfigCheckAndMultiRootWorkspace` 的 `config check --all` 就是这样失败的。

测试（`cmd/vmake/tree_test.go`，8 个，全部在 `run()` 这个缝合面上）：同进程连续两次调用、flag 默认值复位、过程态复位、`runEnv.VmakeDir` 生效与回落、退出码 0/1/2、fatal 会执行 defer、usage 错误映射为 2、重建后树与 CLI 参考完整。

## 审查后修正（第二轮）

两份并行审查（standards + spec）各自抓到的东西，已修：

1. **`exitSignal` 标记接口过宽**（真回归）：`internal/exec` 的 `*exec.ExitError` 也实现 `ExitCode()`，原设计会让 `scriptcall.Recover` 把普通子进程错误当成退出信号重新 panic。改为 `VmakeExitCode() int`——标准库没有任何错误实现它。`internal/scriptcall` 增加对应测试。
2. **`wrapCommandArgs` 每次调用叠加包装**：命令是复用的单例，`Args` 每次被再包一层。加 `wrappedArgs` 集合按指针去重，并加 `TestUsageValidatorNotStackedAcrossInvocations` 钉住"只包一层"。
3. **致命路径转换漏项**：`config.go` 3 处 `os.Exit(1)`、`doctor_cmd.go` 1 处 `os.Exit(1)` 与 4 处 `vlog.Fatal` 全部改走 `fatalErr`/`fatalMsg`/`exitWithCode`。现在 `cmd/vmake` 生产代码里只剩 `Execute()` 一处 `os.Exit`。
4. **注册幂等改成"只跑一次"而非"去重"**：插件加载由 `pluginsLoaded` 保证每进程一次，插件命令记录进 `pluginCommands`，每次调用重建树后再挂回去。这样 toolchain 与 global flag 的注册天然只发生一次，不需要给 `AddGlobal*Flags` 加去重（去重会改变脚本可见的 flag 语义）。
5. **`exitError` 带上 cause**：构建报告在 fatal 时记录真实消息而不是 `exit status 1`；`withReport` 的 panic 分支收敛成一处 `panic(r)`。
6. **测试并发写同一 buffer**：原来一条 `bytes.Buffer` 同时接 stdout 和 stderr，两个 `io.Copy` goroutine 会竞争；改为两条独立 buffer。`-race` 在本仓库不可用（需要 CGO，AGENTS.md 要求 `CGO_ENABLED=0`），所以靠结构消除。
7. **新增两个守卫**：`TestCommandReferenceStableAcrossRebuild`（重建前后 `generateCLIRef` 逐字节相同，防"重建时漏掉命令"）与 `TestUsageValidatorNotStackedAcrossInvocations`。
8. DD-40 的措辞已与实现对齐（`resetCommandTree` + `installCommands`，不用 `newRootCmd`；marker 接口改名；从"涉及代码"里去掉尚未创建的 `internal/quiet/`）。
