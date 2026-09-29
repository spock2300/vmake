# Cross-Package Config Propagation (ImportConfig, GenerateConfigDefines, GenerateConfigHeader)

Cross-package config propagation: the chip/HAL package declares its build options; firmware and other consuming packages import them and emit `-DCONFIG_*` defines (or an `autoconf.h`) for their own targets.

## Prerequisites

- Go 1.26+ and a vmake binary built from the repository root: `CGO_ENABLED=0 go build -o vmake ./cmd/vmake`
- A working C compiler for the `host` toolchain
- No registry setup is needed — the verification fixture uses local packages only

## Project Structure

```
myproject/
├── chip/
│   ├── build.go          # HAL: defines options, generates config defines
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

## chip/build.go (Generate Config as -D Defines)

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
        // Convert options to -DCONFIG_MCU="stm32f405" -DCONFIG_USE_DMA=1
        //          (a choice option also emits -DCONFIG_MCU_STM32F405=1)
        //          Added to ALL targets defined in this package.
        ctx.GenerateConfigDefines()

        ctx.Target("chip").SetKind(api.TargetStatic).
            AddFiles("src/*.c", "src/*.S").
            AddPublicIncludes("include")
    })
}
```

After this, every target in `chip` is compiled with `-DCONFIG_MCU="stm32f405" -DCONFIG_MCU_STM32F405=1 -DCONFIG_USE_DMA=1` (choice/string values are quoted; a choice option additionally emits `CONFIG_<NAME>_<VALUE>=1`; bools emit `=1` when true and nothing when false).

`ctx.ExportConfig()` is intentionally absent: it only records a package flag (`Package.SetExportConfig(true)`, applied by the build phase) that no build path reads today. Propagation is driven entirely by the importing package's `ImportConfig`/`SyncConfigDefines` plus `GenerateConfigDefines`/`GenerateConfigHeader`.

## rtos/build.go (Import Config from chip)

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) {
        // Remote package? It must be reachable from a local root before it
        // can be imported; AddRequires is how it enters the dependency graph.
        // Local packages are scanned and loaded up front.
        ctx.AddRequires("chip")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        // Shorthand for GenerateConfigDefines() + ImportConfig("chip"):
        // emit -DCONFIG_* defines for THIS package's targets from local +
        // imported options. Local options win on name collision.
        ctx.SyncConfigDefines("chip")

        ctx.Target("rtos").SetKind(api.TargetStatic).
            AddFiles("src/*.c").
            AddPublicIncludes("include").
            AddDeps("chip:*")
    })
}
```

## How the Merge Works

- `ImportConfig("a", "b")` only records names — it never emits a define by itself — and repeated calls accumulate the list. `SyncConfigDefines(names...)` is exactly `GenerateConfigDefines()` + `ImportConfig(names...)`.
- `GenerateConfigDefines()` performs one merge when the build phase post-processes this package's `OnBuild`: it collects the loaded packages named by the import list, merges their options and values under the local ones, converts the result to `-D` arguments, and appends them to every target of this package.
- `GenerateConfigHeader()` performs a separate merge of the same import list when the build step writes this package's `generated/autoconf.h`, so a package that only generates a header still gets imported options.
- Merge priority: a local option always wins over an imported option with the same name; imported values never override local values.
- Name resolution: imported names are looked up in the loaded dependency graph (`ctx.DepGraph.Packages`). All local `build.go` packages are loaded up front; a remote package enters the graph only when reachable via `AddRequires` from a local root, and `AddDeps` on targets that link or include it supplies the build edge. Unresolvable names are silently skipped (no error).
- Ordering is not a concern: every package's `OnConfig` option collection and resolution completes before any `OnBuild` runs, so the merge sees final definitions and values regardless of graph order. It is graph membership, not a per-package require edge, that makes a package importable.
- **Warning:** imported and local options with the same name are NOT validated for type or default consistency. The local definition and its value/default win, so the imported value is never used directly; if the local option has no default, the imported value is still coerced by the local type — a string under a local bool silently becomes `/* #undef */`/no define, and a string under a local int renders as `%!d(...)` in the macro. Keep shared option names, types and defaults aligned.
- Imported (non-global) options cannot be read with `ctx.Bool/String/Int` in the importing package: the accessor only knows this package's own options plus global options, and an unknown name is a build error. Consume imported config through the generated defines or `autoconf.h`, or copy the value into a local option in `OnConfig`.
- Global options (`ctx.GlobalOption`, every option in `Group("Global")`, including `mode`/`toolchain`) are excluded from generated defines and `autoconf.h`. Read them directly with the accessors instead.

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

`GenerateConfigHeader` creates `generated/autoconf.h` under the package's build directory (`build/<buildKey>/generated/`), and that directory is added to the include path of this package's targets only — it is not propagated to dependents. The file contains `#define CONFIG_MCU "stm32f405"`, `#define CONFIG_USE_DMA 1` etc. (a choice option additionally emits `#define CONFIG_MCU_STM32F405 1`; a disabled bool appears as `/* #undef CONFIG_XXX */`; an option with no configured value and no `SetDefault` is skipped entirely). Source files in this package can `#include "autoconf.h"`.

## Running / Verifying

`test_data/20_config_propagate` exercises exactly this flow: `chip/build.go` calls `GenerateConfigDefines()`, and the root `build.go` calls `GenerateConfigDefines()` + `ImportConfig("chip")`.

```bash
(cd test_data/20_config_propagate && ../../vmake build --install)
./test_data/20_config_propagate/install/bin/app
```

Inspect the fixture's `build/compile_commands.json`: source arguments in each command are recorded relative to the package's `SourceDir()`, and the entry's `file` field is that source resolved against the absolute `SourceDir()`. The `src/main.c` entry carries the local `app_verbose` option plus the imported `chip` options:

```json
{
  "file": "/abs/path/myproject/rtos/src/main.c",
  "arguments": [
    "...",
    "-DCONFIG_APP_VERBOSE=1",
    "-DCONFIG_CHIP_FEATURE_A=1",
    "-DCONFIG_CHIP_MODE=\"fast\"",
    "-DCONFIG_CHIP_MODE_FAST=1",
    "..."
  ]
}
```

Expected program output:

```
feature_a=1 mode=100 verbose=1
chip_mode=1
```

The chip-only compile of `chip/src/chip.c` shows just `-DCONFIG_CHIP_FEATURE_A=1 -DCONFIG_CHIP_MODE="fast" -DCONFIG_CHIP_MODE_FAST=1` (the chip package imports nothing).

## What This Demonstrates

- **`ImportConfig("pkg")`** — Declares that package's options for merging into defines/header generation; accumulated across calls. Local options take priority on name collision — no overwrite
- **`SyncConfigDefines("a", "b")`** — Shorthand for `GenerateConfigDefines()` + `ImportConfig("a", "b")`, for packages that want the defines without spelling out both calls
- **`GenerateConfigDefines()`** — Converts local + imported options to `-DCONFIG_*` compiler defines, added to ALL targets in this package. Call position within `OnBuild` does not matter — the defines are applied after the OnBuild callbacks have declared the targets
- **`GenerateConfigHeader()`** — Generates `autoconf.h` from local + imported options. Package-local only, does NOT cross packages
- **`ExportConfig()`** — Currently inert: it only sets a package flag with no consumer read path; do not rely on it for propagation

## Key Rules

- `ImportConfig` only records which packages' options to merge — it does NOT add `-D` defines by itself; `GenerateConfigDefines` is what emits them
- `GenerateConfigHeader` merges the same import list separately when writing `autoconf.h`; header generation works even if the package never calls `GenerateConfigDefines`
- The imported package should be present in the loaded graph — use `AddRequires` in `OnRequire` for remote packages (local packages are scanned up front); the merge collects loaded packages, and unresolvable import names are silently skipped
- Same-name options have no cross-package type/default validation — local declaration wins, mismatches can drop or corrupt imported values
- Imported values are NOT readable via `ctx.Bool/String/Int` — only the package's own and global options are; consume imported config as compiler defines or via `GenerateConfigHeader`
- Global options are filtered out of both generated defines and `autoconf.h`
- `autoconf.h` is package-local: `GenerateConfigHeader` in firmware does NOT produce an autoconf.h visible to chip
- Public headers must NOT `#include "autoconf.h"` — it only exists in the package that generated it

## See Also

- Option types and accessors — `SKILL.md - Option & Conditional`
- The choice macro pair emitted by the merge — `SKILL.md - OptionChoice Generates Dual Macros`
- Type/default validation that applies to global options only — `SKILL.md - GlobalOption Cross-Package Consistency`
- build.go pitfalls — `SKILL.md - Common Mistakes`
- `references/api.md` — BuildContext: GenerateConfigDefines, GenerateConfigHeader, ImportConfig, SyncConfigDefines, ExportConfig
- `references/dirs.md` — `BuildDir()` and the `generated/` directory layout
- `examples/config-to-define.md` — the three option→define/header mechanisms compared
