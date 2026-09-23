# Advanced Gotchas

## Static Library Deps with Symbols Not Referenced by Your Code

vmake wraps `AddDeps` archives in `--start-group`/`--end-group`. Your `-l`/`-L` ldflags are placed inside the group too; what lands after the group are libraries the compiler driver appends itself (e.g., libc pulled in by `-specs=nano.specs`). If a static library dep provides symbols only referenced by those post-group libraries — not by your code — the linker won't pull the relevant `.o` from the archive, because nothing in the group needed it.

**Fix (preferred):** Use `-nostdlib` in global LdFlags and `AddGlobalLinks("c_nano", "gcc")` in `SetOnApply`. This places `-lc_nano -lgcc` inside the `--start-group`/`--end-group` for all binary targets, so libc's references to your dep's symbols resolve during group scanning. No changes to the linker script needed.

```go
ctx.Option("mcu").SetType(api.OptionChoice).SetDefault("stm32f405").
    SetOnApply(func(ctx *api.ConfigContext, val any) {
        ctx.AddGlobalLdFlags("-nostdlib", "-nostartfiles")
        ctx.AddGlobalLinks("c_nano", "gcc", "nosys")
    })
```

**Fix (per-target):** Use `AddLinks("c_nano", "gcc")` on the binary target. Same mechanism, scoped to one target instead of global.

**Fix (alternative):** Use `EXTERN(symbol ...)` in the linker script. It forces the linker to treat those symbols as undefined before archive scanning. This works but requires maintaining a symbol list in the linker script.

## OnApply Callbacks, Global Flags, and Discovery Reads

`SetOnApply` callbacks run **once per build**, during the config phase, after all option values are resolved (in sorted option-name order). The context carries real option values — reading other options inside the callback works, and `ctx.Select` sees actual values (no discovery pass runs callbacks).

Global flags registered via `AddGlobalCFlags/CxxFlags/LdFlags/Links` are **buffered per package** and only applied to the toolchain manager for packages that survive `FilterDeps` — flags from pruned packages never leak into the build. Global flag changes also change the BuildKey (via the global-flags hash), so artifacts rebuild when flags change.

Two discovery-era rules still matter:

- In `OnRequire`, direct value reads (`ctx.Bool/String/Int`) are fatal only on the first pass (nil config, discovery); on the `FilterDeps` re-run (real config) they work. On the first pass, use `ctx.When("opt", value)` / `ctx.If(...)` / `ctx.Select(...)` — there `When` returns `true`, `If` returns its `then` items (condition unevaluated), and `Select` returns `""`.
- Inside `SetOnApply`, `Select` sees the resolved value — but an unmapped choice still yields `""`, which `flattenAny` would silently drop from `Add*` lists. Guard before use:

```go
ctx.Option("optimization").SetType(api.OptionChoice).
    SetDefault("O2").
    SetValues("O0", "O1", "O2", "O3", "Os").
    SetOnApply(func(ctx *api.ConfigContext, val any) {
        optFlag := ctx.Select("optimization", map[string]string{
            "O0": "-O0", "O1": "-O1", "O2": "-O2", "O3": "-O3", "Os": "-Os",
        })
        if optFlag != "" { // unmapped values Select to ""
            ctx.AddGlobalCFlags(optFlag)
        }
    })
```

- `When` compares numerics across `int`/`float64` (JSON round-trips decode numbers as `float64`) — `ctx.When("threads", 4)` works regardless of how the value was stored.

## Patching Source Before Build

Registry packages sometimes need source modifications before building (e.g., enabling a `#define` in a config header). Since `SrcDir()` points to the downloaded source, you can patch files inside `SetBuildFunc` using Go's standard `os` and `strings` packages. Note: relative paths passed to `os.*` functions resolve against the **build.go's directory** (script-relative IO), so build source paths from `p.SrcDir()`:

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

This pattern is useful for libraries that use header-based configuration (mbedtls 2.x, some RTOS SDKs) where CMake options don't cover all config flags. For multi-file or complex changes, prefer `AddPatches` (see the Applying Git Patches section below).

## Applying Git Patches (AddPatches / SetPatches)

For registry packages that need source modifications that Go string replacement can't handle (multi-file changes, binary patches, etc.), vmake supports git patch application:

```go
// In OnPackage — patches are applied before OnBuild runs
p.AddPatches("patches/fix-cross.patch", "patches/disable-avx.patch")
```

- `AddPatches(paths ...string)` — append patch files to the list (applied in declaration order)
- `SetPatches(paths ...string)` — replace the entire patch list
- `SetSubmodules(true)` — clone git submodules before applying patches
- **Remote packages**: patches apply to the writable member/build-key workspace at `~/.vmake/cache/v2/<repo>/<pkg>/<version>/out/<sha256(member)>/<buildKey>/work/repo`. The source seed remains immutable. Ordered patch content contributes to the build key.
- **Local packages**: ordinary local sources are patched in place. Local SetGit uses a writable `BuildDir()/work/src` copy exposed through `SrcDir()`. Already-applied patches are detected and skipped.
- Patch files are relative to the directory containing the package's `build.go`.

Use this when wrapping a library that needs compilation fixes (e.g., cross-compilation `CFLAGS` in a Makefile, missing `#include` guards, hardcoded toolchain assumptions). Raw `os.WriteFile` patching (shown above) is better for simple single-line changes; git patches handle multi-file, multi-line modifications reliably.
