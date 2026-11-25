# 07 — 包目录：解耦 pipeline，接口不再是调用方的效果

**Status:** ready-for-agent
**Type:** task
**Card:** 8
**Blocked by:** 03

## 为什么

`pkg/catalog` 是 595 行、22 个导出、**1 个测试**、30 行测试代码。原因是接口形状：

- 4 个入口里有 3 个要求调用方传入 `quietScope func() func()`；
- 入口参数多达 7 个（`BuildPackageSource(mgr, repoName, pkgName, buildGo, commit, native, linked)`）；
- `StatusReader` 由 `NewStatusReaderAt` 在包内读锁文件 + 安装清单构造，因此测试需要真实的 `.vmake/vmake.lock` 与 `install/manifest.json`；
- 为了让 `StatusReader` 能用，catalog 对 `pkg/pipeline` 的全部用法只有 `pipeline.LoadInstallManifest` 一个符号——却因此拖入 pipeline→{build, resolver, repo, toolchain, buildscript, config, lockfile}；
- `Search`/`Show` 还要真的解释执行 build.go 才能读到 description/license/requires。

## 做什么

1. `Env` 携带调用方已读好的数据：锁、安装清单（或已算好的 `Installed` 集合）、`*repo.SourceManager`，以及注入的加载器：
   `LoadScript func(buildscript.Source) (*api.Package, error)`。
2. `NewStatusReaderAt` 移出 catalog（到 `cmd/vmake`）；catalog 只剩一个由 `Env` 构造的 `StatusReader` 值。
3. catalog **不再导入 `pkg/pipeline`**（解耦后成为叶子包，只依赖 `repo`/`buildscript`/`config`/`api`）。
4. 删除全部 `quietScope` 参数：静音包在 `cmd` 注入的 `LoadScript` 里（catalog 对 `quiet` 一无所知）。
5. 接口收敛为 `catalog.Open(env) *Catalog` + 方法 `Search(opts)`、`Show(ref)`、`Script(src)`。

## 验收

- 通用 6 条；`pkg search` / `pkg show` 的 `--json` 与文本输出逐字节相同。
- 测试从 1 个增加到覆盖 `Search`（registry + native 两种仓库）、`Show`、`Script` 的失败路径；用假 `LoadScript` + `t.TempDir()` 里的假仓库，**不**需要 yaegi 真解释执行。
- `grep -rn 'pkg/pipeline' pkg/catalog/` 归零。
- `grep -rn 'quietScope' pkg/catalog/ cmd/vmake/` 归零。

## 涉及文件

`pkg/catalog/catalog.go`、`catalog_test.go`（重写）、`cmd/vmake/pkg_cmd.go`、`pkg_discover.go`。
