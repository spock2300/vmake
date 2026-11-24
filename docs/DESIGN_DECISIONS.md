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
- 修订（2026-11-15）: 安装器的 fallback 已删除。`ArtifactInstaller` 只安装带 placement 的 target（没有 placement 表示本次构建没有产出），void target 显式跳过；直接构造 `PkgInstallInfo` 的调用方必须先用 `PlacementFor` 生成 placement（测试已改为如此）。

## DD-22: 依赖字符串只有一种解析

- 状态: accepted
- 背景: target 依赖字符串（裸 target、`pkg:target`、`pkg/sub:target`、`pkg:*`）在 graph 的 `resolveDep`/`resolveDirectDep`/`resolveDeclaredPkgs`/`resolveRawCrossPkg`、`subgraph.CollectSubGraphPackages` 和 `resolveDependencyLinkerScript` 中各写一遍，`strings.Cut`/`Contains("/")` 的判断已经出现轻微差异。
- 决策: `pkg/build/depref.go:parseDependencyRef` 是唯一解析，返回 `Package`/`Target`/`HasTarget` 与 `packagePath()`；上述所有消费方改用它。遍历策略仍由各消费者保留（public attrs 闭包、linker-script 依赖、subgraph 可达性），但不再各自解释字符串。
- 理由: 依赖语法的扩展（新的 target 形式、子包限定）只需要改一个解析器；遍历差异是业务差异，不应与语法解析混在一起。
- 涉及代码: `pkg/build/depref.go`, `pkg/build/graph.go`, `pkg/build/subgraph.go`, `pkg/build/scheduler_resolve.go`。
- 修订（2026-11-16）: `resolveDep` 与 `resolveDirectDep` 合并为单遍 `resolveDepSpec`，统一校验 `pkg:target` 存在性；`build.AuditBuildGraph` 将原本在 `pkg/pipeline/graph_build.go` 与 `graph_rules.go` 中复刻的 0-target 透传展开、子包最短路径解析、`internal/` 可见性去重与环路检测统一收进 `pkg/build/graph.go`，并通过 `PkgBuildMeta.Unmaterialized` 直接供 `CheckExistingBuildBoundaries` 消费，删除 `CheckProjectDeps` 中伪造虚拟 `TargetVoid` 节点的二次构图（见 DD-36）。

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
- 决策: `pkg/pipeline/state_types.go` 保留 `Invocation`（alias）与 `Configuration`。`Require` 在结尾直接设置 `ctx.Resolver`/`ctx.DepGraph`（不维护镜像字段）；`Configure` 在 `RuntimeContext` 浅拷贝（`Config` 另做容器副本）上执行 `runConfigPhase`，成功后用 `applyConfiguration` 提交配置结果并写回 `Config`，失败不覆盖调用方字段；package 对象仍是共享状态，失败后由调用方丢弃上下文。`prepareBuild` 通过返回的 `buildPrelude` 传递 needed/raw/effective flags 与 hash，不再写 `ctx.preparation`。`RuntimeContext.Configuration()` 复制容器 map、slice 与 buffered flag slice，但 `*api.Option`/`*api.KConfigEntry` 定义对象仍按共享只读值处理，调用方不得修改。`prepareBuild` 的 derived-option 再计算显式视为 Configuration 的第二代，并在结束时刷新快照。
- 修订（2026-11-15）: `Resolution`/`Preparation`、`applyResolution`/`applyPreparation` 与两个 accessor 已删除——生产代码从未读取它们（仅测试引用），镜像与实际字段必然漂移；`prelude.go` 的自我 clone 一并删除。`Configuration` 保留为**读取时派生**的视图：`ctx.configuration` 快照字段、`snapshotConfiguration` 与 `applyConfiguration` 的 clone 均已删除，`Configuration()` 每次从当前字段克隆，因此芯片派生（`displayChipProfile`）不再需要“改字段后重新快照”。后续只剩字段私有化（155 处只读站点）。
- 理由: 阶段边界可审阅，读模型不可被调用方篡改；配置阶段具备事务性边界。完整替换 `RuntimeContext` 字段为嵌套值对象会破坏大量 cmd/测试复合字面量，因此保留字段作为兼容门面，由结果类型承担不变量。
- 涉及代码: `pkg/pipeline/state_types.go`, `pkg/pipeline/phases.go`, `pkg/pipeline/prelude.go`, `pkg/pipeline/pipeline.go`, `pkg/pipeline/run_build_e2e_test.go`。

## DD-26: 有效配置值只有一个实现，按用途暴露投影

- 状态: accepted
- 背景: “包看到的有效值”曾被多处分别推导：`PackageConfigValues`（构建上下文）、`packageConfigValuesWithGlobals`（OnApply，使用 raw config、无默认值与 platform）、`collectAllPkgOptions`（构建键材料）、cmd 的 `packageConfigValues`/`inspectPackageOptions`（inspect/query）以及 `build_report.effectiveGlobalValues`（report 复制 projectGlobalValues）。规则不同导致 OnApply 读到未解析的 `mode`/`toolchain` 且 `target_os`/`target_triple` 直接 fatal；inspect 需要手工对齐 build，历史上已有 8145d19、b0d92da 两次修补。
- 决策: `pkg/pipeline` 是有效配置值的唯一实现，并按用途暴露显式投影：`PackageConfigValues`（构建值：默认值 + 派生 + entry + 可见全局 + 包级 platform）、`DisplayPackageValues`（展示值：仅已声明选项，不合成 platform 键）、`collectAllPkgOptions`（构建键材料：entry + 非默认派生 + 可见全局，输出逐字节保持旧行为）、`applyGlobalValues`（OnApply 值：解析后的 mode/toolchain 与 target 默认值）、`ProjectGlobalValues`（项目全局值）。`Inspection` 携带展示投影（现经 `Inspection.Package(name)` 的 `PackageView.Values`/`PackageView.Platform` 暴露，见修订）；cmd 的 inspect/query/report 只消费投影，不再自行推导。inspect `packages[]` 只新增附加字段 `platform {targetOs, targetTriple}`，`options` 仍只含已声明选项。
- 理由: 四个消费者的规则必须一致但投影不同；把规则收进一个模块后，配置相关改动只改一处，inspect 不再需要手工对齐 build。构建键材料保持逐字节一致，避免已有 build 目录旋转。
- 涉及代码: `pkg/pipeline/config_values.go`, `pkg/pipeline/pipeline.go`, `pkg/pipeline/inspect.go`, `pkg/pipeline/phases.go`, `pkg/pipeline/keys.go`, `cmd/vmake/inspect_cmd.go`, `cmd/vmake/query_cmd.go`, `cmd/vmake/build_report.go`。
- 修订（2026-11-15）: `Inspection` 的六个 per-package map 已私有化（DD-31），展示投影改由 `PackageView` 携带；渲染层不再直接读取 map。

## DD-27: 脚本方法面由显式清单收口，分阶段隐藏宿主接线

- 状态: accepted（阶段 1、2、3 已落地）
- 背景: yaegi 注册类型即暴露其全部导出方法（含嵌入提升的方法与字段），`pkg/api/yaegi_symbols.go` 的包级 allowlist 只能约束包级名字。`pkg/api` 当前有 488 个类型-方法对，其中大量是宿主接线或未文档化方法，脚本可以误用，且新增导出方法不会被任何测试发现。
- 决策: 阶段 1 在 `pkg/api/script_methods.go` 建立两份清单：`scriptMethodAllowlist`（文档化或验收语料在用）与 `scriptMethodPending`（可见但未文档化/仅宿主使用）。`TestScriptMethodManifestMatchesVisibleSurface` 反射所有已注册类型（含提升方法），要求可见方法集合恰好等于两份清单的并集；新增/删除导出方法必须显式分类。阶段 2 把 pending 中的宿主接线移到不注册进 yaegi 的导出 binding 函数（`api.Bind*`，由 `TestYaegiSymbolsComplete` 的前缀规则排除），保持 `*api.Package` 与回调签名不变；当前 allowlist 442 对、pending 0 对。阶段 3 重整嵌入：`ConfigAccessor` 的 `CfgVals`/`Options` 与 `PackageMeta` 的 `Repo`/`Name` 已改为非导出字段，宿主通过 `BindCfgVals`/`BindOptions`/`BindSetCfgVals`/`BindSetOptions`/`BindPackageName`/`BindPackageRepo` 访问；`TargetRegistry`/`InstallItemHolder` 的字段本就非导出，只剩 allowlist 中的方法被提升。验收语料 = vmake `test_data/**`、`official_repo/**`、docs/skill Go 块 + hl-demo / hl-mcu-sdk 的 `build.go`；语料在用或文档化的方法保留。阶段 1 不改变运行时行为。
- 理由: 方法级契约不能靠包级 allowlist 或文档自觉；先建立可失败的清单，再分阶段迁移，避免一次性重写脚本契约。pending 清单就是阶段 2/3 的工作队列。
- 涉及代码: `pkg/api/script_methods.go`, `pkg/api/script_methods_test.go`, `pkg/api/yaegi_symbols.go`, `pkg/api/accessor.go`, `pkg/api/package.go`, `pkg/api/context.go`, `pkg/api/target.go`。

## DD-28: 测试缝与巡检前导的边界

- 状态: accepted
- 背景: 架构复盘的 8 个候选方向里有两条收尾项没有可以零风险落地的形态。(1) 测试侧：`pkg/pipeline` 的 `newBuildPhaseState` 曾手工拼装 `configStage`/`declareStage` 并只跑 `collectAllPkgOptions`，复刻了 `prepareConfig` 的一半；现在它改为调用真实 `prepareConfig`，剩余部分只是"在构造后往图里加包再刷新目录"的脚手架。把 8 个测试文件全部迁到 `stageFixture` 的收益是风格统一，代价是大幅改写既有断言。(2) 巡检侧：`inspectionRequest` 的 `onError` 回调（把 `*inspectionStageError` 的映射收进前导）会把前导与退出码耦合，`config_files.go` 的 `silenceStdout/silenceStderr` 与 `inspectionScope` 的"日志级静音"语义不同，补全路径又受 tty 下 `Fscanln` 阻塞约束。
- 决策: 保留 `newBuildPhaseState` 作为测试专用适配器(它已调用真实 `prepareConfig`，不再复刻规则)；不为巡检前导增加 `onError`，`config check` 不并入 `openInspection`，补全路径不并入前导。作为交换，新增 `cmd/vmake/inspect_preamble_test.go` 钉住这块策略：`inspect` stdout 必须是纯 JSON、`query` 保留进程内打印但静音子进程、blocked write 在 `inspect` 诊断与 `check-symbols` findings 中的呈现、以及 `OnConfig` panic 在 `inspect`/`query targets`/`check-symbols`/`deps graph` 的退出码与信封。
- 理由: 把差异搬进共享前导只减少几行重复，却让前导承担退出码语义并改变 `config check` 的可观察输出；测试脚手架不是生产规则，保留它比大规模改写既有断言更稳。行为由测试而不是由结构保证。
- 涉及代码: `pkg/pipeline/state_test_helpers_test.go`, `cmd/vmake/inspect_session.go`, `cmd/vmake/config_files.go`, `cmd/vmake/inspect_preamble_test.go`。

## DD-29: 依赖图只有一个答案面

- 状态: accepted
- 背景: `inspect` 与 `deps graph` 曾各自推导包边：`inspect` 用 `inspectGraph` + `mergeInspectTargetEdges`（共约 128 行）合并 require/子图/target 边，`deps graph` 用 `BuildUnifiedGraph`；两条命令对同一个项目给出不同边集。图构造还承担了 target 声明，导致 `deps why` 对非 needed 包用 nil toolchain 再声明一次并丢弃 blocked write。
- 决策: `BuildUnifiedGraph` 是唯一构造器，`GraphOptions{IncludeAll, DeclareTargets}` 选择范围与是否执行 OnBuild(声明结果缓存在图里，`DeclaredTargets`/`BuildContext` 读取)；`GraphFacts` 是稳定答案面(包/目标/边/roots/包依赖/issues 的副本)，`inspect`/`deps graph`/`deps why`/命令补全只消费 `GraphFacts`。inspect 的诊断与 `deps graph` 的 warning 只对 `facts.Needed` 内的包报告；`deps status` 的版本投影改用 `LockQuery.Decide` 并保留"commit pin 只显示 commit"这一展示规则。
- 理由: 同一个问题只有一个实现，两个命令的边集在 hl-demo 上逐字节一致(221 条)；target 声明跟随图构造，`deps why` 不再重复声明；答案面与构造状态分离后，渲染器无法依赖内部 map。
- 涉及代码: `pkg/pipeline/graph_types.go`, `pkg/pipeline/graph_build.go`, `pkg/pipeline/graph_query.go`, `pkg/pipeline/graph_check.go`, `pkg/pipeline/deps.go`, `cmd/vmake/deps_graph_cmd.go`, `cmd/vmake/inspect_cmd.go`。
- 修订（2026-11-15）: `deps why` 对**不在活跃根集合**内的包仍会自行调用 `DeclareTargets` 探测目标（图只声明 needed 包）；此时 inspection 对过滤掉的包不解析 toolchain，探测可能带 nil toolchain。这是答案面之外的唯一探测点，注释与本节保持一致，不再声称"复用真实 toolchain"。

## DD-30: 一个包布局，事实与目录各有一个推导

- 状态: accepted
- 背景: 包布局的构建键材料曾被构建阶段(fetch 状态、远端 commit)和 inspect(lock、native 版本)各自推导；`ResolveAllPackageDirs`(包根目录)与 `workspacePackageDirs`(链接/落盘树)也是两条独立遍历；`PackageLayout`→`PkgInfo` 在 scheduler、header 审计、inspect、check-symbols 各有一份转换。
- 决策: `pipeline.LayoutFacts`(`Version`/`Commit`/`PatchHash`/`SourceLink`/`FlagsHash`)是全部构建键材料，构建阶段与 inspect 各自适配来源但必须产出同一值；`PackageLayout.PkgInfo()` 是唯一 layout→PkgInfo 转换(not-ready 布局产出无 BuildDir/BuildKey 的条目)；`SourceTree`/`packageSourceTrees` 是包源位置的唯一推导，`packageSourceDirs`(包根)与 `workspacePackageDirs`(链接/落盘树)是它的投影。键格式未改，`layoutFormatVersion` 未 bump。
- 理由: 事实与目录各自只有一个推导，任何适配器漏字段都会被测试(`TestLayoutFactsCarryAllKeyMaterial`、`TestSourceTreeProjections`)抓住；不轮转 build key 意味着已有构建目录继续有效。
- 涉及代码: `pkg/pipeline/keys.go`, `pkg/pipeline/layout.go`, `pkg/pipeline/inspect.go`, `pkg/pipeline/build_job.go`, `pkg/build/key.go`, `pkg/build/scheduler.go`, `pkg/build/header_check.go`。
- 修订（2026-11-15）: 构建阶段的远端布局改用 `packageLocation` 取 `Owner`/`Member`/`PackageRoot`（link 覆盖），只保留"本次 fetch 实际创建的树根"（`remote.trees`）作为 `PkgRoot` 的覆盖，回退到 `loc.OwnerTreeRoot`；这样两个适配器共享同一套成员/链接推导，同时不改变已物化树的输出目录。`compile_commands.AddCommand`（第二套编译命令推导）已删除，写入口只剩 `AddSpec`。

## DD-31: 配置状态全面收口

- 状态: accepted
- 背景: `RuntimeContext` 上的 8 个配置字段（`AllOptions`/`AllKConfigs`/`ChipProfiles`/`ChipProfile`/`GlobalOptions`/`DerivedOptions`/`BufferedGlobalFlags`/`GlobalMacroDefines`）曾可被任意包读写，"已提交、只读"只是约定。阶段写模型要求把它们收进访问器，使不可写成为类型事实。
- 决策: 全部 8 个字段私有化（`allOptions`/`allKConfigs`/`chipProfiles`/`chipProfile`/`globalOptions`/`derivedOptions`/`bufferedGlobalFlags`/`globalMacroDefines`），跨包读取走返回副本的访问器（`AllOptions()`/`PackageOptions(pkg)`/`KConfigs()`/`ChipProfiles()`/`GlobalOptions()`/`DerivedOptions()`/`GlobalDefines()`）；`applyConfiguration` 仍是唯一写入口。反射守卫 `TestRuntimeContextHidesConfigurationState` 覆盖全部 8 个字段。
- 修订（2026-11-16）: 随着命令级选项解析与变更收进 `pkg/pipeline/config_mutate.go`（`ResolveSetPackageName`/`ResolveSetPackageOption`/`MutateConfig`），`AllOptions` 的历史例外被正式消除并私有化为 `allOptions`。
- 涉及代码: `pkg/pipeline/pipeline.go`, `pkg/pipeline/state_types.go`, `pkg/pipeline/state_guard_test.go`, `pkg/pipeline/config_mutate.go`, `pkg/pipeline/chip.go`, `cmd/vmake/config.go`, `cmd/vmake/query_cmd.go`, `cmd/vmake/build_install.go`, `cmd/vmake/clean.go`, `cmd/vmake/inspect_cmd.go`。

## DD-32: `--locked` 读原始 lock 条目，float 参与 sync

- 状态: accepted
- 背景: `--locked` 的校验与 `deps sync/update` 的"要触碰哪些包"曾被当作版本解析问题，试图直接复用 `LockQuery.LockEntry`（它按 `IgnoredFor` 隐藏被 float/target 覆盖的条目）。但两者问的是不同问题：解析问"该用哪个版本"，校验问"锁是否仍反映这次构建"。
- 决策: `PinLockMismatch`/`LockEntryProblem` 读 `rawLockEntry`（绕过 `IgnoredFor`），因此 target pin、float、`--ignore-lock` 的包仍会与 lock 比较；`--locked` 下 target pin 与锁不一致会报错。`NeedsSync` 对 float 包返回 true：float 的版本故意不可复现，lock 必须跟随它更新；target pin 同样始终触碰。版本解析仍走 `LockEntry`（`IgnoredFor` 优先），两条路径共享同一个 `LockQuery` 与 `LockProblem` 文案。
- 理由: 若校验也走 `IgnoredFor`，`--locked` 会静默接受"锁定值与将要构建的值不同"的包；若 sync 跳过 float，锁会长期滞后于实际解析结果。两条语义都写在 `decision.go` 的注释里，并由 `TestLockQueryPinLockMismatch`/`TestLockQueryNeedsSync`/`TestVerifyLockBeforePreparePinMismatch` 钉住。
- 涉及代码: `pkg/resolver/decision.go`, `pkg/resolver/decision_test.go`, `pkg/pipeline/stage_source.go`, `pkg/resolver/resolver.go`, `pkg/pipeline/deps.go`。

## DD-33: 本地包重名是硬错误

- 状态: accepted
- 背景: 本地项目没有子包语义，`Scan` 把任意深度的嵌套 `build.go` 收为顶层包，包名取目录 basename。两个不同目录名相同时（例如 `app/net/build.go` 与 `lib/net/build.go`），旧实现保留先遇到的、静默丢弃另一个。
- 决策: 重名返回错误，错误信息给出两个冲突的 `build.go` 路径；`ScanSubPackages` 的远程子包全名不受影响。
- 理由: 静默丢弃会产生无声的错误构建：操作者会报告成功，而某个包从未被编译，这违反 agent-first 的验收前提（见 `docs/GOALS.md`）。真正的本地子包语义仍是破坏性变更，未实施。
- 涉及代码: `pkg/buildscript/scanner.go` `Scan`, `pkg/buildscript/scanner_test.go`。

## DD-34: 1.x 内脚本 API 与 CLI 只增不改

- 状态: accepted
- 背景: 一次 API 重塑（`fd7aa64`）删除了 `SetConfigFiles`，并把 `AddGlobalCFlags`/`AddGlobalCxxFlags` 从 `...string` 改为 `...any`。真实固件工程 hk-mcu-sdk（`4e7862a`）没有改动任何脚本，升级后依次在 py32f539 报 `undefined selector: SetConfigFiles`、在 lvgl 报 `reflect: CallSlice using []string as type []interface {}`，lwip 还有同类的旧展开。三次失败信息都指向库内部，而不是"这个 API 已删除/已改签名"。
- 决策: 1.x 内脚本 API 与 CLI 只增不改：不删除、不改现有语义；需要变更时先新增，旧的保留并标注弃用；移除只发生在 2.0。这是内部规则，公开兼容承诺等第一个外部采用者出现后再升格（见 `docs/GOALS.md`）。
- 理由: 采用者的构建脚本会比二进制活得久；没有弃用期的修改会把"一次成功"的失败归因变成噪声——操作者会先去修错误的层。内部约束的成本接近零：脚本面已是显式 allowlist，并由测试锁死。
- 涉及代码: `pkg/api/yaegi_symbols.go`, `pkg/api/script_methods.go`, `AGENTS.md`, `docs/GOALS.md`。

## DD-35: 技能安装记录版本与内容摘要

- 状态: accepted
- 背景: 技能文档是嵌入二进制的副本，`vmake skill install` 把它写到 `~/.agents/skills/vmake/`。升级二进制后若没重装，agent 会继续按旧文档工作；M0 排查时本机就同时存在旧二进制和旧技能副本，而两者都不会主动报错。
- 决策: `vmake skill install` 在安装目录写 `.vmake-skill.json`（`schemaVersion` 1，含 `version`/`commit`/`buildDate`/`skillDigest`/`installedAt`）；`skillDigest` 是嵌入技能 `.md` 内容的 sha256，排除安装时生成的 `references/cli.md`。`vmake doctor` 仅在 `SKILL.md` 存在时检查：记录缺失/无效或摘要不符报 error 并提示重跑 `vmake skill install`；摘要一致时输出一条 ok；技能未安装不报告。
- 理由: agent 的行为由已安装文档定义，陈旧副本会静默改变结果，因此必须能被命令发现（见 `docs/GOALS.md` 的接口约束）。比较内容摘要而不只是版本，能覆盖同一版本/commit 下重建出的不同技能文本（dirty worktree），这正是技能循环的日常场景；未安装技能是合法状态，不能报 error。
- 涉及代码: `cmd/vmake/skill_cmd.go`, `cmd/vmake/doctor_cmd.go`, `cmd/vmake/skill_cmd_test.go`, `cmd/vmake/skills/vmake/SKILL.md`, `cmd/vmake/skills/vmake/references/platforms.md`, `docs/AI_INSTALL_GUIDE.md`。

## DD-36: 深化模块边界——统一图展开、无头配置编辑器、构建后生命周期与独立报告/包目录

- 状态: accepted
- 背景: `cmd/vmake/` 曾聚集了大量领域编排与状态转换逻辑：(1) `pkg/build.NewBuildGraph` 与 `pkg/pipeline.BuildUnifiedGraph` 各自实现了一套目标/包边展开，且 `CheckProjectDeps` 为绕过未物化包在内存中伪造 `TargetVoid` 再二次构图；(2) `pkg/tui.Model`、`cmd/vmake/config.go`、`cmd/vmake/project.go` 与 `pkg/pipeline` 各自维护选项默认值/芯片联动/稀疏持久化与 `-s` 覆盖校验（且临时 `-s` 漏检了 `@local` 的 `internal/` 可见性）；(3) `OnInstall`/`OnClean`/安装清单（`install/manifest.json`）/测试执行（`vmake test`）散落在 `cmd/vmake/`，其中 `OnInstall`/`OnClean` 未经过 `visibleGlobalOptionsFor` 过滤，`vmake manifest checkout` 对本地 `SetGit` 包直接对宿主工程仓库执行 `git checkout`；(4) 构建报告（`vmake-build.json`/`vmake-test.json`）与包目录（`pkg search`/`pkg show`）绑在 `package main` 内，`loadPkgShow` 对未链接缓存的 native 包误传了 `mgr.Path(repoName)`。
- 决策:
  1. **统一图展开引擎（`pkg/build/graph.go`）**：保留严格模式的 `NewBuildGraph` 并新增审计模式的 `AuditBuildGraph(targets, pkgMeta, opts ...GraphAuditOptions)`，通过 `PkgBuildMeta.Unmaterialized` 容忍未物化包并填充 `DeclaredDepPkgs`，`CheckProjectDeps` 直接将 `g.buildGraph` 传给 `CheckExistingBuildBoundaries`；修复 `CollectSubGraphPackages` 中 `meta.Deps` 的子包短名解析。
  2. **无头配置编辑器与统一变更入口（`pkg/config` + `pkg/pipeline` + `pkg/api`）**：`pkg/config` 提供无 BubbleTea 依赖的 `*config.Editor`、`CommitSnapshot`、`SetOption`/`UnsetOption`/`ParseSetFlag`/`ParseUnsetFlag`/`CoerceOptionValue`；`pkg/tui.Model` 委托给 `*config.Editor`；`pkg/api` 提供宿主专用的 `BindEnsureKConfigPreset` 与 `BindChipTargetsApply`；`pkg/pipeline.MutateConfig` 统一持久与临时 `-s`/`-u` 变更并一致执行 `@local` `internal/` 可见性检查。
  3. **构建后生命周期下沉（`pkg/pipeline`）**：`RunInstall`/`ValidateInstallPrefix`、`LoadInstallManifest`/`WriteManifest`/`PinManifestRemotes`/`ImportManifestIntoLock`/`CheckoutManifest`/`CheckoutManifestLocals`/`CheckoutManifestLocalEntry`/`CheckoutManifestRemoteEntry`（只读加载本地 `build.go` 识别 `SetGit` 并物化托管源树）、`RunClean`/`CleanPackagesInspection`/`ExecuteCleanHooks`（统一使用 `visibleGlobalOptionsFor`）与 `ValidateTestProject`/`RunAllTests` 全部收归 `pkg/pipeline`。
  4. **独立构建报告（`pkg/buildreport`）与包目录（`pkg/catalog`）**：`pkg/buildreport` 承接 schemaVersion 2 的 `vmake-build.json`/`vmake-test.json` 与 GNU/脚本诊断提取（显式传入 `configSelector`）；`pkg/catalog` 承接只读包目录检索与展示，通过 `BuildPackageSource` 统一 registry/native 脚本源构造并修复缓存 native 包的根目录；`doctor` 的首选工具链探测改走 `openProject` + `inspectionScope`。
  5. **Git 传输内缝（`pkg/repo`）**：在 `SourceManager` 内部引入未导出的 `gitTransport` 函数回调结构体（默认 CLI 回调 + 故障注入测试回调），将版本/缓存解析（`resolveTargetRevision`）与工作树检出（`materialize`）解耦，保持 `SourceManager` 导出 API 不变。
- 修订（2026-11-24）: 两个构造器的**节点数据**已实测等价（同一 targets 下 `Nodes`、`Deps`/`DirectDeps`/`DeclaredDepPkgs`、`Order` 全部相同），由 `pkg/build/graph_equivalence_test.go:TestStrictAndAuditAgreeOnNodes` 钉住。但两者的**错误契约不可互相投影**，因此不合并遍历：严格模式返回首个解析错误（`dependency not found: <dep>`、`resolvePackageRef` 的 `circular dependency detected`，`pkg:target` 缺失时带 `subPackageHint`），审计模式输出 issue 文案（`(from <target>)`/`(resolved: <target>)`，target 缺失时还会多报一个包环，包环的 reason 文案也不同）。共享部分已收敛为 `declaredTargetDeps`（`UseRequires` 展开）、`visibilityPathFor`/`checkInternalVisibility` 与 `packageCycleError`；严格模式继续校验声明边（raw target deps、`OnRequire` meta deps、subgraphs），审计模式继续报告展开边。
- 涉及代码: `pkg/build/graph.go`, `pkg/build/subgraph.go`, `pkg/config/editor.go`, `pkg/config/store.go`, `pkg/tui/model.go`, `pkg/tui/app.go`, `pkg/api/bind.go`, `pkg/pipeline/config_mutate.go`, `pkg/pipeline/install.go`, `pkg/pipeline/manifest.go`, `pkg/pipeline/clean.go`, `pkg/pipeline/test_runner.go`, `pkg/buildreport/`, `pkg/catalog/`, `pkg/repo/source.go`, `cmd/vmake/`。

## DD-37: 构建目录名是身份摘要的短投影

- 状态: accepted
- 背景: 远程包输出目录曾为 `out/<sha256(member)>/<buildKey>`，两级各 64 位十六进制、共 129 字符。CMake 在构建目录内还会生成 60–90 字符的 try_compile/install 子路径，Windows 的 250/260 字符上限会被突破，而且失败发生在最深的叶子文件上，离决定目录的地方很远。
- 决策: 目录名与身份分离。完整 `BuildKey` 仍是身份（`PackageLayout.BuildKey`、`vmake inspect`），目录名只取前 16 位十六进制（`build.KeyDir`，64 bit）；member 目录改为可移植短名（`build.MemberDir`：空 member 为 `root`，其余清洗路径分隔符与 Windows 非法字符、截断 24 字符并追加 `sha256(member)[:16]` 保证唯一）。`pkg/pipeline.layoutFor` 对本地包用 `build.BuildPath`、对远程包用 `build.MemberDir`/`build.KeyDir` 生成目录，`build.PkgInfo.EffectiveBuildDir`/`OutputPath` 复用同一组投影，`PackageLayout` 仍是目录的唯一来源。构建目录内部同样是投影：`build/object/<objDir>`（`sha256(target, 源码相对路径)`）、`state/<name>.link.json` 和 `state/rsp/rsp-<sha256[:16]>.rsp`（Windows GNU 参数缓存）的文件名取摘要前 16 位（`build.digestDir`），对象文件名仍保留源码 basename。不 bump `layoutFormatVersion`（身份未变），旧目录由 `vmake clean --all` 回收；Windows 路径预算由 `TestRemoteLayoutFitsWindowsPathBudget` 钉住。
- 理由: 构建目录名只需要在同一个 `out/` 下唯一，不需要可校验；把 129 字符的身份编码从路径里拿掉，CMake、链接器和 install 步骤都重新拿到足够余量，而所有调用方无需改动。保留完整键作为身份，避免轮换目录的同时破坏 inspect/lock 的推导。
- 涉及代码: `pkg/build/paths.go`, `pkg/build/key.go`, `pkg/build/scheduler_resolve.go`, `pkg/build/signature.go`, `pkg/build/response.go`, `pkg/build/args.go`, `pkg/build/command_paths.go`, `pkg/pipeline/layout.go`, `pkg/build/paths_test.go`, `pkg/build/scheduler_test.go`, `pkg/pipeline/layout_test.go`, `docs/VMAKE_HOME.md`。

## DD-38: 增量动作的产物与构建链输入按 stat 身份校验

- 状态: accepted
- 背景: 每个 up-to-date 检查都会把动作记录里的所有输入和产物整份重读做 SHA-256：100 TU、每 TU 1MB object 的原生 target，no-op 构建 90.6ms 中有约 70ms 花在重读 object 与归档；100MB 产物在 Linux 上约 133ms（Go SHA-NI 约 1.5GB/s），Windows 叠加 Defender 扫描后按倍数放大。链接动作还会把该 target 的全部 object 当输入再哈希一次；CMake wrapper 的 prebuilt 库同理。这是“Windows 增量构建慢，CMake 第三方包尤其明显”的主要 vmake 侧来源。
- 决策: `fileState` 增加 `Size`/`ModTime`；`plannedAction` 增加 `StatInputs`（链接动作的 object、`DepArtifacts`，以及位于本包 build/install 目录内的 prebuilt 源）。动作记录中用户输入（源文件、头文件、脚本、`AddPostLinkDeps`、仓库内 vendored prebuilt）继续按内容哈希；`plannedAction.StatInputs` 与动作输出（object、`.d`、归档/ELF、post-link 产物）只比较 size/mtime/mode，不再读取内容。`publishTarget` 在目标动作 up-to-date 且暂存副本已存在时跳过内容对比；动作记录保存后的 output 缓存失效保留。`actionRecordVersion` 提升为 4，旧记录一次性失效重建。同一构建内重复出现的路径只解析一次：`pathCanonCache.realPath` 缓存 header 的真实路径，`internal/cmake` 的 cmake 可执行文件摘要按 stat 身份缓存，`CMakeBuild`/`CMakeInstall` 只解析路径，configure 写 stamp 复用同一路径不再读取二进制。
- 理由: 产物由 vmake 自己产生，外部改写必然改变 size 或 mtime；用 stat 身份判断缺失/改写足以保持正确性，校验成本从“与产物总大小成正比”降为每文件一次 stat。用户文件保留内容哈希，仍能发现同尺寸同 mtime 的内容替换。
- 取舍: 产物与构建链输入若被同尺寸且同 mtime 地替换不会被发现（与现有 `fileHashCache` 在单次构建内的跳过同级别）；`touch` 产物会保守触发重建；v4 升级后的第一次构建是全量重建。实测 no-op（Linux）：50×1MB object 90.6ms → 22.9ms，100 TU 原生 target 35.1ms → 27.0ms，缓存 CMake configure 44.3ms → 30.7ms。
- 涉及代码: `pkg/build/signature.go`, `pkg/build/action_plan.go`, `pkg/build/link_action.go`, `pkg/build/header_check.go`, `internal/cmake/incremental.go`, `internal/cmake/engine.go`, `AGENTS.md`, `docs/ARCHITECTURE.md`, `docs/WINDOWS_VALIDATION.md`, `test_windows/bench_build.py`。

## DD-39: 项目模型只有一个载体，`inspect`/`query` 只做编码

- 状态: accepted
- 背景: `cmd/vmake` 把 `inspect` 的 JSON 文档当内部模型用：`buildInspectData` 用 124 行造出 75 个字段，而 `query`/`query targets` 只读其中 4 个；源码目录回退、包类型、远端版本/commit、选项类型投影、目标事实（output/installPath/deps/installs）、诊断文案在 `cmd` 与 `pkg/pipeline` 之间各写一遍，`inspectSourceReference` 还重编码了 `resolver.LockQuery` 的优先级；DD-26/DD-31 留下的 `PackageView` 投影只有一个消费者。结果是理解"项目模型里有什么"必须同时在两个包里读。
- 决策:
  1. `pkg/pipeline.Inspection` 是项目模型的唯一载体，事实在构造时一次算好：信封事实（`Root`/`ConfigFile`/`ConfigSHA256`/`CompileDatabase`/`BuildReport`/`TestReport`/`InstallPrefix`/`InstallManifest` 及三个 `*Present`、`Overrides`）、每包 `PackageFacts`、`Diagnostics`、`PackageTargets`、`PackageInstalls`。新增 `Packages()`（按名排序，`docs/INSPECT.md` 的契约）、`Order()`（resolver 顺序，树渲染用）、`Facts`/`Diagnostics`/`Targets`/`Installs`/`GraphRoots`/`GraphEdges`/`PackageDeps`/`GraphError`/`GlobalOptionDefs`，以及 `PackageView.OptionValues(profile)`/`OptionDefs()`/`SourceDir()`。每包事实与诊断只在 `InspectOptions.Facts` 置位时计算：`inspect`/`query`/`query targets` 与 `check-symbols` 通过巡检前导打开该位（`check-symbols` 需要用 `TargetFacts.OutputPath`，而声明目标本来就是它的原有开销），`clean`、`deps *` 保持不计算。
  2. 统一图仍由命令层构图，`AttachGraph(ctx, graph, err)` 用 `graph.Facts()` 快照答案面（DD-29）并据 `BuildContext` 算出目标事实后，模型只保留 `GraphFacts`，不保留 `*UnifiedGraph`。`BuildUnifiedGraph` 没有自带 `scriptcall.Recover`，把它挪进 `InspectWithOptions` 的 recover 作用域会把 panic 从"命令层诊断"变成 `inspectionStageError`，改变 `status` 与退出码；构图失败仍按原位映射为 `VMAKE_E_INSPECT` 诊断。
  3. `cmd/vmake` 只保留图挂载与编码：`attachInspectionGraph` = 构图 + `AttachGraph`，`buildInspectData` = 它 + `encodeInspect`，`encodeInspect` 只读模型、不再接触 `RuntimeContext`；`query`/`query targets` 直接读模型访问器渲染（不再构造 inspect JSON 后回读）。`PackageFacts.Deps` 保留 resolver 声明顺序（`query` 树按它渲染），JSON 的 `deps` 由编码器排序；`Inspection.PackageDeps` 是图派生边表。字段名与 `omitempty` 仍归 `cmd`（`docs/INSPECT.md` 是它要满足的契约）。
  4. 选项值投影只有一处实现：`pkg/pipeline.OptionProjection.Project(profile)`，两档 `OptionProfileInspect`/`OptionProfileQuery` 逐条固化既有差异（缺值跳过、全局项过滤、`MergeGlobals`、默认值填充、排序、保留类型 vs 拍平文本）。Query 档先克隆值/定义两张 map，因为 `api.ConfigAccessor.MergeGlobals` 会就地改写它们。
- 理由: 同一概念只有一个载体，渲染器不再重算事实；新增一个模型字段只改编码器；`inspect` 输出逐字节不变——`01_simple_c`、`02_with_config`、`03_multi_target`、`05_conditional`、`15_subgraph_siblings`、`18_config_header`、`19_config_defines`、`20_config_propagate`、`21_root_package` 共 9 个 fixture 实测 stdout、stderr、退出码一致。
  5. 版本/commit 的**优先级**改由 `resolver.LockQuery`（`Decide`/`LockEntry`）承担，展示映射（`0.0.0-local`、pin 只显示版本）留在 `pkg/pipeline`；实测输出逐字节不变。
- 非目标: `packages[].version`/`commit` 的取值与缺失源码告警里的版本预测保持原样到 2.0。`docs/INSPECT.md:288` 把它定义为 "project pin, lockfile, or native selection"，`inspectRemoteMetadata` 正是这个规则；改用 `LockQuery.Decide` 会改变已文档化字段的取值，DD-34 在 1.x 内禁止改语义。缺失源码告警里的版本预测（`sourceReference`）同理不动，其文案被三处测试钉住。
- 涉及代码: `pkg/pipeline/inspect.go`, `pkg/pipeline/inspect_model.go`, `pkg/pipeline/option_values.go`, `cmd/vmake/inspect_cmd.go`, `cmd/vmake/inspect_session.go`, `cmd/vmake/query_cmd.go`, `cmd/vmake/inspect_cmd_test.go`, `docs/INSPECT.md`, `GLOSSARY.md`。

## 已知限制 / 待议

以下问题已被识别但尚未决策，修改前需单独讨论：

1. **本地项目无子包概念**：本地嵌套 build.go 被 `Scan` 收为顶层裸名包（目录 basename）；重名现在报错（DD-33）。
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
