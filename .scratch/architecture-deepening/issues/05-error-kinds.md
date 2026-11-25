# 05 — 类型化的错误 kind，取代消息匹配

**Status:** ready-for-agent
**Type:** task
**Card:** 5
**Blocked by:** 无

## 为什么

`deps check --json` 的 `kind` 由字符串嗅探决定：

```go
// cmd/vmake/deps_graph_cmd.go:330-345
switch {
case strings.Contains(msg, "circular dependency"): kind = "package-cycle"
case strings.Contains(msg, "internal package"):    kind = "internal-visibility"
default:                                            kind = "resolve"
}
```

同一子串同时匹配 `graph.go:98` 的 `"package circular dependency detected"` 与 `graph.go:631` 的 `"target circular dependency detected"`——今天 target 环只经审计路径报出（`Kind: "target-cycle"`），所以是潜伏错分；一旦 target 环经错误路径返回就会被误标成 `package-cycle`。另一处同类嗅探是 `pkg/pipeline/manifest.go:393` 对 `"yaegi lookup Main"` 的匹配（检测"这个 build.go 没有 Main"）。

有利条件：`pipeline.GraphIssue` 是 `build.GraphIssue` 的类型别名（`pkg/pipeline/graph_types.go:14`），而 `pkg/build/graph.go:44` 已在审计路径产出 kind 字符串。

## 做什么

1. `pkg/build` 定义 kind 常量（`missing-dependency`/`package-cycle`/`target-cycle`/`internal-visibility`/`resolve`）与 `ResolveError{Kind, Err}`；解析失败返回它。
2. `pkg/buildscript` 增加 `ErrNoEntryPoint`（"脚本没有 Main"），`buildscript.LoadBuildScript` 在缺少 Main 时返回它。
3. `pkg/pipeline` 增加唯一的 `issueFor(err) GraphIssue` 映射；`manifest.go` 改用 `errors.Is(err, buildscript.ErrNoEntryPoint)`。
4. 删除 `cmd/vmake/deps_graph_cmd.go` 的 `classifyResolveIssue` 与 `strings.Contains` 分支。

## 不做什么

- `GraphIssue.Kind` 仍是 `string`（JSON 契约，DD-34）。
- 不改任何已文档化的 kind 取值。

## 验收

- 通用 6 条，包含 9 个夹具的 `deps check --json` 逐字节相同。
- 新测试：构造 package 环、target 环、缺失依赖、`internal/` 越界，断言 `kind` 分别为 `package-cycle`、`target-cycle`、`missing-dependency`、`internal-visibility`，且**不**依赖错误文案。
- `grep -rn 'strings.Contains(.*"[a-z ]*dependency\|strings.Contains(msg' cmd/ pkg/` 归零。

## 涉及文件

`pkg/build/errors.go`、`graph.go`、`pkg/buildscript/`（加载错误）、`pkg/pipeline/manifest.go`、`graph_check.go`、`cmd/vmake/deps_graph_cmd.go`。

## 实现记录

- `pkg/build`：新增五个 kind 常量与 `ResolveError{Kind, Err}`；`packageCycleError`、子图自环、`checkInternalVisibility`、strict 的 `dependency not found` 现在都返回类型化错误。`GraphIssue.Kind` 仍是 `string`，JSON 不变。
- 发现 `deps check` 的包环实际来自 `pkg/api.CheckCycle`（`circular dependency: a -> b -> a`，措辞里没有 "package"），不是 `pkg/build`。把 sentinel 放进新的叶子包 `internal/issue`，而不是 `pkg/api`——放进 api 会成为脚本可见符号，`TestYaegiSymbolsComplete` 与 `TestScriptMethodManifestMatchesVisibleSurface` 都会失败（实测过：注册一个 error 值会把具体的 error 类型连同 `Error()` 方法暴露给脚本）。`CheckCycle` 用 `%w` 包住 sentinel，报文逐字节不变。
- `pkg/buildscript`：`ErrNoEntryPoint`，`yaegi lookup Main` 失败时用 `%w` 返回；`manifest.go` 改用 `errors.Is`。
- `pkg/pipeline.IssueFor` 是唯一的映射：`errors.As(*build.ResolveError)` 取 kind，`errors.Is(issue.ErrCircularDependency)` 归为 package-cycle，其余为 `resolve`；`Message` 始终是 `err.Error()`。
- `cmd/vmake` 的 `classifyResolveIssue` 与两处 `strings.Contains` 删除。

验收 grep：`strings.Contains(msg` / `strings.Contains(…"…dependency"` 在 `cmd/`、`pkg/`、`internal/` 生产代码为 0。

测试：`TestIssueForReadsTypedKindsNotMessages` 覆盖四个 ResolveError kind、require 环、以及一个含 "circular dependency" 字样但**未类型化**的错误必须落到 `resolve`——最后这条正是"不靠报文"的判据。
