# ~/.vmake 目录结构

`~/.vmake` 是 vmake 的全局数据目录，存储工具链配置、包仓库、已安装包产物和缓存。

## 目录总览

```
~/.vmake/
├── config.json                    # 全局工具链配置
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
    ├── plugins/                   # 编译后的 Go 插件缓存
    │   └── <name>/plugin.so
    └── <repo>/<pkg>/repo/         # 包源码的 git clone
```

## config.json

全局工具链配置，存储编译器路径和默认编译选项。

源码：`pkg/toolchain/store.go`
CLI：`vmake toolchain init|list|show`

```json
{
  "version": "1",
  "default_toolchain": "gcc",
  "toolchains": {
    "gcc": {
      "name": "gcc",
      "display_name": "System GCC",
      "host": "x86_64-linux-gnu",
      "tools": {
        "cc": "gcc", "cxx": "g++", "ar": "ar",
        "ld": "ld", "strip": "strip", "ranlib": "ranlib"
      },
      "default_flags": {
        "cflags": ["-O2", "-Wall"],
        "cxxflags": ["-O2", "-Wall", "-Wextra"],
        "ldflags": ["-Wl,--as-needed"]
      }
    }
  }
}
```

## repos/

通过 `git clone` 克隆的包仓库，包含 `build.go` 形式的包定义。

路径规则：`repos/<repo>/packages/<first-char>/<pkg>/build.go`

`first-char` 是包名首字母，用于避免单目录下文件过多。

源码：`pkg/repo/manager.go`
CLI：`vmake repo add|remove|list|update`

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

`cacheHash` 由工具链、构建模式、选项组合生成（`pkg/repo/cache.go`）。

不同工具链或选项组合产生不同的 `cacheHash`，实现配置隔离。

源码：`pkg/repo/installer.go`
CLI：`vmake pkg list|clean`

## cache/

### plugins/

编译后的 Go 插件缓存。每个包的 `build.go` 被编译为 `.so` 文件，按时间戳判断是否需要重新编译。

路径规则：`cache/plugins/<name>/plugin.so`

`name` 中的 `/` 替换为 `_`（如 `official/curl` → `official_curl`）。

源码：`pkg/plugin/compiler.go`

### 源码缓存

包源码的 Git 克隆，避免每次构建都重新下载。

路径规则：`cache/<repo>/<pkg>/repo/`

源码：`pkg/repo/source.go`

## 项目目录

每个项目可有独立的配置文件，存储在 `.vmake/config.json`。

```
project/
├── build.go                       # 项目插件定义
├── .vmake/
│   └── config.json                # 项目配置
└── build/                         # 构建输出
    ├── plugin.so                  # 编译后的插件
    ├── compile_commands.json      # LSP 编译数据库
    └── <tc>-<mode>/               # 如 gcc-debug
        ├── cache.json             # 增量构建缓存
        ├── objects/               # 中间目标文件
        └── <target>               # 最终产物
```

项目配置结构（`pkg/config/store.go`）：

```json
{
  "version": "1",
  "global": {
    "toolchain": "gcc",
    "mode": "debug",
    "options": { "ssl": true }
  },
  "entries": {
    "myproject": {
      "options": { "verbose": false }
    },
    "official/zlib": {
      "version": "1.3.1",
      "options": { "shared": false }
    }
  }
}
```
