# 08 — 收掉 DD-36 留下的转发层

**Status:** ready-for-agent
**Type:** task
**Card:** 7
**Blocked by:** 01, 07

## 为什么

DD-36 把安装、清理、清单、测试生命周期推进了 `pkg/pipeline`，但 `cmd/vmake` 留下了 27 个"单调用转发"函数（函数体只有一行 `return pipeline.X(...)`）。按调用点统计：

- **12 个零生产调用点**，其中 2 个完全死代码（`manifestProjectDir`、`newPkgRef`）；
- 10 个只被 `package main` 的测试调用：`resolveManifestEntryPath`(3)、`installOnePackage`(2)、`writeManifest`(1)、`remainingBuildDirectories`(1)、`checkoutLocal`(4)、`loadPackageScript`(1)、`sortedPackageVersions`(1)、`addRequiresSnippet`(1)、`validateTestExecution`(2)、`boundedTestOutput`(1)；
- 其余 15 个有 1–14 个生产调用点。

它们存在的原因是测试无法从 `package main` 之外到达 pipeline，以及若干转发要注入全局（`installOnePackage` 注入 `prefixFlag`、`getRepoManager` 注入 `getReposDir()`、`beginBuildReport` 注入 `configSelector`）。

## 做什么

1. 删除 12 个零生产调用点的转发；纯改名型的直接在调用点展开。
2. 组合型（注入全局的）在 01 之后重新判断：多数应随全局消失而消失，而不是被内联。
3. 10 个"只为测试存在"的转发，其测试搬进 `pkg/pipeline`（行为住在哪，测试就住在哪）。只有确实在测命令接线的，才在 01 之后改写成 `run()` 级测试。
4. 类型别名（`installManifest`、`installManifestEntry`、`installOwnershipMarker`）一并评估：若只剩测试使用，测试搬走后删除。

## 验收

- 通用 6 条。
- `install/manifest.json`、`vmake clean`、`vmake test` 的行为与输出逐字节相同。
- 死代码检查：`grep` 每个被删函数名在 `cmd/` 与 `pkg/` 内无残留。
- `cmd/vmake` 内"单行 `return <pkg>.X(...)`"的函数数量从 27 降到 0（组合型若保留，必须至少多一个参数或做一件事）。

## 涉及文件

`cmd/vmake/build_install.go`、`build_cmd.go`、`clean.go`、`pkg_discover.go`、`test_cmd.go`、`build_report.go`、`helpers.go`、`inspect_session.go`、`manifest_cmd.go`、`root.go`，以及对应测试。
## 可执行计划（planner 复核版，取代上面的估算）

**重新清点（AST，非 grep）**：单行转发 **28 个**（26 个 `return <pkg.X>` + 2 个 void），不是 27 也不是 31。构成：17 pipeline、5 catalog、2 buildreport、1 repo、1 plugin、2 void。零生产调用点：**10 个返回 + 1 个 void**；有生产调用点：17。

两个原始 ticket 的错误：`newPkgRef`（`helpers.go:89`）**不是死代码**——`manifest_cmd.go:81` 把它当函数值用；`manifestProjectDir` 是唯一完全死的。指标也要改：字面 grep 从 31 降到 **6**，那 6 个是 `return pipeline.ResolveParams` 的错误返回，不是转发。正确指标是"函数体只有一条 `return <项目包>.X(...)`"的函数数：**26 → 0**（含 2 个 void 则 28 → 0）。

| # | 改动 | 验证 | 期望 |
|---|---|---|---|
| S0 | 无编辑：基线构建 `/tmp/vmake-base`，在 `test_data/01_simple_c` 跑 `build --install`+`clean`、`03_multi_target` 跑 `test`，落盘输出；跑一次快照 | — | 仅 11/13/25 失败 |
| S1 | 删 `manifestProjectDir` + 8 个 pipeline 零生产转发 + `cleanAllRemoteOutputs`；新建 `pkg/pipeline/{manifest,install,clean,test_runner}_test.go`；删 `cmd/vmake/build_manifest_test.go`、`build_install_test.go` | build+vet+`go test ./cmd/vmake ./pkg/pipeline` | 绿；28→19 |
| S2 | 内联 9 个 pipeline 有生产调用点的转发（16 处）；把 `checkoutManifestLocals`/`validateInstallPrefix`/`cleanPackages`/`executeCleanHooks` 的测试并入 S1 文件；删 `installManifestEntry`/`installOwnershipMarker` 别名 | 同上 | 19→10 |
| S3 | 内联 2 个 buildreport 转发；给 `clean.go`/`distclean.go`/`pkg_cmd.go` 补 `buildreport` import | build+vet | 10→8 |
| S4 | 内联 `getRepoManager`/`getPluginManager`（24 生产 + 5 测试点） | build+vet+cmd 测试 | 8→6 |
| S5 | 内联 `removeIfExists` | build | 6→5 |
| S6 | **依赖 07**：搬 6 个 catalog 测试、删 5 个 catalog 转发（1 处内联） | build+catalog 测试 | 5→0 |
| S7 | 全量门禁 + 与 S0 字节对照 + 逐名 `grep -r <name> cmd/ pkg/` = 0 + AST 计数 = 0 | — | 全 IDENTICAL |

S1–S5 与 07 **无关**，可先落；只有 S6 依赖 07 的 cmd 迁移（同一文件 `pkg_discover.go` 会有文本冲突）。

**搬家清单（10 条，已核过无重名/无辅助函数冲突）**：`resolveManifestEntryPath`/`writeManifest`/`checkoutLocal` → `pkg/pipeline/manifest_test.go`；`installOnePackage`/`validateInstallPrefix` → `install_test.go`；`cleanPackages`/`executeCleanHooks`/`remainingBuildDirectories` → `clean_test.go`；`sortedPackageVersions`/`addRequiresSnippet`/`loadPackageScript` → `pkg/catalog/catalog_test.go`；`validateTestExecution`/`boundedTestOutput` → `pkg/pipeline/test_runner_test.go`。`bm` 里的测试体已经用 `RuntimeContext`/`BuildResult` 别名，因此近乎逐字搬。

**陷阱（已读代码确认）**：`buildTestOutputLimit`/`Marker` 仍被 `TestTestReportTruncatesLongOutput` 使用，只删 `boundedTestOutput`；`completion.go` 没有 import `catalog`，内联 `listNativePackagesAt` 时要补；删完会有 5 个未用 import（`context`@build_cmd、`api`@clean、`api`@test_cmd、`api/build/resolver`@build_install、`repo/plugin`@helpers）；`pkg/pipeline` 不 import `pkg/catalog`，所以 catalog 的测试必须留在 `pkg/catalog`；`writePkgTestFile` 只存在于 `pkg_discover_test.go`，搬 catalog 测试时要复制过去。

**已定决策**：接受修正后的指标（26→0）；`getRepoManager`/`getPluginManager` 一并内联（否则"零转发"不成立）；S6 等 07 落地后做。

## 进展（S3 + S5 已落地）

- 内联 `beginBuildReport`、`invalidateBuildReport`（buildreport 转发，1 + 3 个生产点），`clean.go`/`distclean.go`/`pkg_cmd.go` 补 `buildreport` import。
- 删除 `removeIfExists`（void 转发），`distclean.go` 三处直接调 `pipeline.RemoveIfExists`，补 `pipeline` import。
- 单行转发计数：**28 → 23**（AST 口径）。
- 验证：`go build`/`go vet` 干净，`TestClean*`/`TestDistclean`/`TestPkgClean*`/`TestBuildReport*` 全绿，`./cmd/vmake ./pkg/... ./internal/...` 无失败。
- 剩余按计划：S1（10 个零生产转发 + 4 个新测试文件 + 删 2 个测试文件）、S2（9 个 pipeline 转发内联 16 处）、S4（`getRepoManager`/`getPluginManager` 24 处）、S6（catalog 5 个，依赖 07 已就绪）。

## 进展（S6 部分：3 个有生产调用点的 catalog 转发）

- 内联 `searchNativeRepo`（`pkg_discover.go` 1 处）、`listNativePackagesAt`（`completion.go` 1 处，补 `catalog` import）、`pkgEntryMatches`（`pkg_discover.go` 1 处）。
- 测试同步改到真实目标：`pkg_discover_test.go` 的 `catalog.ListNativePackagesAt` / `catalog.EntryMatches` / `catalog.SearchNativeRepo(...)`（后者需要 `catalog.SearchOptions{}` 而不是 `pkgSearchOptions{}`——类型不同，编译器抓到了）。
- 单行转发计数：**23 → 21**。
- 剩余：S1（10 个零生产 + 4 个新测试文件 + 删 2 个测试文件，最大一块）、S2（9 个 pipeline 转发、16 处）、S4（`getRepoManager`/`getPluginManager`、24 处）、S6 余下（`sortedPackageVersions`/`addRequiresSnippet`，只被测试调用，需随测试搬去 `pkg/catalog`）。
- 验证：build/vet 干净，`TestPkg*`/`TestSearch*`/`TestShow*`/`TestCompletion*`/`TestNative*` 全绿，全量套件无失败。

## 进展（S4：两个存储根工厂）

- 删除 `getRepoManager()`/`getPluginManager()`，24 个生产点与 5 个测试点改为直接 `repo.NewRepoManager(getReposDir())` / `plugin.NewManager(vmakeDir)`。
- 连带的 import 收拾：`repo_cmd.go` 补 `pkg/repo`；`completion.go` 已有；`helpers.go`/`ext_cmd.go` 去掉变为多余的导入；`build_manifest_test.go`/`trust_test.go` 用的是 `repoPkg` 别名，需用 `repoPkg.NewRepoManager`。
- 单行转发计数：**21 → 19**（累计 28 → 19）。
- 验证：build/vet 干净，`TestTrust*`/`TestRepo*`/`TestExt*`/`TestCompletion*`/`TestPkg*` 全绿，全量套件无失败。
- 剩余：S1（10 个零生产转发 + 4 个新测试文件 + 删 2 个测试文件，最大且必须一次做完）、S2（9 个 pipeline 转发、16 处，依赖 S1）、S6 余（`sortedPackageVersions`/`addRequiresSnippet`，随测试搬去 `pkg/catalog`）。

## 进展（S6 余 + S1 的头两项）

- `sortedPackageVersions`/`addRequiresSnippet`：测试搬进 `pkg/catalog/catalog_test.go`（改为无条件名），两个转发删除。
- `validateTestExecution`/`boundedTestOutput`：新建 `pkg/pipeline/test_runner_test.go`，测试搬入并改用 `BuildTestOutputLimit`/`BuildTestOutputMarker`/`ValidateTestExecution`/`BoundedTestOutput`；两个转发删除。`cmd` 侧 `TestTestReportTruncatesLongOutput` 仍用 cmd 的常量（保留）。
- 连带的未用 import：`test_cmd.go` 去 `api`；`platform_commands_test.go` 去 `api`。
- 单行转发计数：**19 → 15**（累计 28 → 15）。
- 验证：build/vet 干净，`./cmd/vmake ./pkg/... ./internal/...` 全绿。
- 剩余：S1 其余 6 个（`manifestProjectDir` 死代码、`resolveManifestEntryPath`、`installOnePackage`、`writeManifest`、`remainingBuildDirectories`、`checkoutLocal`、`cleanAllRemoteOutputs`）、S2（9 个 pipeline 转发、16 处）。

## 进展（`manifestProjectDir` 死代码）

删除唯一完全无引用的转发 `manifestProjectDir`（`build_cmd.go`）。单行转发计数 **15 → 14**（累计 28 → 14）。build/vet 与全量套件绿。

### 下一步的实测情况（重要）

S1 剩下的 7 个不是"搬测试"那么机械。实际内容是：

- `build_install_test.go`（245 行，4 个测试）与 `build_manifest_test.go`（230 行，7 个测试）都是**端到端 CLI 测试**——它们通过 `run()`/子进程建真实工程再断言，而不是直接调 `pipeline.InstallOnePackage`。搬到 `pkg/pipeline` 等于把 CLI 测试改写成流水线单测，plan-08 的映射里已把其中 3 条标为 "rewrite"。
- 因此 S1 的正确切法不是"移动文件"，而是**先决定每个测试的归宿**：(a) 保留为命令测试但内联掉转发器（零搬家，S2 与它合并）；(b) 重写为 `pkg/pipeline` 单测（覆盖更细但要重写 475 行）。两种都不与"逐字搬"等价。
- 建议下一轮先做 S2 的"内联"部分（9 个 pipeline 转发、16 处）并同步把 `cmd` 测试改为直接调 `pipeline.*`——这不会删除任何测试，且让 S1 的删除变成"改测试引用名"而不是"搬家"。这是本轮验证出来的更安全顺序。

## 完成（S1/S2/S6 全部落地）

按上一轮验证出的更安全顺序执行：**先内联、再改测试引用**，因此没有搬移任何测试文件——475 行的端到端 CLI 测试原地保留，只把调用目标从转发器换成 `pipeline.*`。

| 删除的转发 | 处理 |
|---|---|
| `loadInstallManifest`、`importManifestIntoLock`、`checkoutManifestLocals`、`resolveManifestEntryPath` | 调用点改 `pipeline.*`；`importManifestIntoLock` 的 `findProjectDir()`/`getLockfilePath()` 实参回到调用点 |
| `installOnePackage`、`validateInstallPrefix`、`writeManifest` | 同上；`installOnePackage` 末尾的 `prefixFlag` 回到调用点 |
| `cleanPackages`、`remainingBuildDirectories`、`collectCleanEntries`、`executeCleanHooks`、`cleanAllRemoteOutputs` | 同上（含 void 那个） |
| `runBuildPhase` | 调用点改 `pipeline.RunBuild` |
| `checkoutLocal` | 调用点改 `pipeline.CheckoutManifestLocalEntry(project, nil, entry)` |
| `manifestProjectDir` | 无引用，直接删 |

**指标**：函数体只有一条 `return <项目包>.X(...)` 的函数 **28 → 1**。剩下的 `newPkgRef` 不是转发——它组合 `api.NewPackage().SetRepo().SetName()`，且被当作函数值使用（`manifest_cmd.go:81`），保留。

**保留的组合型助手**（不止一条语句，不符合转发定义）：`executeInstall`（组装 `InstallOptions`）、`installStorageRoots`、`catalogEnv`、`loadPackageScript`（包 quiet 作用域）、`newPkgRef`。

验证：build/vet 干净；`./cmd/vmake`、`./pkg/... ./internal/...` 全绿；快照 6 个夹具 PASS（`build`/`install` 路径改动的直接覆盖）。
