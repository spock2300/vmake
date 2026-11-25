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
