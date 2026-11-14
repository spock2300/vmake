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

## DD-13: 增量动作由单一 plannedAction 描述

- 状态: accepted
- 背景: compile/link/incremental-void 各自内联匿名签名结构，并手工重复 check→explain→invalidate→run→save；compile 的签名命令与 `compile_commands.json`、实际执行命令由三处分别推导。clang 汇编路径执行两条命令，但签名只哈希一条普通编译命令，修改预处理参数不会轮换记录。
- 决策: `pkg/build/action_plan.go` 的 `plannedAction` 是增量动作的唯一描述：签名包含 Kind/Target/WorkDir/TargetOS/Tools/Environment/Commands/声明输入/输出；`runPlannedAction` 统一执行检查、失效、运行与记录。compile/link/void 只负责生产该值；compile 的命令由 `Compiler.planCompile` 一次推导，`compile_commands.json` 使用同一计划的 Display 命令；link 的 post-link 命令在计划中只解析一次。compile 的 `RecordInputs` 只含源文件，run 后发现的头文件依赖再写入记录；check 仍使用上一次 depfile 的依赖，因此删除头文件不会把旧路径带入新记录。`actionRecordVersion` 提升为 3。
- 理由: 让"被哈希的输入"与"实际执行的命令"由同一个值产生，消除签名漂移；匿名 schema 有三份时，新增输入必然漏一处。
- 涉及代码: `pkg/build/action_plan.go`, `pkg/build/compile_action.go`, `pkg/build/link_action.go`, `pkg/build/scheduler_compile.go`, `pkg/build/scheduler_link.go`, `pkg/build/scheduler_postlink.go`, `pkg/build/signature.go`。

## DD-14: 版本/commit/lock 优先级由单一 LockQuery 决定

- 状态: accepted
- 背景: "target > float > config pin > lock > native selected" 的顺序、`IgnoreAll`/`Float`/`Targets` 的抑制规则和错误文案曾在 `Resolver`、`pipeline` 的 `packagePin`/`lockedEntry`/`remoteVersionKey`/`resolvePackageSourceVersion`、`inspect` 与 `downloadRemoteSources` 中各写一遍；锁文件"应该是什么"也只在 `sourceStage.buildLockfile` 内联。
- 决策: `pkg/resolver/decision.go` 的 `LockQuery`（`PinFor`/`IgnoredFor`/`LockEntry`/`Decide`）是优先级的唯一实现；`Resolver` 与 `pipeline` 的辅助函数只做委托。`pkg/pipeline/lock_projection.go` 的 `lockDecision`/`projectLock`/`validateLockedProjection` 是锁文件的唯一投影与 `--locked` 比较入口，`buildLockfile`/`verifyLocked` 只收集决策。native/registry 的下载版本选择也消费 `Decide`（lock 优先于 native selected），native 版本表的选择仍由 resolver 完成。`--ignore-lock`/deps update 的 `IgnoreAll` 同时抑制 config pin 复用 lock commit，保持“完全忽略锁”的语义（resolver 测试固定）。
- 理由: 优先级与锁投影是构建、inspect、query、deps、`--locked` 的共同输入；两份实现会让命令之间静默不一致。native 的版本表选择仍由调用方完成（`repo.SelectNativeVersion` / `Pkg.SelectVersionMulti`），策略模块不接管版本表 I/O。
- 涉及代码: `pkg/resolver/decision.go`, `pkg/resolver/resolver.go`, `pkg/pipeline/pipeline.go`, `pkg/pipeline/keys.go`, `pkg/pipeline/workspace.go`, `pkg/pipeline/lock_projection.go`, `pkg/pipeline/stage_source.go`。

## DD-15: 全局 flags/links 显式传入，build key 不再读进程单例

- 状态: accepted
- 背景: `pipeline.aggregateGlobalFlags` 曾把项目 flags 写入 `toolchain.Manager` 的 project 字段，再立刻读回由 manager 合并后的 global+project 值；`build.GlobalFlagsHash()` 也读同一进程单例。构建键依赖隐式时序（插件注册、`SetProjectFlags`、哈希调用）且测试之间会泄漏状态。
- 决策: `toolchain.Manager` 只保留 extension/toolchain 注册的全局 flags，新增 `ToolchainGlobalFlags()`；项目 flags 由 `pipeline.aggregateGlobalFlags` 显式合并为 `effective`，`build.HashGlobalFlags([4][]string)` 只哈希显式值，摘要算法与旧 `GlobalFlagsHash()` 逐字节一致（测试常量锁定）。`SetProjectFlags` 与读单例的旧函数删除。
- 理由: 构建键的输入必须出现在调用链上，而不是藏在进程状态里；保持摘要兼容可避免已有 build 目录整体轮换。
- 涉及代码: `pkg/toolchain/manager.go`, `pkg/build/key.go`, `pkg/pipeline/prelude.go`。

## DD-16: 源物化结果由 SourceManager 回答

- 状态: accepted
- 背景: `sourceStage` 用 `sourceRequestReady` 重新实现 `EnsureSource` 的复用规则，仅为了决定 "Downloading package sources..." 横幅；规则漂移会让横幅说谎。
- 决策: `SourceManager.reusableState` 是唯一复用判定；`EnsureSource` 与新增 `WouldMaterialize` 共用它。`SourceResult.Materialized` 报告本次是否真正物化/重建；`sourceRequestReady` 只调用 `WouldMaterialize`。
- 理由: 预测与真实行为共享同一实现，新增 SourceRequest 字段不会漏掉第二份谓词。
- 涉及代码: `pkg/repo/source.go`, `pkg/pipeline/stage_source.go`。

## DD-17: 根与 needed 由单一 ComputeNeeded 计算

- 状态: accepted
- 背景: `computeReachable`/`computeRootSeeds`、`computeUnifiedRoots`、`inspectRootsFromEdges`、`test_cmd.validateTestProject` 各自实现根选择；多 `SetRoot`、`OnRequire` 启发式、config root local-only 与 `dependedOn` 范围在不同命令中不一致。
- 决策: `pkg/pipeline/needed.go` 的 `computeNeededGraph`/`ComputeNeeded` 是构建 needed 的唯一实现；`prelude`、`convergeFilterAndDerivedOptions`、`config check`、`test` preflight 共享。报告用的 deps/inspect 根（包含 target 级 `AddDeps`/`UseRequires` 边）由 `RootsFromEdges` 统一；`inspectRoots` 只做视图转换。
- 理由: 构建、inspect、query、test 对 "根" 的判断必须一致；报告图与 needed 图是两种视图，但各自只能有一份算法。
- 涉及代码: `pkg/pipeline/needed.go`, `pkg/pipeline/keys.go`, `pkg/pipeline/stage_config.go`, `pkg/pipeline/depgraph.go`, `cmd/vmake/inspect_cmd.go`, `cmd/vmake/test_cmd.go`。

## DD-18: 包激活只有一条路径

- 状态: accepted
- 背景: `DeclareTargets`（dry-run）与 `stage_declare.executeOnePackage`（真实构建）各自重复 `newBuildContext` → config lookup → platform → stubs → default flags → SetDirs/SetOptions/SetCfgVals/SetToolchain/SetPlatform/SetPackage → ExecBuildFuncs，差异（dry-run、stubs、config defines）靠隐式约定维持。
- 决策: `pkg/pipeline/activation.go` 的 `ActivatePackage` 是唯一激活入口，`ActivationMode` 显式区分 build/declarative，`Configure` 回调安装各路径的 config lookup 与依赖回调；`DeclareTargets` 是 declarative 包装，`executeOnePackage` 使用 build 模式并仍负责 `applyBuildContextConfig`。`check-symbols` 在只读 `scriptfs` 作用域内声明 target，并把 blocked writes 作为 error finding 报告。
- 理由: 新增 BuildContext setter 只需加在一处；dry-run 与真实构建的差异成为可审阅的 mode，而不是两份复制代码。
- 涉及代码: `pkg/pipeline/activation.go`, `pkg/pipeline/pipeline.go`, `pkg/pipeline/stage_declare.go`, `cmd/vmake/check_symbols_cmd.go`。

## DD-19: 包位置由单一 packageLocation 推导

- 状态: accepted
- 背景: 同一个包存在四套路径公式：`ExpectedPackageSourceDir`（实际源码树）、`effectiveSourceDir`（build.go/头文件基址）、`layoutFor` 的 `PkgRoot/src/member`、`preparePackageWorkspace`；再加上 `remoteTreeDir`/`PackageTreeKey`/`treeRootFor` 三种 tree 约定，inspect 与 build 可能给出不同答案。
- 决策: `pkg/pipeline/location.go` 的 `packageLocation` 是唯一推导：`OwnerTreeRoot`（远程树容器）、`PackageRoot`（build.go/头文件基址）、`SourceTree`（实际源码树）、`ThirdPartyRoot`（linked native own-Git 成员）、`TreeRoot`（state/patch 树）。`ExpectedPackageSourceDir`、`effectiveSourceDir`、`treeRootFor`、`linkedThirdPartyRoot` 与 inspect 的远程分支都委托它。`SourceDir()` 与 `SrcDir()` 的差异（包根 vs 实际源码树）保持显式。
- 理由: inspect、query、clean、build 共享一套路径语义；新增 own-Git/link 组合只改一处。
- 涉及代码: `pkg/pipeline/location.go`, `pkg/pipeline/inspect.go`, `pkg/pipeline/source_link.go`, `pkg/pipeline/stage_source.go`, `pkg/pipeline/workspace.go`。

## DD-20: 边界审计不再构造半个 Scheduler

- 状态: accepted
- 背景: `CheckExistingBuildBoundaries` 手工构造 `&Scheduler{graph,pkgs,packages,workspaceDirs,rootDir,canonCache}` 并调用 header 检查方法，依赖"这些方法只碰这几个字段"的未声明契约；新增字段会静默 nil panic，测试还要手工拼 15 个 Scheduler。
- 决策: `pkg/build/audit.go` 的 `auditEnv`/`headerAuditor` 是边界审计的显式环境；`populateHeaderBoundaries`/`checkHeaderBoundaries`/`checkStaticSourceIncludes`/`checkTargetDeclaredPaths` 等方法挂在 `*headerAuditor` 上。`Scheduler` 持有 `auditor`（`auditorFor` 惰性构造以兼容直接构造的测试），`CheckExistingBuildBoundaries` 直接构造 auditor。
- 理由: 审计需要的输入成为类型的一部分，不再随 Scheduler 字段增长而失效；cold/warm 审计路径仍由现有 boundary 测试覆盖。
- 涉及代码: `pkg/build/audit.go`, `pkg/build/header_check.go`, `pkg/build/scheduler.go`, `pkg/build/scheduler_compile.go`, `pkg/build/scheduler_resolve.go`。

## DD-21: 制品放置由单一 ArtifactPlacement 描述

- 状态: accepted
- 背景: 同一个产物路径在 `PkgInfo.OutputPath`、`ArtifactInstaller.getOutputPath`、`getTargetOutputPath` 以及 CLI 对 `PkgInstallInfo` 的重建中各算一次；安装子目录/文件名/PE import library 也分别从 `Target`、`TargetKind` 与 targetOS 推导。target 的构建结果还同时散落在 `Session.outputs`、`Scheduler.placements` 和 `BuildStats`，`BuildJob.Run` 需要重新推导输出、状态和 post-link 路径。
- 决策: `pkg/build/artifact.go` 的 `ArtifactPlacement` 是产物放置的唯一值：`OutputPath`、`ImportLib`、`StageLibDir`/`StageIncludeDir`、`InstallSubdir`/`InstallName`、`PostLinkOutputs`、`Kind`/`NoInstall`/`IsTest`。target 成功结束时由 `Scheduler.Build` 生成一个 `targetOutcome`，包含 `TargetResult`（status/outputs/duration/size）与 placement；`Session` 按 fullName 首次记录 outcome，失败 target 不记录。`BuildResult.Targets`/`TargetPlacements` 直接读 session（按完整 target 名、仅成功 target；placement 只暴露非 void target），`BuildJob.Run` 不再自行拼 target result 或合并 additional scheduler 的 placement；同步 subgraph 与主构建共享 session，因此 subgraph placement 不再丢失。`publishTarget` 和 `ArtifactInstaller`（经 `PkgInstallInfo.Placements`）消费 placement；直接构造 `PkgInstallInfo` 的旧测试仍走 fallback。
- 理由: 发布（per-package staging）与安装（prefix/filter）必须对"源产物、暂存位置、最终位置、import library"达成一致；target result 也必须和 placement 来自同一完成点，否则 subgraph、多 toolchain 和 no-op/install 路径会各自漂移。
- 涉及代码: `pkg/build/artifact.go`, `pkg/build/session.go`, `pkg/build/scheduler.go`, `pkg/build/scheduler_link.go`, `pkg/build/job.go`, `pkg/build/scheduler_publish.go`, `pkg/build/install.go`, `pkg/pipeline/build_phase.go`, `cmd/vmake/build_install.go`。

## DD-22: 依赖字符串只有一种解析

- 状态: accepted
- 背景: target 依赖字符串（裸 target、`pkg:target`、`pkg/sub:target`、`pkg:*`）在 graph 的 `resolveDep`/`resolveDirectDep`/`resolveDeclaredPkgs`/`resolveRawCrossPkg`、`subgraph.CollectSubGraphPackages` 和 `resolveDependencyLinkerScript` 中各写一遍，`strings.Cut`/`Contains("/")` 的判断已经出现轻微差异。
- 决策: `pkg/build/depref.go:parseDependencyRef` 是唯一解析，返回 `Package`/`Target`/`HasTarget` 与 `packagePath()`；上述所有消费方改用它。遍历策略仍由各消费者保留（public attrs 闭包、linker-script 依赖、subgraph 可达性），但不再各自解释字符串。
- 理由: 依赖语法的扩展（新的 target 形式、子包限定）只需要改一个解析器；遍历差异是业务差异，不应与语法解析混在一起。
- 涉及代码: `pkg/build/depref.go`, `pkg/build/graph.go`, `pkg/build/subgraph.go`, `pkg/build/scheduler_resolve.go`。

## DD-23: 命令会话拥有环境意图，锁策略只有一个来源

- 状态: accepted
- 背景: `openProject` 直接读包级全局 `ephemeralSetFlags`/`lockedFlag`/`offlineFlag`/`configFlag` 与 `commandStorageLocks`；每个命令各自拼 `LockPolicy` 字面量，`BuildOptions.Locked/Offline` 与 `ctx.LockPolicy` 两套载体在 `prepareSources`/`newSourceStage`/`deps fetch` 中各有分支。
- 决策: `cmd/vmake/session.go` 的 `commandSession` 在 `PersistentPreRun` 捕获 `--set`/`--locked`/`--offline`/`--config` 与 storage 生命周期；`openProject` 只消费会话，不再直接读这些全局。`openProject` 在 `Require` 前把会话 flag 与调用方 policy 写入 `ctx.LockPolicy`，`RunBuild` 只消费 policy，不再做字段归一；`newSourceStage`/`prepareSources` 以 stage 的 `locked/offline`（由 policy 派生）为准，`prepareSources` 不再单独看 `opts.Locked`。
- 理由: 环境意图与锁策略是跨命令不变量，散落在命令与全局变量里会漏掉某个入口；会话对象让新增命令只需要一个入口。
- 涉及代码: `cmd/vmake/session.go`, `cmd/vmake/root.go`, `cmd/vmake/project.go`, `pkg/pipeline/build_phase.go`。

## DD-24: 锁策略只有 LockPolicy 一个载体

- 状态: accepted
- 背景: `RuntimeContext.IgnoreLock` 与 `LockPolicy.IgnoreAll` 表达同一件事；`BuildOptions.Locked/Offline` 与 `LockPolicy.Locked/Offline` 又是两套载体，`prepareSources` 单独看 `opts.Locked`，`newSourceStage` 做 OR，`deps fetch` 直接写 `src.locked`。
- 决策: 对外输入 `ResolveParams.IgnoreLock` 保留兼容，但只在 `NewContext` 翻译为 `LockPolicy.IgnoreAll`；`RuntimeContext` 不再有 `IgnoreLock`。`BuildOptions` 删除 `Locked/Offline`；`RunBuild` 不再做字段归一。`newSourceStage` 的 `locked/offline` 只从 `effectiveLockPolicy()` 派生，`prepareSources` 以 `src.locked` 为准。`openProject` 在覆盖调用方 policy 后重新应用 `IgnoreAll`。
- 理由: 锁策略是构建/`--locked`/`--offline`/deps 的共同输入；多一个载体就多一个优先级分支。
- 涉及代码: `pkg/pipeline/pipeline.go`, `pkg/pipeline/phases.go`, `pkg/pipeline/build_phase.go`, `cmd/vmake/project.go`, `cmd/vmake/build_cmd.go`, `cmd/vmake/rebuild.go`, `cmd/vmake/test_cmd.go`。

## DD-25: 阶段结果类型与单次提交

- 状态: accepted
- 背景: `Require`/`Configure`/`prepareBuild` 直接写 `RuntimeContext` 的解析/配置/准备字段，阶段之间共享可变 map，调用方无法区分"第几代"的 `DerivedOptions`/`AllOptions`，配置阶段中途失败还会留下半写完的状态。
- 决策: `pkg/pipeline/state_types.go` 定义 `Invocation`（alias）、`Resolution`、`Configuration`、`Preparation`。`Require` 在结尾通过 `applyResolution` 一次提交解析结果；`Configure` 在 `RuntimeContext` 浅拷贝（`Config` 另做容器副本）上执行 `runConfigPhase`，成功后用 `applyConfiguration` 提交配置结果并写回 `Config`，失败不覆盖调用方字段；package 对象仍是共享状态，失败后由调用方丢弃上下文。`prepareBuild` 在结尾通过 `applyPreparation` 记录 needed/effective flags。`RuntimeContext.Resolution()` 返回活的 resolver/graph（调用方只读）；`Configuration()`/`Preparation()` 复制容器 map、slice 与 buffered flag slice，但 `*api.Option`/`*api.KConfigEntry` 定义对象仍按共享只读值处理，调用方不得修改。`prepareBuild` 的 derived-option 再计算显式视为 Configuration 的第二代，并在结束时刷新快照。
- 理由: 阶段边界可审阅，读模型不可被调用方篡改；配置阶段具备事务性边界。完整替换 `RuntimeContext` 字段为嵌套值对象会破坏大量 cmd/测试复合字面量，因此保留字段作为兼容门面，由结果类型承担不变量。
- 涉及代码: `pkg/pipeline/state_types.go`, `pkg/pipeline/phases.go`, `pkg/pipeline/prelude.go`, `pkg/pipeline/pipeline.go`, `pkg/pipeline/run_build_e2e_test.go`。

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
