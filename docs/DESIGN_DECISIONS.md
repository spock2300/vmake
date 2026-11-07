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

## DD-4: target 串行，外部回调交由原生工具增量

- 状态: accepted
- 决策: 所有 target 按依赖拓扑串行执行，只有当前 target 内部的源文件编译并行。Make/CMake helper 使用同一个 jobs 上限。同步 BuildSubGraph 共享会话、执行记录和预算；已完成的 target 不重复执行。
- 决策: Void 构建回调每次构建调用一次，不使用安装目录或包级 stamp 判断是否跳过。Make/CMake 继续负责自身增量，任意自定义生成和打包可能重复执行。
- 理由: 保持脚本 API 简洁，不新增通用动作、输入输出声明和跨运行回调缓存协议。安装复用已有声明，不重跑 OnBuild。

## DD-5: 单工作树与按需浅克隆

- 状态: accepted（v4；"每包一棵工作树"保留，跨工程共享镜像部分由 DD-6 取代）
- 决策: 不再维护共享 bare 镜像。每个包在项目内只物化一棵工作树：远程包 `<project>/.vmake_deps/<repo>/<owner>/src[/<member>]`，本地 SetGit 包 `.vmake_deps/local/<pkg>/src` 并由 `<pkgDir>/src` 链接。未复用锁定工作树时，native 包版本发现用一次 `git ls-remote`（只传 ref 列表，不传对象），物化时按 depth=1 检出所选 ref（v5 起对象优先来自 DD-6 的共享缓存；ref 解析与未命中仍访问上游）：tag 用 `refs/tags/<tag>` 命名空间（annotated tag 取 peeled commit），branch 存到 `refs/remotes/origin/<name>`，只有 commit 时按 SHA 抓取（服务端不支持时回退全量 clone）。子模块同样 `--depth 1`。本地 SetGit 包也走同一条 fetch 路径；普通本地包直接使用其源码目录。
- 决策: 目标 commit 已检出且上游 URL 未变时就地恢复（`checkout --force --detach` + `clean -fdx`），已锁定 commit 的补丁集变化不重新抓取对象；不同 commit 或上游 URL 变化时整树重建。树身份包含共享同一棵树的 native 子包（无独立仓库且本次 needed）的补丁集，其中任一补丁变化都会重置该树，避免在已打补丁的 checkout 上叠加。补丁、Kconfig `.config` 与脚本内源码改动都作用在这一棵树上。会解析工程或写工程状态的命令持有独占工程锁（解析阶段可能物化源码树/链接），同一工程的命令串行执行；每棵树另有 `tree_<hash>.lock` 串行物化。
- 理由: 完整镜像的首次构建要下载整库历史（FreeRTOS-Kernel 等仓库在大陆网络下极慢甚至超时）；`ls-remote` + depth=1 只下载所选版本的快照。当时放弃了跨工程共享镜像：跨工程共享由 DD-6（v5）以内容寻址对象缓存恢复，工作树仍按 depth=1 从该缓存本地 checkout；离线能力不再仅限已有工作树。旧 `cache/mirrors`、`cache/v2`、`vmake_deps` 在布局升级时整体删除，不提供兼容迁移。

---

## DD-6: 共享内容寻址 git 对象缓存

- 状态: accepted（v5，取代 DD-5 中"放弃跨工程共享镜像"的结论；"每包一棵 depth=1 浅工作树"保留）
- 背景: DD-5 为省流量放弃了共享镜像，代价是同一个 commit 在每个工程/CI job 都要重新向上游 depth=1 抓取，离线能力仅限已有工作树。
- 决策: 恢复跨工程共享，但**不是全量 bare 镜像**：在 `<cacheDir>/git/<urlhash>.git`（`urlhash = storage.OwnerKey(归一化 URL)`，`VMAKE_GIT_CACHE=off` 可关闭）为每个上游 URL 维护一个 bare 仓库，只累积 vmake 实际 fetch 过的对象。native 包在需要重新解析版本时仍用一次 `git ls-remote`；物化时先把所选 ref/commit 的对象按 depth=1 抓进缓存并锚定到 `refs/vmake/<commit>`，再用 `git fetch --depth 1 file://<cache>` 在项目内建出工作树。已锁定 commit 命中缓存且 checkout 成功时不访问上游；ref 解析（`ls-remote`）、HEAD 物化、缓存未命中，以及服务端拒绝按 SHA 抓取时的回退仍会访问上游。上游不可达但元数据 `refCommits` 记录了该 ref 时也能离线重建；缓存缺失/损坏/URL 变化在 `cache_<urlhash 前16位>.lock` 独占下重建，绝不阻塞构建。
- 决策: 服务端拒绝按 SHA 抓取（`uploadpack.allowAnySHA1InWant` 关闭）时，缓存兜底只做**限深的 ref tip 抓取**（`--depth 1` 的 `refs/heads/*`、`refs/tags/*`），绝不拉全历史；`cloneHeadTree` 的 commit 记录是 best-effort，只尝试按 SHA 抓取、失败即放弃，不会把一次浅 clone 变成大下载。锚点之外的 commit 仍回退到原网络 clone 路径。
- 理由: 内容寻址缓存（git 原生 SHA 命名）让同一 commit 跨工程只下载一次，同时保持 depth=1、单 commit、自包含的工作树；布局为纯增量，升级不删除既有工作树或对象缓存。

---

## DD-7: 子包 required 时 native owner 必须 needed

- 状态: accepted
- 背景: 子包与父包共享同一 checkout，但依赖图里子包并没有指向父包的隐式边。若子包因某个非 root 本地包解析父包而进入 needed、父包本身却不在 needed 集合，父包补丁不会应用、父包版本也可能没有 lock 条目。
- 决策: 配置阶段校验：任何 needed 子包的所有者（`Resolver.SubParents()` 的值）也必须 needed；否则报错并提示把父包加入 `AddRequires`。
- 理由: 相比把父包隐式标记为 needed（会额外构建/安装父包 target），显式报错让依赖声明保持真实，并避免静默使用未打补丁或未锁定的源码树。
- 涉及代码: `pkg/pipeline/stage_config.go` `validateNeededSubPackageOwners`。

## DD-8: 本地源码 link 是完全本地覆盖

- 状态: accepted
- 背景: native/registry 包通常从 lock 物化到 `.vmake_deps`，开发 SDK 时无法直接编译本地工作树。
- 决策: `vmake pkg link <repo/name> <path>` 把包源码指向本地 git 工作树，记录在机器本地的 `.vmake/local.json`。link 时删除该包已有的 `.vmake_deps/<repo>/<owner>/src` 和 `state.json`，保留 `out/` 与 own-Git 子包的 `third_party/`；link 期间该包不参与版本解析：不读 pin/lock 版本、不查 tag、不校验 `AddRequires` 约束；版本标识为 `0.0.0-local`，commit 为 link 树 HEAD。native link 的 build.go 与子包来自 link 树；registry link 仍使用 registry wrapper build.go，只替换上游源码。构建/安装输出仍写在 `.vmake_deps/<repo>/<owner>/out/...`。lock 旧条目保留但不读取/不重写，`deps` 写操作跳过 link 包；unlink 后下一次构建重新物化。
- 限制: link 包声明的 patch 被拒绝（native own-Git 子包的第三方 patch 除外）；link 包声明的 KConfig 被拒绝；link 树的 build hooks 仍会执行并可能修改该工作树；`.vmake_deps` 到 tree root 的 symlink 被拒绝，避免清理/checkout 跟随到用户仓库。
- 理由: 开发期需要“改源码立即构建”，而发布期需要可复现的 tag+lock；把两者分开，避免为了调试制造临时 tag 或直接改动物化树。

## DD-9: build 与 inspect 共享单一准备 prelude

- 状态: accepted
- 背景: `RunBuild` 与 `InspectWithOptions` 都需要先执行 FilterDeps、计算 needed、选择 chip、解析 buildConfig、聚合全局 flags；历史上两套路径各自实现，严格性和 tools 解析靠手工对齐。
- 决策: `pkg/pipeline/prelude.go:prepareBuild` 是 build/inspect 的唯一准备入口，差异只由 `preludeOptions` 的显式策略位（freeze、严格 toolchain、tools 解析）表达；`deps status/fetch` 使用窄版 `resolveNeeded`，不解析 toolchain。
- 理由: 准备阶段是构建键、inspect 输出与实际 build 的共同输入；两套实现会随时间漂移，而差异应当是可审阅的策略而不是复制代码。
- 涉及代码: `pkg/pipeline/prelude.go`, `pkg/pipeline/build_phase.go`, `pkg/pipeline/inspect.go`, `pkg/pipeline/stage_config.go`。

## DD-10: CLI 项目生命周期由 openProject 拥有

- 状态: accepted
- 背景: `build`/`test`/`rebuild`/`inspect`/`deps`/`clean` 各自重复加载输入、设置 lock policy、应用 `--set`、调用 Require/Configure 并手写 `Resolver.Close()`。
- 决策: `cmd/vmake/project.go:openProject` 是唯一生命周期入口；命令只通过 `projectOptions` 表达意图（execution、lock policy、pins、overrides、softGitignore），需要 best-effort 时走 `openProjectBestEffort`，`closeProject` 统一释放 Resolver。`loadResolveParams` 直接返回 `pipeline.ResolveParams`。
- 理由: 阶段顺序和资源释放是跨命令的不变量，散落后容易漏关 Resolver 或跳过 Configure；收口后新增命令不需要重新推导生命周期。
- 涉及代码: `cmd/vmake/project.go`, `cmd/vmake/root.go`, `cmd/vmake/build_cmd.go`, `cmd/vmake/inspect_cmd.go`, `cmd/vmake/deps_cmd.go`。

## DD-11: CMake 引擎位于 internal/cmake

- 状态: accepted
- 背景: `pkg/api` 是 buildscript/yaegi 的契约包，但曾经包含 CMake 语法解析、configure 参数构建和指纹引擎。
- 决策: 纯 CMake 逻辑（参数、语言解析、configure stamp/fingerprint/env/executable）放在 `internal/cmake`；`pkg/api` 的 `Package` 方法只做 Settings 适配，命令执行、dry-run 和输出渲染仍留在 `pkg/api`。
- 理由: 脚本契约包不应承载第三方构建系统的解析器；引擎独立后可单独测试，且 `internal/cmake` 不 import `pkg/api`，避免循环。
- 涉及代码: `internal/cmake/`, `pkg/api/package_cmake.go`, `pkg/api/package_cmake_incremental.go`。

## DD-12: 配置 TUI 只有单一构造入口

- 状态: accepted
- 背景: `Run`、`NewModel` 和 `newModel(modelInput)` 三套入口并存，生产走最完整的一套，测试却走缺字段的 `NewModel`。
- 决策: `pkg/tui.Input` + `NewModel(Input)` 是唯一构造路径，`Run(Input)` 内部调用它；派生 toolchain/chip/mode 的初始状态由纯函数 `resolveInitialState` 计算，测试与生产共用。构造时复制 `Values`/`GlobalValues`，不修改调用方传入的 map。
- 理由: 测试必须覆盖真实构造路径；隐式派生规则集中在纯函数，便于独立断言。
- 涉及代码: `pkg/tui/model.go`, `pkg/tui/state.go`, `pkg/tui/app.go`, `cmd/vmake/config.go`。

## 已知限制 / 待议

以下问题已被识别但尚未决策，修改前需单独讨论：

1. **本地项目无子包概念**：本地嵌套 build.go 被 `Scan` 收为顶层裸名包（目录 basename），
   且 basename 冲突时静默丢弃（`pkg/buildscript/scanner.go` 的 `namesSeen` 去重）。
   本地子包化是破坏性变更（现有本地多包项目的裸名引用需迁移），暂不实施。
4. **命名歧义（低概率）**：子包全名 `repo/pkg/sub` 经 `SplitPackageRef` 拆分后 pkg 部分
   含 `/`，若 registry 恰好存在名为 `pkg/sub` 的包会被误匹配（`findSource` 先查 registry）。
5. **父包不能在自己的 OnRequire 中引用其子包**：`findNativeSource` 中 `recurseDeps`
   （执行 OnRequire 依赖解析）先于 `scanSubPackages`（注册子包 source）运行，父包
   OnRequire 中的 `父包/子包` 引用会解析失败。外部包可以按全名引用子包，但同一
   `AddRequires` 列表中父包必须列在子包之前；未解析父包时的错误信息会提示这一点。
6. **子包扫描复用保留目录列表**：`ScanSubPackages` 与本地 `Scan` 共用 `skipDirs`，
   名为 `build`、`vendor`、`node_modules` 等的目录不会被当作子包。若 native 仓库
   合法地使用这些目录名声明子包，目前会被静默跳过；修改前需评估第三方源码树的
   误发现成本。
