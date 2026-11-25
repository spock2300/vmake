# 06 — 巡检前导只有一个答案

**Status:** ready-for-agent
**Type:** task
**Card:** 4
**Blocked by:** 01

## 为什么

AGENTS.md 写明"`openInspection` 是巡检命令的唯一前导……不要把前导复制进命令"，但今天有三条起点：

1. `openInspection`（`inspect_session.go:34-64`）——被认可的那条。
2. `openDepsInspection` + `runDepsInspection`（`deps_graph_cmd.go:59-105`）——重新推导 policy 与 quiet，自己静音、自己恢复，并且**绕过** `attachInspectionGraph` 直接调 `pipeline.BuildUnifiedGraph`，因此不经 `OnBlockedWrite` 记录。
3. `completeDepsWhyArg`（`deps_graph_cmd.go:484-529`）——25 行手搭：三套 restore、`lockPolicyFor`、`openProject`、`InspectWithOptions`、`BuildUnifiedGraph`、`closeProject`。只在交互补全时可达，因此无测试。

DD-39 之后 `attachInspectionGraph` 把图灌进 `Inspection` 并只留 `GraphFacts`，而 `runDepsInspection` 仍自己持有 `*UnifiedGraph` 渲染——这正是模型只留答案面之后残留的旁路。

## 做什么

1. `openDepsInspection` / `runDepsInspection` 收薄成 `openInspection` 的调用者。
2. `deps graph`/`deps why` 走 `attachInspectionGraphOptions`，渲染只读 `inspection.GraphFacts()`；`runDepsInspection` 的 `render` 签名从 `*UnifiedGraph` 改为读模型。
3. 补全复用 `runDepsInspection`，把它自己的收集器作为 render 传入——**不新增 output intent 参数**（`render` 已经是那个缝）。
4. 删除 `completeDepsWhyArg` 里手搭的 25 行。

## 验收

- 通用 6 条，包含 `deps graph` / `deps why` / `deps check` 输出逐字节相同（`--json` 与文本两种）。
- 新测试：补全路径在进程内可调用，断言 `pkg` 与 `pkg:` 两种前缀的候选项集合（今天完全无测试）。
- 新测试：`deps graph` 的 blocked-write 记录与 `inspect` 一致（钉住第 2 条）。

## 涉及文件

`cmd/vmake/deps_graph_cmd.go`、`inspect_session.go`、`inspect_cmd.go`（`attachInspectionGraphOptions` 的可复用性），`pkg/pipeline/graph_query.go`（如需 `GraphFacts` 访问器）。

## 实现记录

- `attachInspectionGraphOptions` 现在返回构造出的图（仍把图灌进模型、只保留答案面），因此"图挂载"只有一条路径，`OnBlockedWrite` 也顺带在 `deps` 系生效。
- `runDepsInspection` 不再自己调 `pipeline.BuildUnifiedGraph`，改用 `attachInspectionGraphOptions`，错误从 `inspection.GraphError()` 读——`deps graph` 的 blocked-write 记录因此与 `inspect` 一致。
- `completeDepsWhyArg` 的 25 行手搭前导删除，改为 `resolveProjectRoot` + `openDepsInspection` + `attachInspectionGraphOptions`；包名仍取自 `session.ctx.DepGraph`，目标名取自返回图的 `Facts()`。新增 `TestDepsWhyCompletionUsesSharedPreamble`（这是补全路径的第一个测试）。
- 未做（明确记录）：`render` 签名仍然是 `*RuntimeContext, *pipeline.UnifiedGraph`，因为 `pipeline.WhyQuery(ctx, g, query, allPaths)` 需要整张图，而 DD-39 之后模型只保留答案面。要让 `deps why` 只读 `GraphFacts`，得先把 `WhyQuery` 改成消费事实；那是独立的一次改动，不在本卡内硬塞。

## 收尾（round 3）

- `runDepsInspection` 的 render 改为 `(ctx, inspection, graph)`：`deps graph` 用 `inspection.GraphFacts()`（新增访问器，深拷贝答案面），`deps why` 用图本身。这样 `deps graph` 完全不再持有 `*UnifiedGraph`。
- `deps why` 收图是**代码事实**，不是遗漏：`WhyTarget` 读 `declaredTargets`、`TargetDeps` 和 `inspection.Package()` 视图，三者都不在 DD-39 保留的答案面里。要改就得把图塞回模型，与 DD-39 冲突。DD-40 决策 4 已按这个实测结论改写。
- 补全：仍复用 `openDepsInspection` + 同一挂载（`runDepsInspection` 的 render 缝需要一个 graph 形参，补全不需要图，强行复用会多传一个 nil）。这一点已在 ticket 记录里保持原样。
- `TestDepsGraphReportsTheSameBlockedWriteAsInspect` 已补（round 2）。
