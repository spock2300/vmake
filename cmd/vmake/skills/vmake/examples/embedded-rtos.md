# Embedded / RTOS Firmware

Bare-metal or RTOS firmware build using linker scripts, post-link steps,
and binary-to-header conversion.

## build.go

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("firmware").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c", "src/*.S").
            AddIncludes("include").
            SetLinkerScript("linker/stm32f4.ld").
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

- **`SetLinkerScript(path)`** — Passes `-T` to the linker for memory layout control
- **`AddBinHeader(inputs...)`** — Converts binary assets to `.h` hex arrays, output to `build/<tc>-<mode>/generated/`, include path auto-added
- **`AddPostLinkSize()`** — Prints section size info after linking
- **`AddPostLinkHex()`** — Generates Intel HEX format (`objcopy -O ihex`)
- **`AddPostLinkBin()`** — Generates raw binary (`objcopy -O binary`)
- **Firmware-specific flags** — `-ffunction-sections` + `-fdata-sections` + `-Wl,--gc-sections` for dead code elimination
- **`-nostartfiles`** — Don't link host startup files

## Project Structure

```
my-firmware/
├── build.go
├── src/
│   ├── main.c
│   └── startup.S
├── include/
│   └── stm32f4xx.h
├── linker/
│   └── stm32f4.ld
└── assets/
    └── logo.bin
```

## Build Output

```
build/
└── <toolchain>-<mode>/
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
        SetLinkerScript("linker/stm32f4.ld").
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
- Output: `build/<toolchain>-<mode>/generated/<stem>.h` (e.g., `logo.h`)
- Include path for the `generated/` directory is automatically added
- Incremental: only regenerates when source binary is newer than header
- The generated `.h` file contains a `const unsigned char` array and a `size_t` length

## Key Points

- RTOS tool accessors: `Package.ObjCopy()`, `Size()`, `ObjDump()`, `NM()` for custom post-link steps
- Use `-Wl,--print-memory-usage` during development to catch memory overflow early
- `AddBinHeader` is not limited to firmware — any binary can embed binary data as headers
- For complex firmware with KConfig (U-Boot, Linux kernel), see `examples/firmware.md`

## See Also

- references/api.md - Target post-link methods, AddBinHeader
- SKILL.md - RTOS / Embedded section
- examples/firmware.md - KConfig, EnsureConfig, multi-package firmware
