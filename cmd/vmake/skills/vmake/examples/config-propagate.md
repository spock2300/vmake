# Config Propagation (ExportConfig, ImportConfig, GenerateConfigDefines)

Cross-package config propagation: the chip/HAL package exports its build options as `-DCONFIG_*` defines that firmware and other consuming packages receive automatically.

## Project Structure

```
myproject/
├── chip/
│   ├── build.go          # HAL: defines options, exports config as -D defines
│   └── include/
│       └── hal.h
├── rtos/
│   ├── build.go          # RTOS: uses imported config defines
│   └── include/
└── firmware/
    ├── build.go          # Application: imports config + generates autoconf.h
    └── src/
        └── main.c
```

## chip/build.go (Export Config as -D Defines)

```go
package main

import "gitee.com.spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("mcu").SetType(api.OptionChoice).
            SetDefault("stm32f405").
            SetValues("stm32f405", "stm32f407").
            SetDescription("Target microcontroller")
        ctx.Option("use_dma").SetType(api.OptionBool).
            SetDefault(true).
            SetDescription("Enable DMA transfers")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        // Step 1: Convert options to -DCONFIG_MCU=stm32f405 -DCONFIG_USE_DMA
        //          Added to ALL targets defined in this package.
        ctx.GenerateConfigDefines()

        // Step 2: Mark this package's config as available for ImportConfig.
        ctx.ExportConfig()

        ctx.Target("chip").SetKind(api.TargetStatic).
            AddFiles("src/*.c", "src/*.S").
            AddPublicIncludes("include")
    })
}
```

After this, every target in `chip` is compiled with `-DCONFIG_MCU=stm32f405 -DCONFIG_USE_DMA`.

## rtos/build.go (Import Config from chip)

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        // Merge chip's exported options into this package.
        // Local options (if any) take priority on name collision.
        ctx.ImportConfig("chip")

        // Export again so firmware can also see the merged config.
        ctx.ExportConfig()

        ctx.Target("rtos").SetKind(api.TargetStatic).
            AddFiles("src/*.c").
            AddPublicIncludes("include")
    })
}
```

Note: `ImportConfig` alone does NOT add `-D` defines — it only merges the option values into the local ConfigAccessor. The `-DCONFIG_*` defines come from `GenerateConfigDefines` (called in chip). This merged config is re-exported so firmware can import them all from a single source.

## firmware/build.go (Import + Generate autoconf.h)

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        // Import merged options from chip.
        ctx.ImportConfig("chip")

        // Generate autoconf.h from local + imported options.
        ctx.GenerateConfigHeader()

        ctx.Target("firmware").SetKind(api.TargetBinary).
            AddFiles("src/*.c").
            AddDeps("chip:*", "rtos:*")
    })
}
```

`GenerateConfigHeader` creates `autoconf.h` in the build directory. The file contains `#define CONFIG_MCU stm32f405` etc. Source files in this package can `#include "autoconf.h"`.

## What This Demonstrates

- **`ExportConfig()`** — Marks a package's config as available for other packages to import
- **`ImportConfig("pkg")`** — Merges options from that package into local scope. Local options take priority on name collision — no overwrite
- **`GenerateConfigDefines()`** — Converts all resolved options to `-DCONFIG_*` compiler defines, added to ALL targets in this package. Must come BEFORE targets are created
- **`GenerateConfigHeader()`** — Generates `autoconf.h` from local + imported options. Package-local only, does NOT cross packages

## Key Rules

- `ImportConfig` merges option *values* — it does NOT add `-D` defines by itself
- `GenerateConfigDefines` is what actually emits the `-DCONFIG_*` flags
- Only one package needs to call `GenerateConfigDefines` — consumers via `ImportConfig` get the config values accessible via `ctx.Bool/String/Int`
- `autoconf.h` is package-local: `GenerateConfigHeader` in firmware does NOT produce an autoconf.h visible to chip
- Public headers must NOT `#include "autoconf.h"` — it only exists in the package that generated it

## See Also

- `SKILL.md` — Config Cross-Package Propagation section
- `references/api.md` — BuildContext: GenerateConfigDefines, ExportConfig, ImportConfig, GenerateConfigHeader
