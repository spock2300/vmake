# ~/.vmake 目录结构

`~/.vmake` 是 vmake 的全局数据目录，存储工具链配置、包仓库索引和扩展。第三方包的源码和构建产物存储在项目本地的 `vmake_deps/` 目录。

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
└── repos/                         # 包仓库索引（git clone）
    └── <repo>/
        └── packages/
            └── <first-char>/
                └── <pkg>/
                    └── build.go

## repos/

通过 `git clone` 克隆的包仓库，包含 `build.go` 形式的包定义。

路径规则：`repos/<repo>/packages/<first-char>/<pkg>/build.go`

`first-char` 是包名首字母，用于避免单目录下文件过多。

源码：`pkg/repo/manager.go`（嵌入 `*gitstore.Store`）
CLI：`vmake repo add|remove|list|update`

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

插件 `.so` 文件编译在插件目录内（`extensions/<repo>/<plugin-name>/plugin.so`），而非统一缓存目录。

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

第三方包的构建产物存储在项目根目录的 `vmake_deps/` 中，不在全局目录。

```
vmake_deps/
└── <repo>/<pkg>/                  # Registry 和 Native 包使用相同结构
    ├── src/                       # Git 源码 checkout
    └── out/<buildKey>/
        ├── build/                 # 构建产物
        └── install/               # 安装暂存
```

`buildKey` 由编译器名称、构建模式、选项组合生成。每个包同时只有一个版本。

`vmake_deps/` 在首次构建时自动添加到项目根目录的 `.gitignore`。

源码：`cmd/vmake/paths.go` (`getDepsDir`, `findProjectDir`), `pkg/repo/source.go`, `pkg/repo/installer.go`
CLI：`vmake pkg list|clean|update`

## 项目目录

每个项目可有独立的配置文件，存储在 `.vmake/config.json`。

```
project/
├── build.go                       # 项目构建脚本
├── .vmake/
│   └── config.json                # 项目配置
├── vmake_deps/                    # 第三方包（自动生成，已 gitignore）
│   └── <repo>/<pkg>/
│       ├── src/                   # 源码
│       └── out/<buildKey>/build/  # 构建产物
├── install/                       # 安装输出
└── build/                         # 本地包构建输出
    ├── compile_commands.json      # LSP 编译数据库
    └── <tc>-<mode>/               # 如 host-debug
        ├── state.json             # 构建状态（工具链/模式）
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
