# 架构深化第二轮（architecture-deepening）

## 目标

把 2026-11-24 架构复盘的 8 个候选落成实现。全部属于**结构调整**：不改命令行语义、不改 JSON 契约、不改脚本 API（DD-34）。

## 基线（实测，非估算）

| 度量 | 值 |
|---|---|
| `go test ./cmd/vmake` | 59.6s / 279 个 Test / 43 个测试文件 |
| `pkg/` + `internal/` 合计 | ≈42s / 42k 生产代码行 |
| 命令级子进程调用 | **211** 处（203 个助手调用 + 8 处直接 `os.Executable()`），分布在 21 个文件 |
| 直接写包级 flag/状态全局 | 52 处 / 10 个文件 |
| 包级可变全局（cmd） | 111 个 = 49 个 `*cobra.Command` 单例 + 38 个 pflag 目标 + 24 个其他 |
| pflag 注册表达式 | 75 个，51 个不同目标（38 全局 + 13 函数局部） |
| `func init()`（cmd） | 20 个；`RootCmd.AddCommand` 24 处（1 处在运行时由 `loadPlugins` 追加） |
| 致命路径 | 108 个调用点 / 47 个生产函数 / 12 个 `os.Exit` 位置 |
| 生产代码 `defer` | 45 处（12 个 `closeProject`、7 个 session close、6 个 restore） |

## 顺序

`01 → 02 → 03 → 04 → 05 → 06 → 07 → 08 → 09`（见 `issues/`）。依赖只有三条：02 依赖 01，06 依赖 01，08 依赖 01 与 07。

## 验收（每张 ticket 都适用）

1. `CGO_ENABLED=0 go build -o vmake ./cmd/vmake`
2. `CGO_ENABLED=0 go vet`（改动涉及的包）
3. `CGO_ENABLED=0 go test`（改动涉及的包）
4. `(cd test_data/_snapshot && go test)` 全量零漂移
5. 触及推导或输出的 ticket（04、05、09）额外要求 9 个夹具的 `vmake inspect` 与 `vmake deps check --json` 输出逐字节相同：
   `01_simple_c`、`02_with_config`、`03_multi_target`、`05_conditional`、`15_subgraph_siblings`、`18_config_header`、`19_config_defines`、`20_config_propagate`、`21_root_package`
6. 新测试只落在新的缝合面上；不允许为保住旧结构而把测试写成对已删函数的包装

## 已定政策

- **缝合面位置**：`run()` 留在 `cmd/vmake`，不新建 `internal/cli`。第二个消费者出现前不抽包。
- **fatal 路径**：`fatalErr`/`fatalMsg`/`fatalUsage`/`fatalResolveRoot` 改为 panic 一个 `*exitError`，`Execute()` 边界 recover 并返回退出码。108 个调用点不变。
- **过程级状态**：`run()` 入口复位 `cachedProjectDir`、`activeCommandSession`、`extensionRepoLocks`；`vmakeDir` 改由 `runEnv` 传入。
- **测试策略**：助手整体翻面成进程内调用；保留**一个**子进程冒烟测试钉住进程边界；插件/信任/扩展类测试（约 60 处）保留一个统一的 `runSubprocess` 助手。
- **落档**：本 spec + `issues/` 存计划；实现记录进 `docs/DESIGN_DECISIONS.md`（DD-40）。

## 明确不做

- **不立"toolchain 管理器可重置"的卡片**。`pkg/toolchain.defaultManager` 在 `sync.Once` 后面，重置它要改 1113 行包的构造方式，而按上面的测试策略没有任何测试需要干净的管理器。管理器是**进程级、不可重置**，写成 DD-40 的显式约束。等出现第二个消费者再议。
- **不把 `os.Stdout` 交换改成给 `pkg/log` 注入 sink**。子进程继承 fd 1，交换 Go 变量拦不住，`internal/exec.SetQuiet` 必须独立存在；sink 只能替掉 vlog 一条，换不来等价能力。
- **不合并 `api.SourceOrigin`**。它是两值（`local`/`remote`）且经 `yaegi_symbols.go:42` 注册给脚本，DD-34 冻结；与三值显示词表不是同一概念。
- **不重跑整套 skill-loop**（solver/judge/author）。收尾只跑 `factcheck-*`。

## 完成状态（DD-40）

| 卡 | 提交 | 结果 |
|---|---|---|
| 01 命令树与 `run` | `bb81673` | `run(argv, stdout, stderr, runEnv) int` 是唯一缝合面；每进程仅 `Execute` 一处 `os.Exit`；致命路径走 panic `*exitError`，defer 恢复执行 |
| 02 测试翻面 | `71105ca` | `runCLI` 进程内 + `runSubprocess` 单入口；子进程调用 211 → ~54；`TestCommandProcessBoundary` 钉住进程边界 |
| 03 `internal/quiet` | `0f6244d` | 五个静音杠杆收敛到一个包、四个意图；`silenceStdout`/`silenceStderr`/`inspectionScope`/`quietScriptScope` 归零 |
| 04 单一放置值 | `bc0861f` | `PlacementFor` 独占 PE 导入库与 `Prebuilt` 规则；解析阶段内联 `.a` 拼接删除 |
| 05 类型化错误 kind | `16ab40c` | `build.ResolveError` + `internal/issue` sentinel；两处 `strings.Contains` 报文嗅探删除 |
| 06 巡检前导 | `41ee663` | `attachInspectionGraphOptions` 单路径；补全复用前导并首次有测试 |
| 07 包目录接口 | `3c3ddac` `da05c16` `1b3fed3` `f6ea3f5` | catalog 不再依赖 pipeline；`RepoAccess` 注入；`Open(env)` 门面；测试 1 → 9 |
| 08 转发层 | `32faa43` `ebb7cf0` `6319713` `199971a` `40f36aa` `4830a0e` | 单语句转发 28 → 1（剩下那个是组合函数值，非转发） |
| 09 来源词表 | `1b6f630` `545b45c` | `resolver.SourceKind` + `GitCompatName`；四处生产者与 catalog 字面量归位；`local ≡ git` 进入 INSPECT.md、技能与 GLOSSARY |

全部门禁：`go build`、全量 `go vet`、`./cmd/vmake ./pkg/... ./internal/...`、9 个夹具字节一致性、`TestSkill*`、`vmake skill install`。

**未完成的验收项（如实记录，不算通过）**：

- 03：`ProbeSilent`/`Script` 意图已落，`doctor` 与 `config check` 改成一次 `defer quiet.Enter(...)()`；`inspect`/`deps graph`/`withReport` 仍按需分段恢复（`inspect` 在函数中途恢复 stderr 再写 JSON），所以"12 处手工堆叠全部消失"只做到一部分。
- 06：`render` 仍收 `*pipeline.UnifiedGraph`（`deps_graph_cmd.go`），因为 `pipeline.WhyQuery` 需要整张图；DD-40 决策 4 的"只读 `inspection.GraphFacts()`"未实现。补全复用 `openDepsInspection` 而非 `runDepsInspection`。"blocked-write 与 inspect 一致"已补 `TestDepsGraphReportsTheSameBlockedWriteAsInspect`（实测三条命令都会报 `read-only inspection` + `blocked.txt`）。
- 04/05/09：`inspect`/`deps check --json` 的 9 夹具字节一致性由 `test_data/_snapshot` 覆盖（`baseline/` 未改动即为零漂移），但没有把两命令的对照产物提交进仓库。
- 08：指标达成（28 → 1），但 DD-40 决策 7 写的"10 个测试搬进 `pkg/pipeline`"与实际不符——实际是 2 个进 `pkg/catalog`、2 个进 `pkg/pipeline`、其余端到端 CLI 测试原地保留只改调用目标。

## 验证注意事项（本轮实测）

**不要把 `test_data/_snapshot` 全量套件与 `go test ./cmd/vmake` 并行跑**。两者都会在 `test_data/*` 夹具里删除/生成 `build`、`install`、`vendor` 与 `.vmake`，并行时会互相踩到：本轮并行执行时 `TestInspectFixtures` 报失败，串行重跑即通过（`TestSnapshotsTestData` 只剩 11/13/25 三个已知前置缺失）。并行执行时看到这条失败，先串行复跑再判断是否为回归。

## 最终验证证据（收尾）

- `CGO_ENABLED=0 go build -o vmake ./cmd/vmake`：通过。
- `CGO_ENABLED=0 go vet ./cmd/vmake/... ./pkg/... ./internal/...`：无输出。
- `CGO_ENABLED=0 go test ./cmd/vmake ./pkg/... ./internal/...`：无失败。
- `(cd test_data/_snapshot && go test)`（`HOME`/`GOCACHE` 指到临时目录）：48.7s，**只有 11/13/25 失败**，即 AGENTS.md 记录的前置缺失（`official/tinyexpr` 未注册、`test_build` 原生仓库未建、25 需先跑 `setup.sh`）；其余夹具零漂移，说明 `build`/`install`/`manifest`/`compile_commands` 的行为未变。
- `go test ./cmd/vmake -run '^TestSkill'`：通过（技能文档与 CLI 一致）；`vmake skill install` 已执行。
- 不要并行跑 `_snapshot` 与 `./cmd/vmake`（二者都改 `test_data/*` 夹具），见上面的验证注意事项。

## 遗留的两处"按设计不收敛"（不是缺口）

1. **03**：`inspect` 必须在写 JSON 前恢复 stderr、`deps graph` 只在 `--json` 时静音 stderr、`withReport` 在 panic 分支恢复 stdout——这三处是行为约束，收成单个 `Enter` 会让输出跑到错误的位置。静音杠杆本身已全部归 `internal/quiet`。
2. **06**：`deps why` 收图，因为 `WhyTarget` 依赖 `declaredTargets`/`TargetDeps`/`inspection.Package()`，三者不在 DD-39 的答案面里。DD-40 决策 4 已按实测改写，`deps graph` 已完全走 `inspection.GraphFacts()`。
