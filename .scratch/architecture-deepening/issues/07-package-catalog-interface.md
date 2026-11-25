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

## 第二段记录

- 引入 `catalog.Open(env) *Catalog` 与三个方法 `Search(repo, opts, statuses)`、`Show(repo, pkg, version)`、`Script(src)`；`Search` 依 `Env.repoIsNative` 分派 registry/native。这是薄门面：包级函数保留，现有调用方未迁。
- `ShowPackage` 在 `Env.RepoManager == nil` 时返回 error 而不是 nil-deref——原来它假设调用方喂齐依赖，这让任何"不完整 Env"测试都直接 panic。
- 新增两个测试：`TestCatalogScriptUsesTheInjectedLoader`（假加载器，错误透传）、`TestShowRejectsAnEnvironmentWithoutARepositoryManager`。

仍未做：`cmd/vmake` 的 18 处 `catalog.*` 调用迁到 `Open(env).*`；以及 registry/native 的 `Search` 覆盖（需要可注入的仓库访问，`repo.RepoManager` 目前只有真实目录实现）。
## 可执行计划（planner 复核版，取代上面所有估算）

探针结论：**"18 处调用点"是错的**。18 个 `catalog.` token 里只有 **2 处**是门面调用（`pkg_discover.go` 的 search 与 show）；其余是类型别名、`LoadPackageScript`（它本身就是被注入的 loader）、`NewStatusReaderAt`、`ListNativePackagesAt` 与纯辅助函数，必须留在包级。`GetNativeURL` 未被 catalog 使用。

| # | 改动 | 验证 | 期望 |
|---|---|---|---|
| 1 | `catalog.Env.Repo` 改为 6 方法接口 `RepoAccess`（`Exists`/`IsNative`/`ListPackages`/`FindPackageGo`/`Path`/`Lock`）+ `Env.Commit func` 回落；`ShowPackage`/`SearchRegistryRepo` 加 nil 守卫 | build+vet+`go test ./pkg/catalog ./cmd/vmake` | 编译通过，行为不变 |
| 2 | 4 处 `repo.GetCurrentCommit` 走 `env.commit` | 同上 | 同上 |
| 3 | `cmd` 两处真门面调用改 `catalog.Open(catalogEnv(mgr)).Search/Show`；`catalogEnv` 字段名 `RepoManager`→`Repo` | 字节对照 | 6/6 IDENTICAL |
| 4 | `pkg/catalog/catalog_test.go` 加假 `RepoAccess`：`TestSearchRegistryRepoMetadata`、`TestSearchNativeRepoFromState`、`TestSearchNativeRepoWithoutProject`、`TestShowRegistrySuccess`、表驱动 `TestShowFailures` | `go test ./pkg/catalog -count=1` | 无需 yaegi/git/网络 |
| 5 | DD-40 item 8 改写（接口例外、`NewStatusReaderAt` 留在 catalog）；本文件补完成记录 | grep + 快照 | `grep -rn 'pkg/pipeline\|quietScope\|RepoManager' pkg/catalog` = 0 |

**假实现的硬约束（实测）**：`flock.FileLock` 字段不可导出且 `Release` 对 nil 会 panic，所以假 `Lock` 必须在 `t.TempDir()` 里拿真锁；`catalogEnv` 是 eager 的（调用即执行 `findProjectDirSoft`，后者在 `projectOverride` 非法时 `fatalUsage` panic），所以假实现只能经 `Env` 字面量注入，不能经 cmd。

**字节对照（一次 bash 内完成，`/tmp` 每次调用会重置）**：把基线 commit `git archive` 到 `/tmp/vb/src` 并构建 `/tmp/vb/old`，与当前树构建的 `/tmp/vb/new` 对比 6 条命令（`pkg search --json`、`--details`、`--details --for-project`、`pkg show official/zlib --json`、`pkg show official/zlib`、`pkg show myrepo/libfoo --json`），必须无 diff。夹具用 `/tmp/vh/home/.vmake/repos` 下的假仓库 + 假 `state.json`，`HOME`/`VMAKE_CACHE` 必须指到 `/tmp/vh`。

**已定决策**：接受 6 方法接口（AGENTS.md "回调优先" 的显式例外，记入 DD-40）；`Env.Commit` 用函数字段（先例 `Env.LoadScript`）；`Env.Installed` 是死字段，保留不动；`Search` 内部**不做** pattern/repo 过滤（过滤在 cmd，重复过滤会改输出）。

**不在本卡**：把 `NewStatusReaderAt` 移出 catalog（DD-40 措辞问题）；linked-native `Show` 测试；快照基线更新。
