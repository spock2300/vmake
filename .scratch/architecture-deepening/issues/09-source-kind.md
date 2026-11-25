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
## 可执行计划（planner 复核版）

设计（`pkg/resolver/resolver.go`，放在 `IsNative` 之后）：

```go
type SourceKind string
const (
    SourceKindLocal SourceKind = "local"
    SourceKindNative SourceKind = "native"
    SourceKindRegistry SourceKind = "registry"
)
func (k SourceKind) String() string { return string(k) }
// GitCompatName 是 1.x 拼写：lock 与 deps status 把本地包叫 "git"；2.0 再改（DD-34）。
func (k SourceKind) GitCompatName() string { if k == SourceKindLocal { return "git" }; return string(k) }
func (n *PackageNode) SourceKind() SourceKind {
    if n.IsLocal() { return SourceKindLocal }
    if n.IsNative() { return SourceKindNative }
    return SourceKindRegistry
}
```

| # | 改动 | 验证 | 期望 |
|---|---|---|---|
| 1 | resolver 类型 + `source_kind_test.go`（local/remote、remote+native、nil） | `go test ./pkg/resolver` | 绿 |
| 2 | inspect：删 `packageKind`，`Kind: node.SourceKind().String()` | `go test ./cmd/vmake -run '^TestInspect'` | 绿 |
| 3 | deps：`sk := node.SourceKind()`，`Source: sk.GitCompatName()`，`sk == resolver.SourceKindRegistry` | `go test ./cmd/vmake -run '^TestDeps'` | 绿 |
| 4 | manifest 写入侧用它；读取侧的字符串 switch 保留（它解析持久化 JSON，需要 default 分支） | 快照 | 无漂移 |
| 5 | catalog 四处字面量用它（**四处不是两处**：`276,329,517,549`），分支保留 | `go test ./pkg/catalog ./cmd/vmake -run 'TestPkg\|TestShow\|TestCatalog'` | 绿 |
| 6 | `deps_cmd_test.go:182` 加 `got.Source != "git"` 断言 | 该测试 | 绿 |
| 7 | `docs/INSPECT.md`：`kind` 行 + `## targets` 前一段定义别名 | — | — |
| 8 | 技能 `references/inspect.md` 加一句 `kind: local` ≡ `source: git`（**不能写 `file:line` 形式**，`TestSkillNoFileLineRefs` 会拒） | `go test ./cmd/vmake -run '^TestSkill'` | 绿 |
| 9 | `GLOSSARY.md` 加「包来源类型 / package source kind」 | — | — |
| 10 | 重建二进制 + `vmake skill install` + 夹具里 `vmake doctor` | doctor | `[ok] … matches` |

**为什么第 6 步是必需的**：9 个夹具的 `deps status` 全是 `"packages": []`，字节对照**抓不到** `git`→`local` 的回归；快照基线钉的是 manifest 的 `"source":"local"`。所以 `deps status` 的 `git` 只能靠这条断言守。

**其它实测事实**：`deps status` 的 `Source` 目前**没有任何测试钉住**；catalog 没有 `*PackageNode`，只有 `repoName` + `Env.repoIsNative`，所以它保留分支、只共享拼写；`resolver` 不依赖 catalog/pipeline，加依赖不成环；linked 远端包在 `resolver.go:528` 是 `api.SourceRemote`，直接调 `SourceKind()` 不会把 registry/native 弄错；`lock_projection.go:101` 还有第五处 `git` 拼写，本次**不动**（零行为变化，后续一行）。

**已定决策**：接受 catalog 依赖 `pkg/resolver` 取常量；`lock_projection` 的词表延后；第 6 步用扩展既有测试而不是新起子进程测试。

## 实现记录

已落地：`pkg/resolver.SourceKind` + `String()`/`GitCompatName()` + `PackageNode.SourceKind()`；inspect（删除 `packageKind`）、deps（`Source: GitCompatName()`、registry 比较）、manifest 写入侧全部改用它；`docs/INSPECT.md` 新增 "Package source kind" 段说明 `local ≡ git`；技能 `references/inspect.md` 加操作者视角的一句；`GLOSSARY.md` 新增「包来源类型」。`deps_cmd_test.go` 加了 `got.Source != "git"` 断言——9 个夹具的 `deps status` 全是 `packages: []`，字节对照抓不到这个回归，只有这条断言能守。

偏离/未做：
- catalog 的四处 `registry`/`native` 字面量**未改**为常量。catalog 现在只有仓库类型布尔、没有 `*PackageNode`，改它要新加 `catalog → resolver` 依赖换取零行为收益；等 07 收尾时一并判断。
- `lock_projection.go:101` 的第五处 `git` 拼写未动（按计划）。
- 未跑 `vmake doctor` 的陈旧技能检查（`skill install` 已执行，技能测试通过）。
