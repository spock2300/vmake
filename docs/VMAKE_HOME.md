# ~/.vmake 目录结构

`~/.vmake` 是 vmake 的全局数据目录，存储包仓库索引、扩展和工具链。远程包以及本地 `SetGit` 包的源码以**每个包一棵浅工作树**的形式物化在项目内的 `.vmake_deps/`（depth=1）；同一上游 URL 的对象另有一份跨工程共享的**内容寻址对象缓存** `~/.vmake/cache/git/`，commit 已缓存的请求从该缓存本地 depth=1 checkout，不再直接向上游重复下载。普通本地包（未 `SetGit`）不使用 `.vmake_deps/`，其源码和补丁直接作用在包目录。

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
│   └── <host-os>/<host-arch>/<name>/<version>/   # 工具链安装目录
├── cache/                         # 缓存根（VMAKE_CACHE 可覆盖根路径）
│   ├── git/                       # 共享内容寻址 git 对象缓存（VMAKE_GIT_CACHE=off 关闭）
│   │   └── <urlhash>.git/         # 每个上游 URL 一个 bare 仓库：objects/ + refs/vmake/<sha> + vmake-cache.json
│   └── _locks/                    # 生命周期锁、owner 锁、per-tree 与 per-cache-repo 锁
├── config.json                    # 全局配置（trustedRepos 远程脚本信任）
└── repos/                         # 包仓库索引（git clone）
    └── <repo>/
        └── packages/
            └── <first-char>/
                └── <pkg>/
                    └── build.go
```

旧版布局（`~/.vmake/sources`、`cache/v2`、`cache/_localgit`、`cache/mirrors`、项目 `vmake_deps/`）会在升级后首次执行加载项目配置/依赖的命令时清理（`loadResolveInputs` 中的 `cleanupLegacyStorage`；`doctor`、`pkg list`、`toolchain list`、`manifest show` 等不经过该路径的命令不会触发），依赖重新物化；不提供兼容迁移（布局标记 `.vmake/layout`，见 `cmd/vmake/storage.go`）。`cache/git/` 是当前布局，升级时不删除。

## repos/

通过 `git clone` 克隆的包仓库，包含 `build.go` 形式的包定义。

路径规则：`repos/<repo>/packages/<first-char>/<pkg>/build.go`

`first-char` 是包名首字母（小写），用于避免单目录下文件过多。

源码：`pkg/repo/manager.go`（嵌入 `*gitstore.Store`）
CLI：`vmake repo add|remove|list|update|trust|untrust`

## cache/

缓存根保存锁、生命周期状态与共享 git 对象缓存。`VMAKE_CACHE` 环境变量可覆盖根路径（`cmd/vmake/paths.go` `getCacheDir`）。

- `git/` — 共享内容寻址 git 对象缓存，每个上游 URL 一个 bare 仓库（`<urlhash>.git`，`urlhash = storage.OwnerKey(归一化 URL)`）。仓库只累积 vmake 实际 fetch 过的对象，用 `refs/vmake/<sha>` 锚点固定；commit 已缓存的请求再从该缓存本地 depth=1 checkout。设 `VMAKE_GIT_CACHE=off` 可完全关闭
- `_locks/` — 生命周期锁、owner 锁、per-tree 物化锁与 per-cache-repo 锁（`cache_<urlhash 前16位>.lock`）；锁文件位于工作树与缓存仓库之外，清理时不删除

工作树存放在项目内（见下节），物化时按需 `git ls-remote` 解析版本、把所选 ref/commit 的对象按 depth=1 抓进 `cache/git/` 并锚定，再从缓存本地 fetch 出工作树。命中缓存后不触网只适用于 **commit 已固定且对象已缓存** 的请求；需要解析版本/ref 的请求仍可能先访问上游（失败时可用缓存记录的 ref→commit 或现有树离线兜底），默认分支 HEAD 物化直接 clone 上游，缓存未命中或服务器拒绝按 SHA fetch 时回退到上游/full clone。缓存缺失/损坏或 URL 变化会在锁内重建；上游不可达且无缓存兜底时构建会失败。

需要加载项目解析输入的命令先取得 `project/.vmake/_locks/project.lock`（独占）：任何会解析依赖的命令都可能物化源码树或创建源码链接，因此同工程命令串行（`doctor`、`pkg list`、`manifest show` 等不经过 `loadResolveInputs` 的命令除外）。全局缓存的生命周期锁在构建/读取时共享，在 `clean`/`distclean`/`pkg clean`/`rebuild`、`lock update` 和 `pkg update` 时独占（`requiresExclusiveStorage`）；`ext update`/`repo update` 不经过该存储会话。同一棵树在同一时间只允许一个物化操作（`tree_<hash>.lock`），每个缓存 bare 仓库的写入与 checkout 由 `cache_<urlhash 前16位>.lock` 串行（不同 URL 不同锁）。构建与 `lock update` 在 `Require`/`Configure` 完成后的配置阶段取得所需远程包（native 子包归并到根父包）的 `owner_<sha256(name)>.lock`，并持有到命令结束，而不是整个命令期间。

网络 git 操作默认超时 30 分钟，并把 git 自己的进度实时透传到终端（`-q` 时静默）。网络较慢时可提高上限：

```bash
VMAKE_GIT_TIMEOUT=7200 vmake build   # 秒；VMAKE_FETCH_TIMEOUT 为兼容别名
VMAKE_GIT_CACHE=off vmake build      # 关闭共享 git 对象缓存，直接向上游 depth=1 抓取
```

源码：`pkg/repo/source.go`、`pkg/repo/git.go`、`pkg/repo/storage.go`、`internal/storage/`

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

工具链由 `toolchain.json` 声明，vmake 启动时扫描扩展仓库子目录自动注册，首次使用时自动下载安装。安装过程的互斥锁位于 `toolchains/_locks/<host-os>/<host-arch>/<name>/<version>.lock`。

源码：`pkg/toolchain/discovery.go` (`Manager.RegisterRepo`)、`pkg/toolchain/install.go` (`Install`)

## 项目本地目录（.vmake_deps/）

`.vmake_deps/` 为远程包和本地 `SetGit` 包保存每个包唯一的一棵工作树：所有构建配置共享同一份源码，补丁、Kconfig `.config` 和脚本内的源码改动都作用在它上面。普通本地包（未 `SetGit`）不在其中，vmake 既不复制也不重置其源码，补丁直接作用在包目录上。构建产物按构建键分开存放，不复制源码。

```
.vmake_deps/
├── <repo>/<owner>/
│   ├── src/                       # 工作树（depth=1 浅克隆，只有所选版本）
│   │   └── <member>/              # native 子包自带仓库时：子包自己的树
│   │       ├── src/
│   │       └── state.json
│   ├── state.json                 # urls/version/ref/commit/patchHash/submodules 物化状态
│   └── out/<sha256(member)>/<buildKey>/
│       ├── build/                 # 对象、库、可执行文件
│       └── install/               # 该包对外安装的 include/lib
└── local/<pkg>/
    ├── src/                       # 本地 SetGit 包的工作树
    └── state.json
```

`owner` 对普通包就是包名，对 native 子包是其根父包；`member` 是子包相对父包的路径，普通包为空字符串（`sha256("")` 仍形成一级目录）。

`buildKey` 由格式版本、工具链身份（含工具内容）、构建模式、选项组合，以及有序全局 flags、commit、buildscript 哈希等共同生成（`pkg/build/key.go` `BuildKey`）。远程包的键还包含版本号与补丁集哈希；本地包的键不包含版本和补丁集，只由通用的工具链/模式/选项/全局 flags/commit/buildscript 哈希组成。远程包的版本或补丁集变化时会重建或恢复工作树（同一 commit 的补丁变化走 `checkout --force --detach` + `clean -fdx`，不触网）；本地 `SetGit` 包的补丁变化也会重置工作树，但沿用同一构建目录，已有对象是否重编由增量规则决定。切换工具链/选项只更换 `out/<buildKey>`（远程包）或包内 `build/<buildKey>`（本地包）。

本地 SetGit 包保留 `<包目录>/src` 符号链接指向 `.vmake_deps/local/<包名>/src`，因此 `AddFiles("src/...")`、`p.SourceDir()/src/...` 等既有写法不变；链接目标不再随构建键变化。vmake 只在项目根写入 `.vmake_deps/` 到 `.gitignore`；本地 SetGit 包应在自己的 `.gitignore` 中忽略 `src/`（多数包已经如此，否则 `git status` 会显示该链接）。

`vmake clean` 只删除当前配置的构建键目录，`clean --all` 删除所有配置的产物，两者都保留 `.vmake_deps/` 源码树；`vmake distclean` 同样保留下载的工作树（下次构建直接复用，不重新下载），只有加 `--purge-sources` 才删除整个 `.vmake_deps/`。`vmake pkg clean <repo/name>` 删除单个包的构建产物，`-a` 连工作树一并删除；下次构建会重新物化该树，但共享对象缓存已有该 commit 时无需重新下载对象（版本/ref 解析仍可能访问上游）。该命令也会删除 `build/vmake-build.json`。

源码：`cmd/vmake/paths.go` (`getDepsDir`, `findProjectDir`), `pkg/repo/source.go`, `pkg/repo/storage.go`
CLI：`vmake pkg list|search|clean|update`

## 项目目录

每个项目可在 `.vmake/` 中保存多份配置，由 `.vmake/project.json` 选择当前生效的文件。没有 `project.json` 的旧工程仍使用 `config.json`，不会自动生成选择文件。

```
project/
├── build.go                       # 项目构建脚本
├── .vmake/
│   ├── project.json               # 当前配置选择（可选）
│   ├── config.json                # 默认配置
│   ├── config-debug.json          # 其他配置，格式与默认配置相同
│   ├── vmake.lock                 # 所有配置共享的远程包/有版本的本地 SetGit 包版本、commit、来源及 registry wrapper commit 锁
│   ├── layout                     # 存储布局版本标记（当前 "5"）
│   └── _locks/project.lock        # 工程独占锁（同工程命令串行）
├── .vmake_deps/                   # 第三方源码工作树与产物（自动生成，已 gitignore）
│   └── <repo>/<owner>/
│       ├── src/                   # 唯一工作树
│       └── out/<sha256(member)>/<buildKey>/
├── install/                       # 安装输出（--prefix 默认 ./install）
└── build/                         # 构建输出与结果
    ├── compile_commands.json      # LSP 编译数据库
    ├── vmake-build.json           # 结构化构建结果（build/rebuild 发布，clean/distclean/pkg clean 删除）
    └── <buildKey>/                # 64 位十六进制哈希目录
        ├── object/<hash>/foo.o    # hash 隔离 target 与源码路径，保留源码文件名
        └── <target>               # 最终产物
```

`vmake.lock` 的每条记录包含 `version`、`commit`、`source`（`registry`/`native`/`git`）；registry 包还记录提供 wrapper 的仓库 HEAD `wrapperCommit`，解析时会校验该 commit 未漂移。

`project.json` 仅保存配置文件名：

```json
{
  "config": "config-debug.json"
}
```

文件名必须以 `.json` 结尾，位于 `.vmake/` 内，不接受路径或保留名称 `project.json`。显式选择的文件缺失或 JSON 无法解析时会报错，不退回默认配置；损坏的 `project.json` 需要修正后再执行命令。

```bash
vmake config list
vmake config copy config-debug.json
vmake config use config-debug.json
vmake config
vmake build
vmake config use config.json
```

`list` 按文件名排序，用 `*` 标记当前配置，并显示未保存或无法解析的状态，以及读到的 `description` 说明。`describe` 打印当前配置的说明，`describe <文本>` 设置或清空（空字符串）该说明。`copy` 原样复制当前配置，不切换、不覆盖已有文件；默认配置尚未保存时复制的是默认骨架（`version`、空的 `global` 与 `entries`），不是空文件。`use` 验证目标后更新选择，不执行构建脚本或解析依赖，即使原选择的文件丢失也可切换。`use` 支持文件名补全，补全候选会附带说明。

说明是配置文件顶层的文本（可含换行），最长 200 字符，可为中文；不参与依赖解析，也不进入构建缓存键。TUI 选项面板顶部常驻一行 `Description`，按 `D` 或点击该行编辑，随 `Ctrl+S` 与其他修改一起保存。

TUI 和 `vmake config --set` 仅保存当前配置。构建、测试、查询、清理、`doctor` 和 `lock update` 均读取当前配置。配置选择相对于现有项目定位规则找到的根目录，构建脚本扫描范围保持不变；子目录被识别为独立项目时使用其自身配置。

所有配置共享 `.vmake/vmake.lock`，切换本身不改写依赖锁。依赖版本选择与锁更新沿用原有规则。构建缓存键不包含配置文件名，等效配置可以复用产物；`clean` 清理当前配置对应的构建输出，`clean --all` 和 `distclean` 保留所有配置及选择文件。安装仍默认使用 `install/`，同时保留多个安装结果时指定不同的 `--prefix`。

每份项目配置的结构（`pkg/config/store.go`）：

```json
{
  "version": "1",
  "description": "Board A 调试配置",
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
