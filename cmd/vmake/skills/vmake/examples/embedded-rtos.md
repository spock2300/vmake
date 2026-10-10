# Embedded / RTOS Firmware

Bare-metal or RTOS firmware build using dependency linker scripts, project-wide toolchain
flags, post-link steps, and binary-to-header conversion.

Typical multi-package layout: a **chip package** provides the linker script via
`SetProvidedLinkerScript`, and the **firmware package** inherits it via
`UseDependencyLinkerScript()`.

## Prerequisites

| Requirement | Why |
|-------------|-----|
| cross toolchain registered with vmake (e.g. `arm-none-eabi` from an extension repository) | the firmware target is compiled/linked by vmake |
| `vmake doctor --toolchain <name>` clean | validates resolved compiler/linker/objcopy paths |
| host C compiler | only for host-simulated RTOS projects such as `test_data/12_rtos_simulate` |

## chip/build.go (linker-script BSP)

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.SetProvidedLinkerScript("linker/sim.ld")
    })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("chip").SetKind(api.TargetVoid)
    })
}
```

`TargetVoid` produces no linked output: the package's contribution is the linker script,
attached to the package (not to a target) with `SetProvidedLinkerScript`. `AddDeps("chip:*")`
in the firmware package expands to **all targets of `chip` plus all targets of every package
`chip` depends on**, so the archive and public includes of a real HAL package arrive without
listing each target.

## Full HAL package (chip + driver sources)

For real projects the chip package also compiles startup code and drivers and supplies CPU
flags. Set those flags globally so the dependency's own static library is compiled with the
section flags too:

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("chip_model").SetType(api.OptionChoice).
            SetDefault("sim_v1").
            SetValues("sim_v1", "sim_v2").
            SetOnApply(func(ctx *api.ConfigContext, val any) {
                switch val.(string) {
                case "sim_v1":
                    ctx.AddGlobalCFlags("-DSIM_V1")
                case "sim_v2":
                    ctx.AddGlobalCFlags("-DSIM_V2")
                }
                ctx.AddGlobalCFlags("-ffunction-sections", "-fdata-sections")
                ctx.AddGlobalLdFlags("-Wl,--gc-sections")
                ctx.SetProvidedLinkerScript("linker/sim.ld")
            })
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("chip").SetKind(api.TargetStatic).
            AddFiles("src/*.c").
            AddPublicIncludes("include")
    })
}
```

`AddGlobalCFlags` / `AddGlobalCxxFlags` / `AddGlobalLdFlags` / `AddGlobalLinks` return
**nothing** — do not chain them — and they are **project-wide**, not package- or
target-scoped: vmake buffers them per declaring package, drops flags from packages that
`FilterDeps` prunes, applies the survivors to every target of every needed package, and
folds the result into the BuildKey (so changing a global flag rebuilds artifacts).
`SetOnApply` is a method of `Option` (`ctx.Option(...).SetOnApply(...)`); there is no
`Package.SetOnApply`.

`-ffunction-sections`/`-fdata-sections` must reach the package whose objects you want
trimmed. Adding them only to the firmware target leaves the dependency archives compiled
without function/data sections, and `--gc-sections` cannot shrink those objects — use
global flags as above, exactly like `test_data/12_rtos_simulate/chip_sim`. If the chip
package is only a `TargetVoid` BSP, set the same global flags in the firmware package's
`OnConfig` instead.

## firmware/build.go

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires("chip")
    })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("firmware").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c", "src/*.S").
            AddIncludes("include").
            AddDeps("chip:*").
            UseDependencyLinkerScript().
            AddBinHeader("assets/logo.bin").
            AddPostLinkSize().
            AddPostLinkHex().
            AddPostLinkBin()
    })
}
```

`UseDependencyLinkerScript()` is evaluated only on `TargetBinary`: the scheduler takes the
first dependency whose package declares `ProvidedLinkerScript` and passes it as `-T`. If no
dependency provides one, the build fails with `no dependency provides a linker script`, so
`chip` must actually be in `AddDeps`.

## Project Structure

```
my-project/
├── chip/
│   ├── build.go
│   ├── include/
│   │   └── chip.h
│   ├── src/
│   │   └── chip.c
│   └── linker/
│       └── sim.ld
└── firmware/
    ├── build.go
    ├── src/
    │   ├── main.c
    │   └── startup.S
    ├── include/
    │   └── stm32f4xx.h
    └── assets/
        └── logo.bin
```

## Build Output

```
firmware/
└── build/
    └── <hash>/
        ├── firmware              # ELF binary
        ├── firmware.hex          # Intel HEX
        ├── firmware.bin          # Raw binary
        └── generated/
            └── logo.h            # BinHeader output (auto-included)
```

## Bare-Metal Setup

Declare the bare-metal platform as global options and supply CPU flags. A toolchain
definition may provide the target defaults, so the declared options can stay
value-free:

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString)     // toolchain default, e.g. "none"
    ctx.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString) // toolchain default
    ctx.AddGlobalCFlags("-mcpu=cortex-m4", "-mthumb", "-ffunction-sections", "-fdata-sections")
    ctx.AddGlobalCxxFlags("-mcpu=cortex-m4", "-mthumb", "-ffunction-sections", "-fdata-sections")
    ctx.AddGlobalLdFlags("-mcpu=cortex-m4", "-mthumb", "--specs=nosys.specs", "-Wl,--gc-sections")
})
```

- `target_os="none"` marks bare metal and drives artifact naming and link policy; `target_triple` feeds `Configure --host` and CMake's compiler target. Resolution order: user configuration → project default → toolchain default (`toolchain.json`) → empty.
- Select the compiler with `vmake build --toolchain arm-none-eabi` (or the `toolchain` option in the active configuration). Toolchains contributed by extensions may carry `target_os`/`target_triple` defaults but never CPU flags, so declare `-mcpu`/`-mthumb`/`--specs` yourself. Validate with `vmake doctor --toolchain arm-none-eabi`. See `SKILL.md - Cross-Compiling`.
- `-nostartfiles` skips the **target** crt0/startup files provided by the compiler driver's spec; it does **not** unlink libc, so it is not bare-metal linking. A hosted libc stays linked. For bare metal use `-nostdlib` plus explicit libgcc/libc links (see the next section) or `--specs=nosys.specs` (hosted newlib, syscall stubs).
- Cross/bare-metal test binaries cannot execute on the host: `vmake test` refuses non-host targets with a clear error. Use `vmake build --tests` to compile them.
- The host-simulated fixture `test_data/12_rtos_simulate` uses the host toolchain, `-nostartfiles`, and a simulated linker script, so it builds on the development machine without a cross toolchain.

## Multiple Targets (Firmware + Test Runner)

```go
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("firmware").
        SetKind(api.TargetBinary).
        AddFiles("src/*.c", "src/*.S").
        AddIncludes("include").
        AddDeps("chip:*").
        UseDependencyLinkerScript().
        AddPostLinkSize()

    ctx.Target("test_runner").
        SetKind(api.TargetBinary).
        AddFiles("src/*.c", "tests/*.c").
        AddIncludes("include").
        AddDefines("UNIT_TEST").
        SetTest(true)
})
```

`SetTest(true)` excludes the test runner from ordinary builds. On a host-targeted
(simulated) project, `vmake test` builds and runs it; on a cross/bare-metal target
(`target_os=none`, a target OS different from the host, or a non-empty `target_triple`)
execution is refused — use `vmake build --tests` to build without running.

## Post-Link Steps

| Method | What it does |
|--------|-------------|
| `AddPostLinkSize()` | `size {output}` |
| `AddPostLinkHex()` | `objcopy -O ihex {output} {output}.hex` |
| `AddPostLinkBin()` | `objcopy -O binary {output} {output}.bin` |
| `AddPostLinkStrip()` | `strip -o {output}.stripped {output}` |
| `AddPostLink(tool, args...)` | Custom step on a toolchain tool: `objcopy`, `size`, `objdump`, `nm`, or `strip`; supports the `{output}` placeholder |
| `AddPostLinkOutputs(paths...)` | Declares extra outputs for missing-file rebuilds and automatic installation |
| `AddPostLinkDeps(files...)` | Declares extra SourceDir-relative inputs; changing one relinks and re-runs every post-link step |

Hex/Bin/Strip declare their outputs automatically. Custom steps must declare outputs explicitly; command arguments do not imply outputs. Output templates support `{output}`, with relative paths based on SourceDir.

## Static Libs, libc, and Archive Pull-In

Dependency archives collected through `AddDeps` are whole-archived on binary links, so
their objects are always pulled into the link. The classic "archive member not pulled
because nothing in the group references it" failure applies only to libraries supplied as
plain `-l` flags (`AddLinks` / `AddGlobalLinks`). The full failure model is in
`references/gotchas.md` (`Static Library Deps with Symbols Not Referenced by Your Code`);
firmware-specific fixes, in order of preference:

1. **Global:** `-nostdlib` in global LdFlags plus `AddGlobalLinks("c_nano", "gcc")` — moves libc/libgcc inside the group for all binary targets.
2. **Per-target:** `AddLinks("c_nano", "gcc")` on the binary target — same mechanism, scoped to one target.
3. **Alternative:** `EXTERN(symbol ...)` in the linker script forces the symbols undefined before archive scanning; the symbol list must be kept in sync by hand.

```go
ctx.Option("mcu").SetType(api.OptionChoice).
    SetDefault("stm32f405").
    SetValues("stm32f405", "stm32h743").
    SetOnApply(func(ctx *api.ConfigContext, val any) {
        ctx.AddGlobalLdFlags("-nostdlib", "-nostartfiles")
        ctx.AddGlobalLinks("c_nano", "gcc", "nosys")
    })
```

`AddGlobal*` methods return nothing, cannot be chained, and are project-wide (see the HAL
section above). `SetOnApply` belongs to the option: `ctx.Option(...).SetOnApply(...)`. The
full failure model and all three patterns are in `references/gotchas.md`.

## AddBinHeader Details

```go
AddBinHeader("assets/logo.bin")
AddBinHeader("assets/a.bin", "assets/b.bin")
AddBinHeader([]string{"assets/a.bin", "assets/b.bin"})
```

- Input files can be strings or `[]string`
- Output: `build/<buildKey>/generated/<stem>.h` (e.g., `logo.h`) — a **comma-separated hex fragment**, not a complete declaration. Wrap it in your own array and size it with `sizeof`:

```c
static const unsigned char logo[] = {
    #include "logo.h"
};
/* length = sizeof(logo) */
```

- Include path for the `generated/` directory is automatically added
- Incremental: only regenerates when source binary is newer than header

## Running / Verifying

```bash
# cross/bare-metal project
vmake toolchain list
vmake doctor --toolchain arm-none-eabi
vmake build --toolchain arm-none-eabi --tests
```

Expected result:

- `vmake doctor --toolchain arm-none-eabi` resolves the toolchain without PATH fallback.
- The build prints the link command plus a `size` report, and writes `firmware`, `firmware.hex`, and `firmware.bin` under `build/<buildKey>/`; `--tests` also compiles the `test_runner` binary without executing it.

```bash
# host-simulated fixture (uses the host toolchain)
cd test_data/12_rtos_simulate && ../../vmake build
```

Expected: exits 0 and writes `firmware.hex`/`firmware.bin` under `build/<buildKey>/`.

## Key Points

- RTOS tool accessors: `Package.ObjCopy()`, `Size()`, `ObjDump()`, `NM()` for custom post-link steps
- Use `-Wl,--print-memory-usage` during development to catch memory overflow early
- `AddBinHeader` is not limited to firmware — any binary can embed binary data as headers
- `-nostartfiles` skips the target crt0 but still links libc; bare metal needs `-nostdlib` + explicit libc/libgcc links or `--specs=nosys.specs`
- `AddGlobal*` methods return nothing and apply project-wide; they are included in the BuildKey
- `AddDeps("chip:*")` expands to all targets of `chip` plus its transitive package deps
- `vmake test` refuses cross/bare-metal targets; use `vmake build --tests`
- For complex firmware with KConfig (U-Boot, Linux kernel), see `examples/firmware.md`

## See Also

- SKILL.md - RTOS / Embedded
- SKILL.md - Cross-Compiling
- SKILL.md - Common Mistakes
- SKILL.md - Dependencies
- examples/symbol-management.md - visibility, version scripts, and symbol auditing for firmware libraries
- examples/firmware.md - KConfig, EnsureConfig, multi-package firmware
- references/api.md - Target post-link methods, AddBinHeader, AddLinks, AddGlobalLinks
- references/gotchas.md - archive pull-in failure model and all three fix patterns
