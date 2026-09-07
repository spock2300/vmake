# VMake 设计决策记录

本文件记录 VMake 中"有意为之"的设计决策（Design Decisions）：这些行为容易被误读为缺陷或缺失，
但实际是明确选择的结果。每条决策包含背景、决策内容、理由与涉及代码。修改这些决策前请先更新本文件。

条目格式：

```
## DD-<序号>: <决策标题>
- 状态: accepted / superseded by DD-xx
- 背景: ...
- 决策: ...
- 理由: ...
- 涉及代码: ...
```

---

## DD-1: Registry 包装包不支持子包

- 状态: accepted
- 背景: 远程包有两类来源——native 仓库（vmake 原生组织，build.go 位于仓库根）和 registry
  包装仓库（封装第三方 C/C++ 库，build.go 是 wrapper，源码由 wrapper 下载）。
- 决策: 子包扫描（checkout 内嵌套 build.go 被识别为 `父包/相对路径` 独立包）**仅对 native
  远程包生效**。registry 包装包 checkout 后不调用 `scanSubPackages`，其中嵌套的 build.go
  （如果存在）不会被识别为子包。
- 理由: registry 仓库的内容是第三方源码树的 wrapper，目录结构由上游第三方决定，不是 vmake
  组织的源码树；在其中发现嵌套 build.go 不代表存在 vmake 子包语义。native 仓库则完全由
  vmake 侧组织，嵌套 build.go 是包作者有意的子包声明。
- 涉及代码: `pkg/resolver/resolver.go` `findNativeSource` 末尾调用 `scanSubPackages`
  （registry 路径 `findSource` → `FindPackageGo` 不经过该调用）。

## DD-2: 子包版本完全跟随父包

- 状态: accepted
- 背景: 子包共享父包（native 仓库）的版本 checkout，没有独立的版本解析。
- 决策: 子包**不单独出现在 `.vmake/vmake.lock` 和 `manifest.json` 中**，也不接受版本约束；
  锁定/检出父包版本即锁定了其全部子包。对子包施加约束没有意义，也不被解析。
- 理由: 子包与父包同处一个 git checkout，物理上不存在独立版本；拆分会制造"同一 checkout
  内多个版本"的假象，并使 lockfile 与 manifest 语义复杂化。
- 涉及代码: `pkg/resolver/resolver.go` `scanSubPackages`（仅注册 source 与 subParents，
  不创建带版本信息的节点）；`pkg/lockfile`（按父包 ID 记录）。

## DD-3: 子包懒加载

- 状态: accepted
- 背景: 父包 checkout 完成后即能发现其中全部子包，但不是每个子包都会被依赖。
- 决策: 发现阶段（`scanSubPackages`）只把子包注册进 `r.sources` 与 `subParents`，
  **不解释其 build.go**；只有当某个包的依赖（OnRequire 或 `AddDeps`）解析到该子包时，
  才走 `findSource` → `resolveOne` 正常加载进图。
- 理由: 一个大型 native 仓库可能包含大量子包（不同功能模块），全量解释会拖慢每次构建的
  Require 阶段；懒加载保证未使用的子包零成本。
- 涉及代码: `pkg/resolver/resolver.go` `scanSubPackages` / `findSource`。

---

## 已知限制 / 待议

以下问题已被识别但尚未决策，修改前需单独讨论：

1. **本地项目无子包概念**：本地嵌套 build.go 被 `Scan` 收为顶层裸名包（目录 basename），
   且 basename 冲突时静默丢弃（`pkg/buildscript/scanner.go` 的 `namesSeen` 去重）。
   本地子包化是破坏性变更（现有本地多包项目的裸名引用需迁移），暂不实施。
2. **子包构建产物落在父包版本 checkout 内**：`setupSubPackageDirs` 以父包
   `SourceDir`（`~/.vmake/cache/<repo>/<pkg>/<ver>/src`）为根派生 build 目录，
   子包 `SetGit` 也克隆进其中，与"per-version checkout 不可变"的全局约定冲突，
   多项目共享缓存时可能互相污染。重构子包目录布局（迁入 `out/` 或项目侧）影响面大，待议。
3. **子包 `SetGit` 克隆无缓存无锁**：直接 `repo.Clone`，与本地包的 `EnsureURL`
   （`_localgit` 内容寻址 + 锁）不一致，跨项目不共享。与上一条一并解决。
4. **命名歧义（低概率）**：子包全名 `repo/pkg/sub` 经 `SplitPackageRef` 拆分后 pkg 部分
   含 `/`，若 registry 恰好存在名为 `pkg/sub` 的包会被误匹配（`findSource` 先查 registry）。
5. **父包不能在自己的 OnRequire 中引用其子包**：`findNativeSource` 中 `recurseDeps`
   （执行 OnRequire 依赖解析）先于 `scanSubPackages`（注册子包 source）运行，父包
   OnRequire 中的 `父包/子包` 引用会解析失败。外部包可以按全名引用子包，但同一
   `AddRequires` 列表中父包必须列在子包之前。
