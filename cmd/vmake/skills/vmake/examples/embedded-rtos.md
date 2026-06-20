# Embedded / RTOS Firmware

Bare-metal or RTOS firmware build using dependency linker scripts, post-link steps,
and binary-to-header conversion.

Typical multi-package layout: a **chip package** provides the linker script via
`SetProvidedLinkerScript`, and the **firmware package** inherits it via
`UseDependencyLinkerScript()`.

## chip/build.go

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
            AddCFlags("-ffunction-sections", "-fdata-sections").
            AddLdFlags("-nostartfiles", "-Wl,--gc-sections", "-Wl,--print-memory-usage").
            AddBinHeader("assets/logo.bin").
            AddPostLinkSize().
            AddPostLinkHex().
            AddPostLinkBin()
    })
}
```

## What This Demonstrates

- **`AddDeps("chip:*")`** — Wildcard dependency: links all targets from the `chip` package
- **`UseDependencyLinkerScript()`** — Firmware target auto-inherits `-T` from the first dependency that provides one
- **`AddBinHeader(inputs...)`** — Converts binary assets to `.h` hex arrays, output to `build/<buildKey>/generated/`, include path auto-added
- **`AddPostLinkSize()`** — Prints section size info after linking
- **`AddPostLinkHex()`** — Generates Intel HEX format (`objcopy -O ihex`)
- **`AddPostLinkBin()`** — Generates raw binary (`objcopy -O binary`)
- **Firmware-specific flags** — `-ffunction-sections` + `-fdata-sections` + `-Wl,--gc-sections` for dead code elimination
- **`-nostartfiles`** — Don't link host startup files

## Project Structure

```
my-project/
├── chip/
│   ├── build.go
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

`SetTest(true)` excludes the test runner from normal builds. Use `vmake test` to build and run it, or `vmake build --tests` to build without running.

## Post-Link Steps

| Method | What it does |
|--------|-------------|
| `AddPostLinkSize()` | `size {output}` |
| `AddPostLinkHex()` | `objcopy -O ihex {output} {output}.hex` |
| `AddPostLinkBin()` | `objcopy -O binary {output} {output}.bin` |
| `AddPostLinkStrip()` | `strip -o {output}.stripped {output}` |
| `AddPostLink(tool, args...)` | Custom: runs `tool args...`, supports `{output}` placeholder |

## AddBinHeader Details

```go
AddBinHeader("assets/logo.bin")
AddBinHeader("assets/a.bin", "assets/b.bin")
AddBinHeader([]string{"assets/a.bin", "assets/b.bin"})
```

- Input files can be strings or `[]string`
- Output: `build/<buildKey>/generated/<stem>.h` (e.g., `logo.h`)
- Include path for the `generated/` directory is automatically added
- Incremental: only regenerates when source binary is newer than header
- The generated `.h` file contains a `const unsigned char` array and a `size_t` length

## Key Points

- RTOS tool accessors: `Package.ObjCopy()`, `Size()`, `ObjDump()`, `NM()` for custom post-link steps
- Use `-Wl,--print-memory-usage` during development to catch memory overflow early
- `AddBinHeader` is not limited to firmware — any binary can embed binary data as headers
- For complex firmware with KConfig (U-Boot, Linux kernel), see `examples/firmware.md`
- If a static library dep provides symbols only referenced by libc (not your code), use `-nostdlib` + `AddGlobalLinks("c_nano", "gcc")` in `SetOnApply` to link libc inside the group — see SKILL.md Common Mistakes

## See Also

- references/api.md - Target post-link methods, AddBinHeader, AddLinks, AddGlobalLinks
- SKILL.md - RTOS / Embedded section, Common Mistakes (AddGlobalLinks vs EXTERN)
- examples/firmware.md - KConfig, EnsureConfig, multi-package firmware
