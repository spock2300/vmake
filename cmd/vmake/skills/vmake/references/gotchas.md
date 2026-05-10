# Advanced Gotchas

## Static Library Deps with Symbols Not Referenced by Your Code

vmake wraps `AddDeps` archives in `--start-group`/`--end-group`. Libraries added via `-specs` or `-l` after the group (e.g., libc from `-specs=nano.specs`) are linked later. If a static library dep provides symbols only referenced by those post-group libraries — not by your code — the linker won't pull the relevant `.o` from the archive, because nothing in the group needed it.

**Fix (preferred):** Use `-nostdlib` in global LdFlags and `AddGlobalLinks("c_nano", "gcc")` in `SetOnApply`. This places `-lc_nano -lgcc` inside the `--start-group`/`--end-group` for all targets, so libc's references to your dep's symbols resolve during group scanning. No changes to the linker script needed.

```go
ctx.Option("mcu").SetType(api.OptionChoice).SetDefault("stm32f405").
    SetOnApply(func(ctx *api.ConfigContext, val any) {
        ctx.AddGlobalLdFlags("-nostdlib", "-nostartfiles")
        ctx.AddGlobalLinks("c_nano", "gcc", "nosys")
    })
```

**Fix (per-target):** Use `AddLinks("c_nano", "gcc")` on the binary target. Same mechanism, scoped to one target instead of global.

**Fix (alternative):** Use `EXTERN(symbol ...)` in the linker script. It forces the linker to treat those symbols as undefined before archive scanning. This works but requires maintaining a symbol list in the linker script.

## `ctx.Select()` Returns `""` During discoverAll

vmake runs an internal `discoverAll` phase before the real build to discover all targets. During this phase, `ctx.Select()` returns `""` regardless of the option value. If you pass this empty string to `AddGlobalCFlags` or `AddGlobalLdFlags` inside a `SetOnApply` callback, the empty string persists in the Manager singleton (dedup won't catch it on the real build pass because `""` ≠ `"-O2"`). GCC then interprets the empty string as a filename during compilation, producing errors like:

```
arm-none-eabi-gcc: warning: : linker input file unused because linking not done
arm-none-eabi-gcc: error: : linker input file not found: No such file or directory
```

**Fix:** guard option-dependent values before passing to global flag APIs. Global flags should only be set inside `SetOnApply` callbacks (on `ConfigContext`), not in `OnBuild`:

```go
ctx.Option("optimization").SetType(api.OptionChoice).
    SetDefault("O2").
    SetValues("O0", "O1", "O2", "O3", "Os").
    SetOnApply(func(ctx *api.ConfigContext, val any) {
        optFlag := ctx.Select("optimization", map[string]string{
            "O0": "-O0", "O1": "-O1", "O2": "-O2", "O3": "-O3", "Os": "-Os",
        })

        globalCFlags := []string{
            "-Wall", "-Wchar-subscripts", "-Wformat",
            "-std=c99", "-fno-builtin",
            "-fdata-sections", "-ffunction-sections",
        }
        if optFlag != "" {
            globalCFlags = append(globalCFlags, optFlag)
        }
        ctx.AddGlobalCFlags(globalCFlags...)
    })
```

**Root cause:** `AddGlobalCFlags/LdFlags/Links` write to `toolchain.GetManager()`, a **global singleton** that persists across the discoverAll and real-build phases. The empty string from discoverAll is appended and never removed, so it reappears on the real build. Per-target `AddCFlags` does not have this problem because per-target flags are ephemeral — they exist only for the current build phase and are not stored in a global singleton.

## Patching Source Before Build

Registry packages sometimes need source modifications before building (e.g., enabling a `#define` in a config header). Since `SrcDir()` points to the downloaded source, you can patch files inside `SetBuildFunc` using Go's standard `os` and `strings` packages:

```go
SetBuildFunc(func(p *api.Package) error {
    configPath := filepath.Join(p.SrcDir(), "include", "config.h")
    raw, _ := os.ReadFile(configPath)
    raw = []byte(strings.Replace(string(raw),
        "//#define MY_FEATURE\n",
        "#define MY_FEATURE\n", 1))
    os.WriteFile(configPath, raw, 0644)
    p.CMakeConfigure("-DBUILD_SHARED_LIBS=OFF")
    p.CMakeBuild()
    p.CMakeInstall()
    return nil
})
```

This pattern is useful for libraries that use header-based configuration (mbedtls 2.x, some RTOS SDKs) where CMake options don't cover all config flags. For multi-file or complex changes, prefer `AddPatches` (see Advanced Patches section below).

## Applying Git Patches (AddPatches / SetPatches)

For registry packages that need source modifications that Go string replacement can't handle (multi-file changes, binary patches, etc.), vmake supports git patch application. Patches are applied automatically during the build pipeline:

```go
// In OnPackage — patches are applied before any build phase runs
p.AddPatches("patches/fix-cross.patch", "patches/disable-avx.patch")
```

- `AddPatches(paths ...string)` — append patch files to the list
- `SetPatches(paths ...string)` — replace the entire patch list
- `SetSubmodules(true)` — clone git submodules before applying patches
- Patches are tracked via `repo.IsPatchApplied` — same patch file won't be applied twice even across rebuilds
- Patch files are relative to `SourceDir()` (where `build.go` lives)

Use this when wrapping a library that needs compilation fixes (e.g., cross-compilation `CFLAGS` in a Makefile, missing `#include` guards, hardcoded toolchain assumptions). Raw `os.WriteFile` patching (shown above) is better for simple single-line changes; git patches handle multi-file, multi-line modifications reliably.
