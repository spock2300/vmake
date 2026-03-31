# VMake 嵌入式固件构建扩展设计

## 概述

扩展 vmake 从应用编译工具升级为能构建完整嵌入式 Linux/RTOS 固件的工具。

**核心思路：**
1. **KConfig 管理** — `vmake config` 嵌套管理 U-Boot/Kernel 的 `.config`
2. **预设配置** — 厂家发布预设，用户选择后定制
3. **固件打包** — `TargetVoid` + `SetBuildFunc` 收集依赖产物并打包

---

## 1. 配置存储

### 1.1 config.json 格式

`.config` 文件（含 `#` 注释行）完整编码为 JSON 字符串：

```json
{
  "version": "1",
  "global": {
    "toolchain": "host",
    "mode": "release"
  },
  "entries": {
    "u-boot": {
      "version": "2024.01",
      "selected_preset": "rockchip_rk3568_defconfig",
      "kconfig": "# CONFIG_LOCALVERSION_AUTO is not set\nCONFIG_SYS_TEXT_BASE=0x00200000\nCONFIG_CMD_BOOTM=y\nCONFIG_BOOTDELAY=3\n..."
    },
    "linux": {
      "version": "6.6",
      "selected_preset": "multi_v7_defconfig",
      "kconfig": "#\n# Automatically generated file; DO NOT EDIT.\n# Linux/arm 6.6.0 Kernel Configuration\n#\nCONFIG_EXT4_FS=y\nCONFIG_NETFILTER=y\n..."
    }
  }
}
```

### 1.2 编码规则

- 原始 `.config` 内容按 UTF-8 读取
- 换行符编码为 `\n`，双引号转义 `\"`，反斜杠转义 `\\`
- 完整保留所有 `#` 注释行

### 1.3 同步流程

| 时机 | 操作 |
|------|------|
| 构建前 | config.json kconfig → 源码目录 `.config` |
| menuconfig 后 | 源码目录 `.config` → config.json kconfig |
| 切换 preset | 预设文件 → 编码 → config.json kconfig |

---

## 2. KConfig API

### 2.1 新增类型

```go
// pkg/api/kconfig.go
type KConfigEntry struct {
    name          string
    presets       map[string]string  // preset_name → file_path
    defaultPreset string
    currentConfig string             // 当前配置（原始 .config 字符串）
}
```

### 2.2 ConfigContext 扩展

```go
func (ctx *ConfigContext) KConfig(name string) *KConfigEntry
```

### 2.3 KConfigEntry API

```go
func (k *KConfigEntry) AddPreset(name, configPath string) *KConfigEntry
func (k *KConfigEntry) SetDefault(presetName string) *KConfigEntry
func (k *KConfigEntry) Presets() []string
func (k *KConfigEntry) DefaultPreset() string
func (k *KConfigEntry) PresetPath(name string) string
func (k *KConfigEntry) CurrentConfig() string
func (k *KConfigEntry) SetCurrentConfig(config string) *KConfigEntry
```

### 2.4 使用示例

```go
// packages/uboot/build.go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.KConfig("u-boot").
        AddPreset("rockchip_rk3568", "configs/rockchip_rk3568_defconfig").
        AddPreset("minimal", "configs/minimal.config").
        AddPreset("nanopi-r5s", "configs/nanopi_r5s.config").
        SetDefault("rockchip_rk3568")
})
```

---

## 3. 预设配置系统

### 3.1 目录结构

```
packages/uboot/
├── configs/
│   ├── rockchip_rk3568_defconfig    # 厂家预设
│   ├── minimal.config                # 精简版
│   └── nanopi-r5s.config             # 板级定制
└── build.go
```

### 3.2 预设类型

预设仅支持完整 `.config` 格式，包含所有选项和 `#` 注释行，可直接使用。

---

## 4. vmake config TUI 扩展

### 4.1 界面

```
VMake Configuration
├── [Global]
│   ├── toolchain        [host                    ]
│   └── mode             [release                 ]
├── myapp
│   └── debug            [ ] Enable debug mode
├── u-boot
│   ├── preset           [rockchip_rk3568         ▼]
│   └── Run menuconfig...
└── linux
    ├── preset           [multi_v7_defconfig      ▼]
    └── Run menuconfig...
```

### 4.2 Run menuconfig 流程

1. 从 config.json 解码 kconfig → 写入源码目录 `.config`
2. `cd <source_dir> && make menuconfig`
3. 用户退出后读取 `.config`
4. 编码为 JSON 字符串 → 更新 config.json

---

## 5. 固件打包

### 5.1 使用 TargetVoid

```go
// firmware/build.go
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("u-boot", "linux")
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("firmware").
        SetKind(api.TargetVoid).
        AddDeps("u-boot:uboot", "linux:kernel", "linux:dtbs").
        SetBuildFunc(func(pkg *api.Package) error {
            return buildFirmware(pkg)
        })
})

func buildFirmware(pkg *api.Package) error {
    partitions := []Partition{
        {"bootloader", pkg.DepFile("u-boot", "u-boot.bin"),     0x000000, 0x400000},
        {"dtb",        pkg.DepFile("linux", "dtbs/rk3568.dtb"), 0x400000, 0x420000},
        {"kernel",     pkg.DepFile("linux", "zImage"),          0x420000, 0x900000},
    }
    return packFirmware(partitions, "firmware.img")
}

type Partition struct {
    Name   string
    Source string
    Offset int64
    Size   int64
}

func packFirmware(partitions []Partition, output string) error {
    f, err := os.Create(output)
    if err != nil {
        return err
    }
    defer f.Close()

    for _, part := range partitions {
        f.Seek(part.Offset, 0)
        data, _ := os.ReadFile(part.Source)
        f.Write(data)
    }
    return nil
}
```

### 5.2 辅助 API

```go
// pkg/api/package.go - 新增
func (p *Package) DepInstallDir(name string) string
func (p *Package) DepFile(name, relPath string) string
```

---

## 6. 构建流程

```
vmake build
│
├── Phase 0: 加载配置
│   └── 读取 config.json（含 kconfig 字符串）
│
├── Phase 1: OnRequire → 解析依赖图
│
├── Phase 1.5: 恢复 .config
│   └── 对每个有 kconfig 的包：解码 → 写入源码目录 .config
│
├── Phase 2: OnConfig → 收集 Options
│
├── Phase 3: OnBuild
│   ├── u-boot TargetVoid: make olddefconfig && make
│   ├── linux TargetVoid: make olddefconfig && make zImage dtbs
│   └── 应用 TargetBinary: 正常编译
│
├── Phase 4: 固件打包
│   └── firmware TargetVoid: 收集产物 → 按分区打包 → 签名
│
└── Phase 5: 保存配置（如有变更）
```

---

## 7. 完整目录结构

```
my-firmware/
├── .vmake/
│   └── config.json              # 完整配置（含 kconfig 字符串）
├── packages/
│   ├── uboot/
│   │   ├── configs/             # 预设配置
│   │   │   ├── rockchip_rk3568_defconfig
│   │   │   ├── minimal.config
│   │   │   └── nanopi-r5s.config
│   │   └── build.go
│   ├── linux/
│   │   ├── configs/
│   │   │   ├── multi_v7_defconfig
│   │   │   └── rockchip_defconfig
│   │   └── build.go
│   └── rootfs/
│       ├── overlay/
│       └── build.go
├── firmware/
│   └── build.go                 # 固件打包定义
└── keys/
    └── private.pem              # 签名密钥（可选）
```

---

## 8. 实施计划

| Phase | 内容 | 周期 |
|-------|------|------|
| **1** | KConfig 基础：类型、API、config.json 扩展、编码/解码 | 1-2 周 |
| **2** | TUI 扩展：预设选择器、menuconfig 集成 | 1-2 周 |
| **3** | 构建集成：.config 恢复、配置同步 | 1 周 |
| **4** | 固件打包示例：test_data 项目、文档 | 1 周 |
| **5** | 高级功能：RootFS、FIT Image、OTA、签名 | 后续 |

---

## 9. 关键设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| .config 存储 | JSON 字符串 | 完整保留原始内容，含 `#` 注释 |
| TargetKind | TargetVoid | 复用现有机制，不增加复杂度 |
| 预设格式 | 原始 .config/defconfig | 兼容 U-Boot/Kernel 原生格式 |
| 配置同步 | 构建前恢复 | 确保每次构建使用正确配置 |
