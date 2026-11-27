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
├── _locks/                        # 包仓库与扩展仓库锁（repo_<hash>.lock、ext_<hash>.lock）
├── trust.json                     # 包仓库与扩展仓库信任（按仓库身份绑定）
├── trust.json.lock                # trust.json 的读写锁（位于文件旁，不在 _locks/ 下）
└── repos/                         # 包仓库索引（git clone）
    └── <repo>/
        └── packages/
            └── <first-char>/
                └── <pkg>/
                    └── build.go
```

旧版布局目录（`~/.vmake/sources`、`~/.vmake/config.json`、`cache/v2`、`cache/_localgit`、`cache/mirrors`、项目 `vmake_deps/`）不再自动清理；需要时手动删除。`trust.json` 只读取 version 2；旧版本文件按空库处理并在下一次写入时重建。`cache/git/` 是当前布局。

## repos/

通过 `git clone` 克隆的包仓库，包含 `build.go` 形式的包定义。

路径规则：`repos/<repo>/packages/<first-char>/<pkg>/build.go`

`first-char` 是包名首字母（小写），用于避免单目录下文件过多。

源码：`pkg/repo/manager.go`（嵌入 `*gitstore.Store`）
CLI：`vmake repo add|remove|list|update|trust|untrust`

## cache/

缓存根保存锁、生命周期状态与共享 git 对象缓存。`VMAKE_CACHE` 环境变量可覆盖根路径（`cmd/vmake/paths.go` `getCacheDir`）。

- `git/` — 共享内容寻址 git 对象缓存，每个上游 URL 一个 bare 仓库（`<urlhash>.git`，`urlhash = storage.OwnerKey(归一化 URL)`）。首次初始化在 `.<urlhash>.git.tmp-*` staging 目录中写入标记并执行 `git init`，完成后原子 rename 为最终命名空间；进程中断留下的 staging 目录由同一命名空间的锁持有者清理。仓库只累积 vmake 实际 fetch 过的对象，用 `refs/vmake/<sha>` 锚点固定；commit 已缓存的请求再从该缓存本地 depth=1 checkout。设 `VMAKE_GIT_CACHE=off` 可完全关闭
- `_locks/` — 生命周期锁、owner 锁、per-tree 物化锁与 per-cache-repo 锁（`cache_<urlhash 前16位>.lock`）；锁文件位于工作树与缓存仓库之外，清理时不删除
- 缓存命名空间必须带 `vmake-cache.json` 标记；缺失且非空的目录视为外部数据，vmake 不会删除它（缓存未命中时回退到直接抓取上游）。空目录、只有标记但缺少 `HEAD`/`config`/`objects` 的半初始化目录会在锁内重建；标记存在但元数据损坏的目录可通过 `vmake cache remove <url>` 删除。

包仓库与扩展仓库的锁在 `~/.vmake/_locks/`（`repo_<hash>.lock`、`ext_<hash>.lock`），把仓库身份/信任检查与 buildscript/插件加载串行化；扩展仓库的共享锁保留到进程退出，因此构建期工具链安装与插件执行期间 `ext update/trust/remove` 会等待。`flock` 不可重入，信任判定不能回调会加独占锁的命令。

工作树存放在项目内（见下节），物化时按需 `git ls-remote` 解析版本、把所选 ref/commit 的对象按 depth=1 抓进 `cache/git/` 并锚定，再从缓存本地 fetch 出工作树。命中缓存后不触网只适用于 **commit 已固定且对象已缓存** 的请求；需要解析版本/ref 的请求仍可能先访问上游（失败时可用缓存记录的 ref→commit 或现有树离线兜底），默认分支 HEAD 物化直接 clone 上游，缓存未命中或服务器拒绝按 SHA fetch 时回退到上游/full clone。缓存缺失/损坏或 URL 变化会在锁内重建；上游不可达且无缓存兜底时构建会失败。

需要加载项目解析输入的命令先取得 `project/.vmake/_locks/project.lock`（独占）：任何会解析依赖的命令都可能物化源码树或创建源码链接，因此同工程命令串行（`doctor`、`pkg list`、`pkg search`、`pkg show`、`manifest show` 等不经过 `loadResolveParams` 的命令除外）。全局缓存的生命周期锁在构建/读取时共享，在 `clean`/`distclean`/`pkg clean`/`rebuild` 和 `deps sync|update|prune|pin|unpin|fetch` 时独占（`requiresExclusiveStorage`）；`ext update`/`repo update` 不经过该存储会话。同一棵树在同一时间只允许一个物化操作（`tree_<hash>.lock`），每个缓存 bare 仓库的写入与 checkout 由 `cache_<urlhash 前16位>.lock` 串行（不同 URL 不同锁）。构建与 `deps` 写操作在 `Require`/`Configure` 完成后的配置阶段取得所需外部模块容器（`<repo>/<module>` 或 `<repo>`）的 `owner_<sha256(name)>.lock`，并持有到命令结束，而不是整个命令期间。

网络 git 操作默认超时 30 分钟；clone/fetch 在传输阶段把 git 进度实时透传到终端，而 `git ls-remote` 本身没有进度输出。解析依赖或物化包源码的命令（如 `build`/`rebuild`/`deps sync`/`deps update`/`deps pin`/`deps fetch`）会在解析 ref、列举 native 版本或尝试每个源 URL 前先打印一行状态（包名/树键、已知的目标 ref/commit、源 URL），便于判断当前步骤；缓存命中时这行也可能出现，但通常不触网。这些状态行与 git 进度都在 `-q` 下静默，`query`/`check-symbols` 固定静默。状态行不是心跳：`git ls-remote`、建立连接和服务端枚举对象期间 git 自身没有输出，可能一直静默到操作完成或触发 `VMAKE_GIT_TIMEOUT`。网络较慢时可提高上限：

```bash
VMAKE_GIT_TIMEOUT=7200 vmake build   # 秒
VMAKE_GIT_CACHE=off vmake build      # 关闭共享 git 对象缓存，直接向上游 depth=1 抓取
VMAKE_ENV_EXTRA=MY_FLAG,OTHER vmake build  # 把额外环境变量纳入增量指纹
```

CLI：`vmake cache list` 查看缓存命名空间，`vmake cache remove <url>` 删除单个命名空间（交互终端需确认；非交互环境必须传 `-y`，否则报错）。

源码：`pkg/repo/source.go`、`pkg/repo/git.go`、`pkg/repo/storage.go`、`internal/storage/`

## trust.json

全局信任库，按仓库身份记录包仓库和扩展仓库，不再绑定 commit：

```json
{
  "version": 2,
  "records": {
    "repo:official": {
      "kind": "repo",
      "name": "official",
      "url": "https://github.com/example/vmake-packages",
      "trustedAt": "2026-11-07T00:00:00Z"
    },
    "repo:myorg": {
      "kind": "repo",
      "name": "myorg",
      "url": "https://git.example.com/{name}.git",
      "trustedAt": "2026-11-07T00:00:00Z"
    },
    "extension:tools": {
      "kind": "extension",
      "name": "tools",
      "url": "https://example.invalid/tools.git",
      "trustedAt": "2026-11-07T00:00:00Z"
    }
  }
}
```

- 记录键是 `kind:name`；同一仓库后续的新 commit/新 package 继续使用同一条记录，不再逐 commit 授权。
- 包仓库由 `vmake repo trust|untrust <name>` 管理；registry 和 native 都可用，`repo trust` 不需要提交或已解析的 package commit。`repo update`/`deps update` 保持信任，`repo remove`/`untrust` 撤销。native 记录的 URL 是 `repo.json` 中的 URL 模板；对在项目 `vmake.json` `"repos"` 中自描述声明的 native 仓库，同样受信任门控并绑定 `(repo, url-template)`，可通过 `vmake repo trust <name> --url <template>` 预先授权（无需先全局 `repo add`）。
- 扩展仓库由 `vmake ext trust|untrust <name>` 管理；`ext add` 可交互确认，`ext update` 保持信任，`ext remove`/`untrust` 撤销，`ext provision` 安装当前宿主有 `installations` 条目的工具链。扩展不要求工作树干净；本地修改随仓库信任一起执行。
- 记录中保存了 URL 且与当前仓库 URL（registry 的 `remote.origin.url`、native 的 URL 模板、扩展的 origin URL）不一致时视为未信任，需要重新 `trust`。正常 update 不改变 URL；仓库没有配置 origin 时退化为按名称信任（`repo trust` 会提示），registry 仓库配置无法读取时按错误处理。
- 信任文件损坏（version 2 的 JSON 无效）时，包脚本/插件不会执行；旧版本文件按空库处理。`VMAKE_TRUST_ALL=1` 可跳过信任检查（仅限 CI/测试；它同时影响包仓库和扩展仓库）。

源码：`internal/truststore/`、`cmd/vmake/trust.go`

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
  "apiVersion": 1,
  "sourceDir": "src",
  "enabled": true
}
```

源码：`pkg/plugin/manager.go`（嵌入 `*gitstore.Store`）
CLI：`vmake ext add|remove|list|update|provision|trust|untrust`

插件源码由 yaegi 解释器动态加载（`extensions/<repo>/<plugin-name>/src/main.go`），无需编译，不产生 `.so` 文件。只有 `~/.vmake/trust.json` 中记录了仓库身份的扩展才会注册工具链或加载插件；`vmake ext update` 保持信任，本地工作树修改也不阻止加载。加载插件时取得的仓库共享锁保留到进程退出，构建期工具链安装读取的仍是同一份已信任工作树。

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

`.vmake_deps/` 为外部模块容器（native / registry）与声明了独立上游源码（`SetGit`）的构建包保存各自唯一的工作树：所有构建配置共享同一份源码，补丁、Kconfig `.config` 和脚本内的源码改动都作用在它上面。普通本地构建包（未 `SetGit`）不在其中，vmake 既不复制也不重置其源码，补丁直接作用在包目录上。构建产物按构建键分开存放，不复制源码。

`vmake pkg link <target> <path>` 可将原生模块容器（`<repo>/<module>`）或带独立上游源码的构建包（如 `<repo>/<pkg>`、`<repo>/<module>/<subpkg>`、`//<pkg>`）指向一个本地 git 工作树，记录在 `.vmake/local.json` 的 `links` 字段（机器本地，不进入 lock/config；同文件还保存 `vmake config use` 的本地激活配置游标 `config`）。link 时删除目标已有的物化工作树与 `state.json`，避免旧版本源码继续造成误解；`out/` 与容器内其他包的 `third_party/` 保留。link 期间该目标不物化到 `.vmake_deps/`，版本、tag 和 `vmake.json` 的版本约束都不参与；构建/安装输出仍写在 `.vmake_deps/<repo>/<owner>/out/...`。link 原生模块容器时，容器内的构建包按路径从 link 树按需加载，其中带 `SetGit` 独立上游源码的构建包把第三方源码物化到 `.vmake_deps/<repo>/<owner>/third_party/<memberDir>/src`，避免写入链接仓库。被 link 的目标声明 patch 或 KConfig 时拒绝（被 link 容器内未 link 的独立上游源码补丁除外）。`vmake pkg unlink <target>` 删除记录，下一次构建使用 lock 中的版本（无 lock 条目时重新解析并写入）。需要更新依赖版本或同步工作树时使用 `vmake deps update` / `vmake deps sync` / `vmake deps pin`。

```
.vmake_deps/
├── <repo>/<owner>/
│   ├── src/                       # 原生模块容器或 registry 上游源码工作树（depth=1；pkg link 时删除）
│   ├── state.json                 # urls/version/ref/commit/patchHash/submodules/patchesApplied 物化状态
│   ├── third_party/<memberDir>/   # 原生模块容器内构建包通过 SetGit 声明的独立上游源码
│   │   ├── src/
│   │   └── state.json
│   └── out/<memberDir>/<keyDir>/
│       ├── build/                 # 对象、库、可执行文件
│       └── install/               # 该构建包对外安装的 include/lib
└── local/<memberDir>/
    ├── src/                       # 本地 SetGit 构建包的独立上游源码工作树
    └── state.json
```

`owner` 对原生模块容器是 `<module>`，对 registry 包是 `<pkg>`；`member` 是容器内构建包的相对路径（根包为空字符串）。输出目录名和独立上游源码目录名是身份的投影而不是身份本身：`<memberDir>` 是可移植目录名（空 member 为 `root`，其余把路径分隔符和 Windows 非法字符换成 `-` 并追加 `sha256(member)` 的前 16 位），`<keyDir>` 取完整 `buildKey` 的前 16 位（64 bit），构建目录内的 `object/<objDir>`（隔离 target 与源码路径）、`state/<name>.link.json` 和 `state/rsp/rsp-<sha256[:16]>.rsp`（Windows GNU 命令的内容寻址参数文件：编译按 options 集合共享、链接/归档按命令复用，复用时不写临时文件，随构建目录清理）的文件名同样只取摘要前 16 位。完整构建键仍是 `PackageLayout.BuildKey` 并在 `vmake inspect` 中输出；目录名只需在同一 `out/` 下唯一，因此不再用两个 64 位哈希挤占 Windows 的 `MAX_PATH` 预算。升级后旧的 `out/<sha256(member)>/<buildKey>` 与 `build/<buildKey>` 目录不再使用，运行一次 `vmake clean --all` 即可回收。

`buildKey` 由格式版本、工具链身份（含工具内容）、构建模式、选项组合、有序全局 flags 与 commit 共同生成（`pkg/build/key.go` `BuildKey`）。远程包的键还包含版本号与补丁集哈希；本地包的键不包含版本和补丁集。build.go 文本不进入构建键，命令、include/define、文件列表等实际构建输入由 action 签名捕获，因此只改注释不会切换构建目录。远程包的版本或补丁集变化时会重建或恢复工作树（同一 commit 的补丁变化走 `checkout --force --detach` + `clean -fdx`，不触网）；本地 `SetGit` 包的补丁变化也会重置工作树，但沿用同一构建目录，已有对象是否重编由增量规则决定。切换工具链/选项只更换 `out/<memberDir>/<keyDir>`（远程包）或包内 `build/<keyDir>`（本地包）。

本地 `SetGit` 构建包保留 `<包目录>/src` 符号链接指向 `.vmake_deps/local/<memberDir>/src`，因此 `AddFiles("src/...")`、`p.SourceDir()/src/...` 等既有写法不变；链接目标不再随构建键变化。vmake 在项目根把 `.vmake_deps/` 写入 `.gitignore`；`vmake pkg link` 与 `vmake config use` 在需要时把 `.vmake/local.json` 也加入，若该文件已被 git 跟踪则拒绝写入。本地 `SetGit` 包应在自己的 `.gitignore` 中忽略 `src/`（多数包已经如此，否则 `git status` 会显示该链接）。

`vmake clean` 只删除当前配置的构建键目录，`clean --all` 删除所有配置的产物（包括 `pkg link` 包在 `.vmake_deps` 下的 `out/`），两者都保留 `.vmake_deps/` 源码树和 `.vmake/local.json`；`vmake distclean` 同样保留下载的工作树（下次构建直接复用，不重新下载），只有加 `--purge-sources` 才删除整个 `.vmake_deps/`。`pkg link` 已删除的 `src`/`state.json` 不会由 clean/distclean 恢复；unlink 后下一次构建重新物化。`vmake pkg clean <pkg>` 删除单个包的构建产物，`-a` 连工作树一并删除但不删除 link 记录；下次构建会重新物化该树，但共享对象缓存已有该 commit 时无需重新下载对象（版本/ref 解析仍可能访问上游）。该命令也会删除 `build/vmake-build.json` 与 `build/vmake-test.json`。

源码：`cmd/vmake/paths.go` (`getDepsDir`, `findProjectDir`), `pkg/pipeline/source_workspace.go`, `pkg/repo/source.go`, `pkg/repo/storage.go`
CLI：`vmake pkg list|search|show|clean|link|unlink`、`vmake deps status|sync|update|prune|pin|unpin|fetch`

## 项目目录

项目在版本控制的 `configs/**/*.json` 目录中保存多份构建配置（默认 `configs/default.json`）。`.vmake/project.json` 仅保存工程级共享的默认配置 `default_config` 与依赖 `pins`；开发者本机切换的当前配置游标保存在机器本地的 `.vmake/local.json`（`config` 字段，已 gitignore，不污染版本库），命令行也可通过无状态 `-c / --config <profile>` 直接按指定配置运行而不改写任何选择文件。

```
project/
├── vmake.json                     # 模块清单（roots / dev_roots / exports / repos / modules / sources）
├── build.go                       # 根构建包脚本（可选，有则 ID 为 "//"）
├── configs/                       # 受版本控制的构建配置（稀疏显式意图）
│   ├── default.json               # 默认配置（或由 .vmake/project.json default_config 指定）
│   ├── sku-a.json                 # 量产 SKU 配置
│   └── test/                      # 二级分组配置（例如靶机验证配置）
│       └── gfx.json
├── .vmake/
│   ├── project.json               # 团队共享的 default_config 与项目级依赖 pin（可选，可提交）
│   ├── vmake.lock                 # 锁文件 v2：containers（模块容器）与 sources（独立上游源码）的版本、commit、来源
│   ├── local.json                 # 机器本地的当前配置游标 config 与源码 link（不提交）
│   ├── layout                     # 存储布局版本标记（当前 "5"）
│   ├── _install/                  # 工程内安装前缀替换期间的瞬态恢复目录（提交或回滚后移除，distclean 清理）
│   └── _locks/                    # project.lock（工程独占锁）与 install-*.lock（工程内安装前缀锁）
├── .vmake_deps/                   # 第三方源码工作树与产物（自动生成，已 gitignore）
│   ├── <repo>/<owner>/
│   │   ├── src/                   # 模块容器或 registry 唯一工作树
│   │   ├── third_party/<memberDir>/ # 原生模块容器内构建包的 SetGit 独立上游源码
│   │   └── out/<memberDir>/<keyDir>/
│   └── local/<memberDir>/         # 本地 SetGit 构建包的独立上游源码
├── install/                       # 安装输出（--prefix 默认 ./install）
└── build/                         # 构建输出与结果
    ├── compile_commands.json      # LSP 编译数据库
    ├── vmake-build.json           # 结构化构建结果（build/rebuild 发布，clean/distclean/pkg clean 删除）
    ├── vmake-test.json            # 结构化测试结果（test 发布，clean/distclean/pkg clean 删除；不覆盖 build）
    └── <keyDir>/                  # buildKey 前 16 位十六进制
        ├── object/<objDir>/foo.o  # objDir 取摘要前 16 位，隔离 target 与源码路径
        ├── generated/             # GenerateConfigHeader/Generate/AddBinHeader 产物
        ├── state/                 # action 记录与 Windows 参数缓存
        │   ├── <name>.link.json   # 链接动作记录
        │   ├── rsp.lock           # 各进程首次发布或修复参数文件的排他锁
        │   └── rsp/rsp-<hash>.rsp # 内容寻址的 GNU 参数文件（编译按 options 集合共享，链接/归档按命令复用）
        └── <target>               # 最终产物
```

`vmake.lock`（schema `version: 2`）分 `containers`（原生模块容器与 registry 仓库容器的 `kind`/`version`/`commit`/`source`）与 `sources`（registry 包与各构建包 `SetGit` 独立上游源码的 `version`/`commit`/`source`）两个节。普通构建、`vmake deps sync` 与 `vmake deps update` 采用多 SKU 渐进式保留策略（更新当前激活条目，保留其他配置的历史条目，`--locked` 同样容忍非激活条目），仅显式执行 `vmake deps prune` 或 `--prune` 时裁剪失效条目；本地路径容器（`modules` 中的 `{"path": ...}`）不写入 `vmake.lock`。被 `pkg link` 的容器或源码保留旧 lock 条目，但 link 期间不读取、不校验、不重写；`vmake deps` 写操作跳过这些条目。

`project.json` 保存工程默认配置与全工程共享的依赖 pin（`vmake deps pin|unpin`）：

```json
{
  "default_config": "configs/sku-a.json",
  "pins": {
    "official/zlib": "1.3.1"
  }
}
```

配置解析优先级为：命令行 `-c / --config`（无状态覆盖）→ `.vmake/local.json` 的 `config` → `.vmake/project.json` 的 `default_config` → `configs/default.json`。配置文件必须位于 `configs/` 下并以 `.json` 结尾；CLI 支持简写（如 `sku-a`、`test/gfx` 自动规范化为 `configs/sku-a.json`、`configs/test/gfx.json`）。显式指定或配置选择指向的文件缺失或 JSON 无法解析时会严格报错，不静默退回默认配置。

```bash
vmake config list
vmake config copy sku-debug
vmake config use sku-debug
vmake config
vmake config --unset //:verbose
vmake config check --all
vmake build -c sku-a
```

`list` 按配置路径排序，用 `*` 标记当前配置，并显示未保存或无法解析的状态，以及读到的 `description` 说明。`describe` 打印当前（或 `-c` 指定）配置的说明，`describe <文本>` 设置或清空（空字符串）该说明。`copy` 原样复制当前（或 `-c` 指定）配置到 `configs/` 下的新文件，不切换、不覆盖已有文件。`use` 验证目标后将选择写入 `.vmake/local.json`（并确保 `.gitignore` 忽略它），不执行构建脚本或解析依赖。`check`（及 `check --all`）严格校验配置中的 `root`/`roots`、全局与包级选项名称/类型/枚举、包可达性以及 dry-run 目标声明。

说明是配置文件顶层的文本（可含换行），最长 200 字符，可为中文；不参与依赖解析，也不进入构建缓存键。TUI 选项面板顶部常驻一行 `Description`，按 `D` 或点击该行编辑，随 `Ctrl+S` 与其他修改一起保存。TUI 保存采用稀疏意图模式：只持久化显式覆盖的选项，按 `R` 可恢复默认/派生值并从配置中移除显式项；CLI 可用 `vmake config --unset/-u <[pkg:]option>` 移除显式覆盖。

TUI 和 `vmake config --set/--unset` 仅保存当前（或 `-c` 指定）配置。构建、测试、查询、`inspect`、清理、`doctor` 和 `deps` 均读取当前配置并支持 `-c <profile>` 无状态覆盖。

所有配置共享 `.vmake/vmake.lock`（schema v2，分 `containers` 与 `sources` 两区）与 `.vmake/project.json` 中的 `pins`，切换配置本身不改写依赖锁。构建缓存键不包含配置文件名，等效配置可以复用产物；`clean` 清理当前配置对应的构建输出，`clean --all` 和 `distclean` 保留所有配置及选择文件。安装仍默认使用 `install/`；同时保留多个安装结果时，可通过 `--prefix` 指定独立输出目录（例如 `-p install/sku-a`）。安装采用同卷事务替换：失败、取消或中断时恢复上一次交付结果；工程内前缀（`install/` 及 `install/*`）将恢复元数据与锁放在 `.vmake/_install/` 与 `.vmake/_locks/install-*.lock`，工程外自定义 `--prefix`（或指向外部目录的符号链接）在目标父目录旁维护隐藏的 `.<base>.vmake-installing` 与 `.<base>.vmake-install.lock` 以串行化跨工程写入。

每份项目配置的结构（`pkg/config/store.go`）：

```json
{
  "version": "1",
  "description": "Board A 调试配置",
  "root": "//",
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
      "options": { "shared": false },
      "kconfig": "",
      "selected_preset": ""
    }
  }
}
```
