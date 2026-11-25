# 03 — `internal/quiet`：一个模块替代五个机制

**Status:** ready-for-agent
**Type:** task
**Card:** 3
**Blocked by:** 无

## 为什么

"让进程安静"有五个独立机制、十二处手工堆叠、以及每个调用点各自决定的恢复顺序：

| 机制 | 位置 |
|---|---|
| 交换 `os.Stdout` / `os.Stderr` | `inspect_cmd.go:265-281` |
| vlog 级别 + exec quiet + scriptfs 只读 | `pipeline/config_preview.go:14-28` |
| 只交换流（解释执行 build.go 时） | `pkg_discover.go:160-163` |
| `-q` 下把 stdout 指向 `/dev/null` | `build_report.go:16-45`（含手写 `restored` 幂等标志） |
| 直接 `iexec.SetQuiet` | `config_files.go:183`、`root.go:80` |

`withReport` 需要手写幂等标志，正是因为两套栈会重叠。

## 做什么

1. 新建 `internal/quiet`，三个意图：

   | 意图 | 换 fd | vlog | exec | scriptfs 只读 |
   |---|---|---|---|---|
   | `Probe` | ✓ | ✓ | ✓ | ✓ |
   | `Script` | ✓ | — | — | — |
   | `Quiet` | ✓（仅 stdout） | — | — | — |

2. 接口：`quiet.Enter(intent) (restore func())`，restore 幂等。
3. `pipeline.EnterQuietReadOnlyScope` 变成 `quiet.Enter(quiet.Probe)` 的薄调用者（保持签名，避免改动 5 个调用点）。
4. 12 处手工堆叠改为一次 `defer quiet.Enter(...)()`；删除 `build_report.go` 的手写 `restored` 标志。

## 不做什么

- 不给 `pkg/log` 注入 sink。子进程继承 fd 1，交换 Go 变量拦不住；sink 只能替掉 vlog 一条，换不来等价能力（见 `spec.md` 的"明确不做"）。
- 不把 `config check` 并入巡检前导（DD-28 已定）。

## 验收

- 通用 6 条；`inspect`/`query`/`deps`/`doctor` 输出逐字节相同。
- 新测试直接钉住三个意图各自的杠杆组合，以及 restore 幂等（连续调用两次 restore 不报错、不重复恢复）。
- `grep -c 'silenceStdout\|silenceStderr\|inspectionScope'` 在 `cmd/vmake` 与 `pkg/` 内归零。

## 涉及文件

新增 `internal/quiet/`。改：`cmd/vmake/inspect_cmd.go`、`inspect_session.go`、`pkg_discover.go`、`doctor_cmd.go`、`deps_graph_cmd.go`、`config_files.go`、`build_report.go`、`root.go`、`pkg/pipeline/config_preview.go`。
