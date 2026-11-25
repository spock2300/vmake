# 09 — 包来源类型：一个概念，一个所有者

**Status:** ready-for-agent
**Type:** task
**Card:** 6
**Blocked by:** 无

## 为什么

同一个三值概念（本地 / native / registry）在两个机器可读输出里用了两种拼写：

| 出处 | 字段 | 本地包的值 |
|---|---|---|
| `pkg/pipeline/inspect_model.go:257-265` `packageKind` | `kind` | `local` |
| `pkg/pipeline/deps.go:132-137` `sourceKind` | `source` | **`git`** |
| `pkg/pipeline/manifest.go:266-270` 安装清单 | `source` | `local` |
| `pkg/catalog/catalog.go:275,328` | `source` | 只产出 `registry`/`native` |

`local` 在 `docs/INSPECT.md:288` 与安装清单里都有文档；**`git` 这个取值没有任何文档**。操作者（agent）要把 `inspect` 与 `deps status` 对上，必须自己知道 `local ≡ git`。

`pkg/api.SourceOrigin`（`package.go:123-128`）**不是**第四种拼写：它是两值（`SourceLocal`/`SourceRemote`），且经 `yaegi_symbols.go:42` 注册给脚本，DD-34 冻结。不合并。

## 做什么

1. `pkg/resolver` 增加 `SourceKind`（由 `PackageNode` 的 `IsLocal()`/`IsNative()` 派生）：
   - `String()` → `local` / `native` / `registry`
   - `GitCompatName()` → `git` / `native` / `registry`（具名的兼容投影，不是第二套规则）
2. `packageKind`、`sourceKind`、安装清单、catalog 的两处字面量全部改用它。
3. 文档：`docs/INSPECT.md` 记字段契约与别名；`cmd/vmake/skills/vmake/references/inspect.md` 加一句操作者视角的对照（`kind: local` 对应 `deps status` 的 `source: git`）。
4. `GLOSSARY.md` 增加"包来源类型 / package source kind"——缺这个词正是分裂出两种拼写的原因。
5. 改 `git` → `local` 留给 2.0。

## 验收

- 通用 6 条，包含 9 个夹具的 `inspect` 与 `deps check --json` 逐字节相同；`deps status --json` 的 `source` 仍为 `git`。
- 新测试钉住映射：同一 `PackageNode` 的 `String()` 与 `GitCompatName()` 只差本地包那一个值。
- 技能文档改动后按 AGENTS.md 流程重建 `vmake` 并 `vmake skill install`，`vmake doctor` 不报陈旧。

## 涉及文件

`pkg/resolver/resolver.go`、`pkg/pipeline/inspect_model.go`、`deps.go`、`manifest.go`、`pkg/catalog/catalog.go`、`docs/INSPECT.md`、`cmd/vmake/skills/vmake/references/inspect.md`、`GLOSSARY.md`。
