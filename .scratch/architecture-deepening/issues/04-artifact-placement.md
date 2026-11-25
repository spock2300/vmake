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
