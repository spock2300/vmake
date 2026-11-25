# 02 — 命令测试翻面到进程内

**Status:** ready-for-agent
**Type:** task
**Card:** 1b（只动测试）
**Blocked by:** 01

## 为什么

211 处 CLI 调用分布在 21 个文件，靠 5 个助手（`runInspectCommand` 90、`runBuildCommand` 53、`extensionCommand` 46、`extensionCommandEnv` 8、`runShadowedRootCommand` 6）+ 8 处直接 `os.Executable()`。另有 52 处直接写包级全局，靠 `resetResolveTestGlobals`（`root_test.go:57-84`）保存/恢复。

## 做什么

1. 5 个助手内部从"re-exec 测试二进制"改成调 `run()`；211 个调用点一行不动。
2. 删除 `resetResolveTestGlobals` 一类保存/恢复助手：改用 `runEnv` 与 `run()` 的入口复位。
3. 保留**一个**子进程冒烟测试：真实二进制、真实 `os.Exit`、真实 fd，只断言进程边界本身（一个成功命令 + 一个 usage 错误退出码 2）。
4. 新增 `runSubprocess(t, dir, env, args...)` 作为唯一 spawn 入口，供插件/信任/扩展类测试使用（约 60 处：`ext_provision_test.go` 10、`ext_recovery_test.go` 10、`trust_test.go`、`ext_lfs_test.go`、`ext_flags_test.go`、`nonbuild_toolchain_test.go`）。理由：这些测试需要干净的 `pkg/toolchain` 管理器，而管理器是进程级、按 spec 明确不可重置。
5. `TestMain`（`ext_recovery_test.go:17-21`）里拦截 `--version` 子进程的入口一并调整。

## 验收

- `go test ./cmd/vmake` 全绿，并记录新的 wall time（基线 59.6s）。
- 子进程调用数从 211 降到约 60，且全部经由 `runSubprocess`。
- 不允许删除测试来达成降数；每个被翻面的测试保留原有断言。

## 实现记录

- 新增 `cmd/vmake/cli_test.go`：`runCLI`（进程内，`t.Setenv` + 进出目录 + 双缓冲）与 `runSubprocess`（唯一 spawn 入口，走 `TestExtensionBootstrapChild`）。
- 翻面：`runInspectCommandWithState`、`runBuildCommandEnv`、`runShadowedRootCommand` 现在直接调 `run()`。删除 `TestInspectBootstrapChild`、`TestBuildReportBootstrapChild`、`TestShadowedRootBootstrapChild` 三个重复子进程入口。
- 保留子进程：`extensionCommandEnv` 改走 `runSubprocess`（插件/信任/扩展/平台类共 ~54 处），因为它们改的是进程级 toolchain 状态；`TestOwnerInterruptChild` 与 `TestExtensionBootstrapChild` 各留一个。
- 新增 `TestCommandProcessBoundary`：唯一钉住进程边界的测试（真实 `os.Exit`、真实 fd：`version` 退出 0 且写真实 stdout，`build --no-such-flag` 退出 2 且写真实 stderr）。真实二进制本身由 `_snapshot` 覆盖。
- 剩余 spawn：20 处 `exec.Command`（含 `exec.LookPath` 之类非 spawn 调用与中断测试）。

实测：`go test ./cmd/vmake` **57.5s**（翻面前 59.9s）。提速有限，因为构建类测试本身要真的跑编译器；这张卡的价值是缝合面而不是墙钟时间——90 处 inspect 断言现在走 `run()`，且不再为每条命令付一次测试二进制启动。
