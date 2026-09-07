# 25_subpackage — 子包（Sub-Package）

验证 native 远程包内的嵌套 build.go 被识别为子包（`父包/相对路径`），覆盖：

- 外部包按全名引用子包（`AddRequires("subtest/mother/sub_a")`、`AddDeps("subtest/mother/sub_a:*")`）
- 子包之间用短名互引（sub_a 的 `ctx.AddRequires("sub_b")` 与 `AddDeps("sub_b:utils_b")`）
- `pkg:target` 与 `pkg:*` 两种依赖形式
- 子包版本跟随父包（lockfile 只记录 `subtest/mother`）

## 前置（一次性）

```sh
sh ./setup.sh
```

setup.sh 从 `.fixture-src/mother/` 构建本地 bare git 仓（`.fixture/mother.git`，tag v1.0.0），
并注册 native 仓库 `subtest`（URL 模板 `file://.../.fixture/{name}.git`，全局
`~/.vmake/repos/subtest`）。

## 运行

```sh
../../vmake build --install
./install/bin/app   # 输出 43（utils_b=21 → utils_a=42 + base=1）
```

修改 `.fixture-src/` 后需重建 fixture 并清理全局缓存：

```sh
rm -rf .fixture .vmake vmake_deps ~/.vmake/cache/subtest
sh ./setup.sh
```

注意：本测试未覆盖子包 `SetGit`（fixture URL 是机器相关绝对路径，写进 build.go 会破坏可移植性）。
