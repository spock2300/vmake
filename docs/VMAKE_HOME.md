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
│   ├── <repo>/<pkg>/<version>/src/    # 每版本不可变源码 checkout（临时目录 + 原子改名）
│   │   ├── out/<buildKey>/            # 共享二进制缓存（build/install 暂存）
│   │   │   ├── build/
│   │   │   └── install/
│   │   └── patched/<patchHash>/src/   # 应用补丁后的副本
│   ├── <repo>/<pkg>/_refs/            # tag 列表用可变 clone（native 包）
│   ├── <repo>/<pkg>/_head/            # `vmake pkg update` 用可变 clone
│   ├── _localgit/<sha256(url)>/src/   # 本地 SetGit 包的共享 clone
│   └── _locks/<repo>_<pkg>.lock       # 包级构建锁（位于受保护目录之外）
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

- `<repo>/<pkg>/<version>/src/` — 每版本不可变源码 checkout，一旦生成不再原地修改（临时 clone + 原子改名发布）
- `<repo>/<pkg>/<version>/out/<buildKey>/` — 共享二进制缓存（`build/`、`install/`），跨项目复用
- `<repo>/<pkg>/<version>/patched/<patchHash>/src/` — 声明了补丁的包使用的内容寻址补丁副本
- `<repo>/<pkg>/_refs/`、`<repo>/<pkg>/_head/` — 可变 clone（native 包 tag 列表 / `vmake pkg update`）
- `_localgit/<sha256(url)>/src/` — 本地 `SetGit` 包的共享 clone
- `_locks/<repo>_<pkg>.lock` — 包级构建锁文件，位于受保护的包目录之外，不会被删除

源码：`pkg/repo/source.go`（`SourceManager`，注释中给出完整布局）

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

## toolchains/

已安装的交叉编译工具链，由扩展自动下载或手动安装。

```
toolchains/<name>-<version>/
├── bin/
│   ├── <prefix>-gcc
│   ├── <prefix>-g++
│   └── ...
├── lib/
└── <sysroot>/
```

工具链由 `toolchain.json` 声明，通过 `tc` 插件或 `RegisterToolchainsFromRepo()` 注册，首次使用时自动下载。

源码：`pkg/toolchain/manifest.go` (`ScanRepoToolchains`)

## 项目本地目录（vmake_deps/）

`vmake_deps/` 是指向全局缓存的符号链接目录（symlink farm）：源码和构建产物实际存储在 `~/.vmake/cache/` 中，`vmake_deps/` 只保存链接，让各项目拥有独立的依赖视图。

```
vmake_deps/
└── <repo>/<pkg>/                  # Registry 和 Native 包使用相同结构
    ├── src -> ~/.vmake/cache/<repo>/<pkg>/<version>/src
    └── out -> ~/.vmake/cache/<repo>/<pkg>/<version>/out
        └── <buildKey>/
            ├── build/             # 构建产物
            └── install/           # 安装暂存
```

`buildKey` 由工具链（CC 路径）、构建模式、选项组合，以及版本号、commit、全局 flags 哈希、补丁集哈希、buildscript 哈希共同生成（`pkg/build/key.go` `BuildKey`）。src/out 符号链接同一时刻只指向一个版本目录，但缓存中可同时保留多个版本。

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
        ├── objects/               # 中间目标文件
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
