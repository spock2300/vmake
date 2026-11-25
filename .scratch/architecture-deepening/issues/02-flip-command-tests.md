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
