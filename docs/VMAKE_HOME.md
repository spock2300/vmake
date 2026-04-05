# ~/.vmake 目录结构

`~/.vmake` 是 vmake 的全局数据目录，存储工具链配置、包仓库、已安装包产物和缓存。

## 目录总览

```
~/.vmake/
├── extensions/                    # 扩展仓库（git clone）
│   └── <repo-name>/
│       ├── <plugin-name>/         # 插件目录
│       │   ├── plugin.json        # 插件元信息
│       │   └── src/main.go        # 插件源码
│       └── assets/toolchains/     # 工具链资源（可选）
│           ├── manifest.json      # 工具链清单
│           └── *.tar.gz           # 工具链压缩包（Git LFS）
├── toolchains/                    # 已安装的交叉编译工具链
│   └── <name>-<version>/          # 工具链安装目录
├── repos/                         # 包仓库索引（git clone）
│   └── <repo>/
│       └── packages/
│           └── <first-char>/
│               └── <pkg>/
│                   └── build.go
├── packages/                      # 已安装包的构建产物
│   └── <repo>/<pkg>/
│       └── <version>/
│           └── <cacheHash>/
│               ├── build/         # 中间产物（.o 文件等）
│               └── install/       # 最终产物（库、头文件）
└── cache/
    ├── buildscripts/              # 编译后的构建脚本缓存
    │   └── <name>/build.so
    └── <repo>/<pkg>/repo/         # 包源码的 git clone
```

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
├── <plugin-name>/
│   ├── plugin.json              # 插件元信息
│   └── src/main.go              # 插件入口
└── assets/
    └── toolchains/
        ├── manifest.json        # 工具链清单
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

工具链由 `manifest.json` 声明，首次使用时自动下载。

源码：`cmd/vmake/ext_cmd.go` (`loadAllToolchainManifests`)

## packages/

已安装包的构建产物。每个包的不同配置变体独立存储。

路径规则：

```
packages/<repo>/<pkg>/<version>/<cacheHash>/
├── build/                         # 中间构建产物
└── install/                       # 最终安装文件
    ├── include/                   # 头文件
    └── lib/                       # 静态库/动态库
```

`cacheHash` 由编译器名称（如 gcc）、构建模式、选项组合生成（`pkg/repo/cache.go`）。

不同工具链或选项组合产生不同的 `cacheHash`，实现配置隔离。

源码：`pkg/repo/installer.go`
CLI：`vmake pkg list|clean`

## cache/

### buildscripts/

编译后的构建脚本缓存。每个包的 `build.go` 被编译为 `.so` 文件，按时间戳判断是否需要重新编译。

路径规则：`cache/buildscripts/<name>/build.so`

`name` 中的 `/` 替换为 `_`（如 `official/curl` → `official_curl`）。

源码：`pkg/buildscript/compiler.go`

### plugins/

扩展插件在插件目录内原地编译（`extensions/<repo>/<plugin-name>/plugin.so`），不使用统一缓存目录。

源码：`pkg/plugin/compiler.go`

### 源码缓存

包源码的 Git 克隆，避免每次构建都重新下载。

路径规则：`cache/<repo>/<pkg>/repo/`

源码：`pkg/repo/source.go`

## 项目目录

每个项目可有独立的配置文件，存储在 `.vmake/config.json`。

```
project/
├── build.go                       # 项目构建脚本
├── .vmake/
│   └── config.json                # 项目配置
└── build/                         # 构建输出
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
