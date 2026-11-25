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

## 实现记录（部分落地）

已完成这张卡的核心目标——**依赖切断与效果闭包移除**：

- `pkg/catalog` **不再导入 `pkg/pipeline`**（验收 grep 为 0）。它唯一用到的 `pipeline.LoadInstallManifest` 移到 `cmd/vmake/pkg_discover.go`：调用方读好安装清单，把 `installed map[string]bool` 传给 `catalog.NewStatusReaderAt`。
- `Env` 增加两个字段：`LoadScript func(buildscript.Source) (*api.Package, error)` 与 `Installed`。`Env.loadScript` 优先用注入的加载器。
- `quietScope func() func()` 参数从 `LoadPackageScript`/`SearchRegistryRepo`/`SearchNativeRepo`/`ShowPackage` 全部删除（验收 grep 为 0）。静音改由 `cmd` 的 `loadPackageScript` 用 `quietpkg.Enter(quietpkg.Script)` 包裹——这正是 03 落地后才可能的形状。
- 测试：新增 `TestEnvLoadScriptPrefersInjectedLoader`（用假加载器，不需要 yaegi）。

未完成（明确记录，下一轮继续）：

1. **`Open(env) *Catalog` + `Search`/`Show`/`Script` 方法形态**尚未引入；现在仍是包级函数 + `Env` 首参。
2. 验收里"测试覆盖 registry/native 的 `Search`、`Show`、`Script` 失败路径"只做到 `loadScript`。尝试给 `ShowPackage` 写空 `Env` 测试时发现它会 nil-deref（`catalog.go:458`，`Env.RepoManager` 为 nil）——这是既有行为，说明该函数仍假设调用方给全依赖，正是本卡想根除的东西；要真正可测，得先有 `Open(env)` 与可注入的仓库访问。

结论：依赖方向已经掰正（catalog 现在是叶子），`Search`/`Show` 的接口收敛留作本卡的剩余部分。
