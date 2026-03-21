# ~/.vmake 目录结构

`~/.vmake` 是 vmake 的全局数据目录，存储工具链配置、包仓库定义、已安装包产物和各类缓存。

## 目录总览

```
~/.vmake/
├── config.json
├── repos/
│   └── <repo>/
│       └── packages/
│           └── <first-char>/
│               └── <pkg>/
│                   └── build.go
├── packages/
│   └── <repo>/<pkg>/
│       └── <version>/
│           └── <cacheHash>/
│               ├── build/
│               └── install/
└── cache/
    ├── plugins/
    │   └── <name>/
    │       └── plugin.so
    └── <repo>/<pkg>/
        └── repo/
```

## config.json

全局工具链配置文件。存储可用工具链定义（编译器路径、默认编译选项）和默认工具链选择。

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
        "cc": "gcc",
        "cxx": "g++",
        "ar": "ar",
        "ld": "ld",
        "strip": "strip",
        "ranlib": "ranlib"
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

源码：`pkg/toolchain/store.go`
CLI：`vmake toolchain init|list|show`

## repos/

通过 `git clone` 克隆的包仓库。每个仓库是一个独立的 Git 仓库，包含多个包的定义。

路径规则：`repos/<repo>/packages/<first-char>/<pkg>/build.go`

`first-char` 是包名首字母，用于避免单目录下文件过多。

源码：`pkg/repo/manager.go`
CLI：`vmake repo add|remove|list|update`

## packages/

已安装包的构建产物。每个包的不同配置变体独立存储。

路径规则：

```
packages/<repo>/<pkg>/<version>/<cacheHash>/
├── build/      # 中间构建产物（.o 文件等）
└── install/    # 最终安装文件（静态库/动态库/头文件/vmake.json）
```

`cacheHash` 由工具链、构建模式、选项组合生成（`pkg/repo/cache.go`）：

```
cacheHash = base64-URL({"toolchain":"gcc","mode":"release","options":{"ssl":"openssl"}})
```

不同工具链或选项组合会产生不同的 `cacheHash`，实现配置隔离。

源码：`pkg/repo/installer.go`
CLI：`vmake pkg list|clean`

## cache/

### plugins/

编译后的 Go 插件缓存。每个包的 `build.go` 被编译为 `.so` 文件，按时间戳判断是否需要重新编译。

路径规则：`cache/plugins/<name>/plugin.so`

`name` 中的 `/` 替换为 `_`（如 `official/curl` → `official_curl`）。

源码：`pkg/repo/resolver.go`

### 源码缓存

包源码的 Git 克隆，避免每次构建都重新下载。

路径规则：`cache/<repo>/<pkg>/repo/`

源码：`pkg/repo/source.go`


