# 04 — 一个放置值，不是两个

**Status:** ready-for-agent
**Type:** task
**Card:** 2
**Blocked by:** 无

## 为什么

DD-21 让 `build.PlacementFor` 成为唯一的放置值，但解析阶段仍在自行推导：

- `scheduler_resolve.go:434-436 getTargetOutputPath` 重新计算 `pkgInfo.OutputPath(targetFilename(kind, name, targetOS))`，与 `PlacementFor` 的 `OutputPath` 是同一推导。
- `scheduler_resolve.go:92-93` 把 PE 导入库规则内联成 `depOutput += ".a"`，而规则已经由 `linker.go:100 importLibraryPath` 表达；内联副本多了 `Prebuilt() == ""` 这个 guard，`PlacementFor` 没有——也就是说 `PlacementFor` 至今会给一个预编译 shared 目标报出并不存在的 `ImportLib`。

## 做什么

1. `PlacementFor` 在 `target.Prebuilt() != ""` 时不设 `ImportLib`（规则归位到唯一所有者）。
2. `getTargetOutputPath` 改为返回 `placementFor(...).OutputPath`，或直接在两个调用点（`:86`、`:219`）用 `placementFor`。
3. 解析阶段的 PE 拼接改为读 `placement.ImportLib`，删除内联 `+= ".a"`。
4. 不走 `targetOutcome`——那是另一张卡，与卡片 1 的改动面重叠。

## 验收

- 通用 6 条，包含 9 个夹具的 `inspect` 逐字节相同。
- 新增跨平台测试（linux 主机、`target_os=windows`）：预编译 shared 目标 → `ImportLib == ""`；非预编译 shared → `ImportLib == <dll>.a`；同一目标的 `OutputPath` 与旧推导字符串相等。
- `grep -n '"+ \".a\"" pkg/build` 归零。

## 涉及文件

`pkg/build/artifact.go`、`scheduler_resolve.go`、`linker.go`、`link_action.go`、`artifact_test.go`（新增）。

## 实现记录

- `PlacementFor` 现在只在 `target.Prebuilt() == ""` 时设 `ImportLib`；规则因此只有一处。
- 解析阶段的两个 `getTargetOutputPath` 调用点改为消费放置值；该函数保留为**纯投影**（一行 `PlacementFor(...).OutputPath`），因为 8 处测试直接用它，删掉只会制造 churn。
- 依赖扫描里那处内联 `depOutput += ".a"` 删除。改为：先取放置值，若 `ImportLib != ""` 则用 `importLibraryPath(depOutput, …)` 对**该依赖自己的输出基**套用同一条规则——不能直接用 `placement.ImportLib`，因为放置值里的导入库按包源目录解析，而依赖制品是在同伴输出旁边查找的（`TestSchedulerPackagePlatformArtifacts` 抓到了这个差异：相对基变成源目录绝对路径后 `dependency artifact missing`）。
- 新增 `TestArtifactPlacementPrebuiltSharedHasNoImportLibrary`：预编译 shared 无导入库、非预编译有 `.a`、两者 `OutputPath` 相同。
- `grep '"+ \".a\""' pkg/build` 为零：`.a` 只由 `importLibraryPath` 产生。

未做（按票）：不把解析阶段接进 `targetOutcome`。
