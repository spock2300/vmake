# ~/.vmake 目录结构

`~/.vmake` 是 vmake 的全局数据目录，存储包仓库索引、扩展、工具链和内容寻址缓存。第三方包的源码和构建产物实际存储在全局缓存 `~/.vmake/cache/` 中，项目本地的 `vmake_deps/` 只保存指向缓存的符号链接。

## 目录总览

```
~/.vmake/
├── extensions/                    # 扩展仓库（git clone）
│   └── <repo-name>/
│       ├── <plugin-name>/         # 插件目录
│       │   ├── plugin.json        # 插件元信息
│       │   └── src/main.go        # 插件源码
│       ├── <toolchain-name>/      # 工具链声明
│       │   └── toolchain.json     # 工具链定义
│       └── assets/toolchains/     # 工具链压缩包（可选，Git LFS）
│           └── *.tar.gz           # 工具链二进制包
├── toolchains/                    # 已安装的交叉编译工具链
│   └── <name>-<version>/          # 工具链安装目录
├── cache/                         # 内容寻址缓存（VMAKE_CACHE 可覆盖根路径）
│   ├── v2/
│   │   ├── <repo>/<pkg>/<version>/
│   │   │   ├── src/
│   │   │   └── out/<sha256(member)>/<buildKey>/
│   │   │       ├── build/
│   │   │       ├── install/
│   │   │       └── work/repo/
│   │   ├── <repo>/<pkg>/{_refs,_head}/
│   │   └── _localgit/<sha256(url)>/
│   │       ├── src/
│   │       └── commits/<commit>/src/
│   └── _locks/
├── config.json                    # 全局配置（trustedRepos 远程脚本信任）
└── repos/                         # 包仓库索引（git clone）
    └── <repo>/
        └── packages/
            └── <first-char>/
                └── <pkg>/
                    └── build.go
```

旧版 `~/.vmake/sources` 目录会在升级后首次运行时自动删除，过期的项目 `vmake_deps/` 也会一并重建（布局标记 `.vmake/layout`，见 `cmd/vmake/storage.go`）。

## repos/

通过 `git clone` 克隆的包仓库，包含 `build.go` 形式的包定义。

路径规则：`repos/<repo>/packages/<first-char>/<pkg>/build.go`

`first-char` 是包名首字母（小写），用于避免单目录下文件过多。

源码：`pkg/repo/manager.go`（嵌入 `*gitstore.Store`）
CLI：`vmake repo add|remove|list|update|trust|untrust`

## cache/

全局内容寻址缓存，存储远程包的源码和构建产物，所有项目共享。`VMAKE_CACHE` 环境变量可覆盖缓存根路径（`cmd/vmake/paths.go` `getCacheDir`）。

- `v2/<repo>/<pkg>/<version>/src/` — 不可变 source seed，以临时目录物化并原子发布
- `v2/<repo>/<pkg>/<version>/out/<sha256(member)>/<buildKey>/` — 每个包成员与构建键的 build、install 和可写 work/repo 工作区；native 子包使用各自成员目录
- `v2/<repo>/<pkg>/_refs/`、`_head/` — tag 列表与更新使用的可变克隆
- `v2/_localgit/<sha256(url)>/commits/<commit>/src/` — 本地 SetGit 的提交级 source seed；可写副本位于该本地包的 BuildDir/work/src
- `_locks/` — 生命周期、owner 和物化锁；锁文件位于缓存内容目录之外，清理时不删除

补丁与外部构建写入工作区。source seed 不可变是受信任脚本需要遵守的契约，不是文件权限沙箱。初始物化以临时目录完成并原子发布；后续外部构建失败可能留下部分产物，下一构建会话重新执行回调。

项目操作先取得 `project/.vmake/_locks/project.lock`，构建再持有 cache lifecycle 共享锁，清理/更新持独占锁。执行前固定 owner 集合并按排序取得 owner 锁，声明、同步子图、构建和安装复用同一会话锁，避免其他构建读取半完成输出。

源码：`pkg/repo/source.go`、`pkg/repo/workspace.go`、`internal/storage/`

## config.json

全局配置文件，记录远程仓库脚本信任状态：

```json
{
  "trustedRepos": ["official"],
  "trustedCommits": { "official": "<commit>" }
}
```

由 `vmake repo trust|untrust <name>` 管理；`VMAKE_TRUST_ALL=1` 可跳过信任检查（CI 场景）。

源码：`cmd/vmake/trust.go`

## extensions/

扩展仓库，包含 CLI 插件和可选的工具链资源。

```
extensions/<repo-name>/
├── <plugin-name/>
│   ├── plugin.json              # 插件元信息
│   └── src/main.go              # 插件入口
├── <toolchain-name/>
│   └── toolchain.json           # 工具链定义
└── assets/
    └── toolchains/
        └── *.tar.gz             # 工具链压缩包（Git LFS）
```

**plugin.json 结构**：
```json
{
  "name": "tc",
  "version": "1.0.0",
  "description": "Toolchain management",
  "entry": "src/main.go",
  "enabled": true
}
```

源码：`pkg/plugin/manager.go`（嵌入 `*gitstore.Store`）
CLI：`vmake ext add|remove|list|update`

插件源码由 yaegi 解释器动态加载（`extensions/<repo>/<plugin-name>/src/main.go`），无需编译，不产生 `.so` 文件。

仓库中的 `toolchain.json` 由 vmake 自身扫描注册，不需要插件参与；只提供编译器的仓库可以不含任何插件代码。

## toolchains/

已安装的交叉编译工具链，由扩展声明后自动下载或手动安装。

```
toolchains/<host-os>/<host-arch>/<name>/<version>/
├── bin/
│   ├── <prefix>gcc
│   ├── <prefix>g++
│   └── ...
├── lib/
└── <sysroot>/
```

工具链由 `toolchain.json` 声明，vmake 启动时扫描扩展仓库子目录自动注册，首次使用时自动下载安装。

源码：`pkg/toolchain/discovery.go` (`Manager.RegisterRepo`)、`pkg/toolchain/install.go` (`Install`)

## 项目本地目录（vmake_deps/）

`vmake_deps/` 是指向全局缓存的符号链接目录（symlink farm）：源码和构建产物实际存储在 `~/.vmake/cache/` 中，`vmake_deps/` 只保存链接，让各项目拥有独立的依赖视图。

```
vmake_deps/
└── <repo>/<pkg>/                  # Registry 和 Native 包使用相同结构
    ├── src -> ~/.vmake/cache/v2/<repo>/<pkg>/<version>/src
    ├── out -> ~/.vmake/cache/v2/<repo>/<pkg>/<version>/out
    │   └── <sha256(member)>/<buildKey>/
    │       ├── build/
    │       ├── install/
    │       └── work/repo/
    └── _members/<sha256(member)>/src -> Native 子包的实际源码工作区
```

`buildKey` 由格式版本、工具链身份（含工具内容）、构建模式、选项组合，以及版本号、commit、有序全局 flags、补丁集和 buildscript 哈希共同生成（`pkg/build/key.go` `BuildKey`）。src/out 符号链接同一时刻只指向一个版本目录，但缓存中可同时保留多个版本。

Native 子包的链接使用 `_members/<sha256(member)>/src`，其中 `member` 是使用 `/` 分隔的仓库相对路径。子包名不会直接成为链接目录，因此 `src`、`out` 和嵌套子包不会穿过父包的源码或输出链接。

`vmake_deps/` 在首次构建时自动添加到项目根目录的 `.gitignore`。

源码：`cmd/vmake/paths.go` (`getDepsDir`, `findProjectDir`), `pkg/repo/source.go`, `pkg/repo/installer.go`
CLI：`vmake pkg list|search|clean|update`

## 项目目录

每个项目可有独立的配置文件，存储在 `.vmake/config.json`。

```
project/
├── build.go                       # 项目构建脚本
├── .vmake/
│   ├── config.json                # 项目配置
│   └── vmake.lock                 # 锁定远程包版本+commit（vmake lock update 重新解析）
├── vmake_deps/                    # 第三方包符号链接（自动生成，已 gitignore）
│   └── <repo>/<pkg>/
│       ├── src -> ~/.vmake/cache/.../src
│       └── out -> ~/.vmake/cache/.../out
├── install/                       # 安装输出（--prefix 默认 ./install）
└── build/                         # 本地包构建输出
    ├── compile_commands.json      # LSP 编译数据库
    └── <buildKey>/                # 64 位十六进制哈希目录
        ├── object/<hash>/foo.o    # hash 隔离 target 与源码路径，保留源码文件名
        └── <target>               # 最终产物
```

项目配置结构（`pkg/config/store.go`）：

```json
{
  "version": "1",
  "global": {
    "toolchain": "host",
    "mode": "debug",
    "options": { "ssl": true }
  },
  "entries": {
    "myproject": {
      "options": { "verbose": false }
    },
    "official/zlib": {
      "version": "1.3.1",
      "options": { "shared": false },
      "kconfig": "",
      "selected_preset": ""
    }
  }
}
```
