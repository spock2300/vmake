# Config Propagation (ExportConfig, ImportConfig, GenerateConfigDefines)

Cross-package config propagation: the chip/HAL package exports its build options; firmware and other consuming packages import them and emit `-DCONFIG_*` defines for their own targets.

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

import "github.com/spock2300/vmake/pkg/api"

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
        // Step 1: Convert options to -DCONFIG_MCU="stm32f405" -DCONFIG_USE_DMA=1
        //          (a choice option also emits -DCONFIG_MCU_STM32F405=1)
        //          Added to ALL targets defined in this package.
        ctx.GenerateConfigDefines()

        // Step 2: Mark this package's config as exportable.
        ctx.ExportConfig()

        ctx.Target("chip").SetKind(api.TargetStatic).
            AddFiles("src/*.c", "src/*.S").
            AddPublicIncludes("include")
    })
}
```

After this, every target in `chip` is compiled with `-DCONFIG_MCU="stm32f405" -DCONFIG_MCU_STM32F405=1 -DCONFIG_USE_DMA=1` (choice/string values are quoted; a choice option additionally emits `CONFIG_<NAME>_<VALUE>=1`; bools emit `=1` when true and nothing when false).

## rtos/build.go (Import Config from chip)

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) {
        // chip must be a declared dependency: ImportConfig resolves names
        // against the dependency graph, and the require edge guarantees
        // chip's options are collected before this merge runs.
        ctx.AddRequires("chip")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        // Emit -DCONFIG_* defines for THIS package's targets from local +
        // imported options. Local options win on name collision.
        ctx.GenerateConfigDefines()
        ctx.ImportConfig("chip")

        // Mark this package's OWN options as exportable.
        // Imported options are NOT re-exported.
        ctx.ExportConfig()

        ctx.Target("rtos").SetKind(api.TargetStatic).
            AddFiles("src/*.c").
            AddPublicIncludes("include").
            AddDeps("chip:*")
    })
}
```

Note: `ImportConfig` alone does NOT add `-D` defines — it only records which packages' options to merge. The merge (local options win on collision) happens when `GenerateConfigDefines` runs, and the resulting defines apply only to this package's targets — so every consuming package must call `GenerateConfigDefines` itself. The imported package must be a declared dependency (`AddRequires` in `OnRequire`, plus `AddDeps` on targets that link or include it) — import names resolve against the dependency graph, and names that do not resolve are silently skipped. `ExportConfig` only sets the export flag (propagated to the package during the build phase); the merge does not require it. Imported options are not re-exported.

## firmware/build.go (Import + Generate autoconf.h)

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires("chip", "rtos")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        // Import chip's options (merged with local options when the
        // header is generated).
        ctx.ImportConfig("chip")

        // Generate autoconf.h from local + imported options.
        ctx.GenerateConfigHeader()

        ctx.Target("firmware").SetKind(api.TargetBinary).
            AddFiles("src/*.c").
            AddDeps("chip:*", "rtos:*")
    })
}
```

`GenerateConfigHeader` creates `autoconf.h` in the package's build directory (`generated/autoconf.h`, added to the include path automatically). The file contains `#define CONFIG_MCU "stm32f405"`, `#define CONFIG_USE_DMA 1` etc. (a choice option additionally emits `#define CONFIG_MCU_STM32F405 1`; a disabled bool appears as `/* #undef CONFIG_XXX */`). Source files in this package can `#include "autoconf.h"`.

## What This Demonstrates

- **`ExportConfig()`** — Sets the export flag on this package's config (propagated to `Package.SetExportConfig(true)` during the build phase); the option merge itself is driven by `ImportConfig` + `GenerateConfigDefines`
- **`ImportConfig("pkg")`** — Declares that package's options for merging into defines/header generation. Local options take priority on name collision — no overwrite
- **`GenerateConfigDefines()`** — Converts local + imported options to `-DCONFIG_*` compiler defines, added to ALL targets in this package. Call position within `OnBuild` does not matter — the defines are applied after the OnBuild callbacks have declared the targets
- **`GenerateConfigHeader()`** — Generates `autoconf.h` from local + imported options. Package-local only, does NOT cross packages

## Key Rules

- `ImportConfig` only records which packages' options to merge — it does NOT add `-D` defines by itself
- The imported package must be a declared dependency (`AddRequires` in `OnRequire`) — the require edge guarantees its options are collected before the merge; unresolvable import names are silently skipped
- `GenerateConfigDefines` is what actually emits the `-DCONFIG_*` flags — every package that wants them on its own targets must call it
- Imported values are NOT readable via `ctx.Bool/String/Int` — only the package's own and global options are; consume imported config as compiler defines or via `GenerateConfigHeader`
- `autoconf.h` is package-local: `GenerateConfigHeader` in firmware does NOT produce an autoconf.h visible to chip
- Public headers must NOT `#include "autoconf.h"` — it only exists in the package that generated it

## See Also

- `SKILL.md` — "Cross-package config propagation" entry in the Decision Guide; "OptionChoice Generates Dual Macros" section
- `references/api.md` — BuildContext: GenerateConfigDefines, ExportConfig, ImportConfig, GenerateConfigHeader
