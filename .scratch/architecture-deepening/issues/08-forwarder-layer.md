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
