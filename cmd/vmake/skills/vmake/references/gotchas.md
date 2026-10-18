# Advanced Gotchas

## Static Library Deps with Symbols Not Referenced by Your Code

On ELF and PE binary links, vmake places `AddDeps` dependency archives inside `--start-group`/`--end-group` and wraps `.a`/`.so`/`.dylib` inputs in `-Wl,--whole-archive` (PE `.dll.a` import libraries are excluded), so every object of such an archive is pulled in; shared-library links pass archives as plain inputs and pull only referenced members. On Mach-O targets there is no group or whole-archive: dependency archives are force-loaded with `-force_load`, with the same all-members effect. Your `-l`/`-L` ldflags are inside the group but not whole-archived; what lands after the group are libraries the compiler driver appends itself (e.g., libc pulled in by `-specs=nano.specs`). A library supplied as a plain `-l` fails when its symbols are only referenced by those post-group libraries — nothing in the group caused the linker to scan it. Symptom: `undefined reference to ...` from libc/crt code.

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

`SetOnApply` callbacks run **once per build per declaring package**, during the config phase, after all option values are resolved (in sorted option-name order within each package; packages follow resolver order). The context carries real option values — reading other options inside the callback works, and `ctx.Select` sees actual values (no discovery pass runs callbacks).

Global flags registered via `AddGlobalCFlags/CxxFlags/LdFlags/Links` are **buffered per package** and only applied to the toolchain manager for packages that survive `FilterDeps` — flags from pruned packages never leak into the build. Global flag changes also change the BuildKey (via the global-flags hash), so artifacts rebuild when flags change.

Two discovery-era rules still matter:

- In `OnRequire`, direct value reads (`ctx.Bool/String/Int`) are fatal only on the first pass (nil config, discovery); on the `FilterDeps` re-run they work for this package's `entries.<pkg>.options` values. Global configuration values are not merged into that pass; unset reads fall back to defaults. On the first pass, use `ctx.When("opt", value)` / `ctx.If(...)` / `ctx.Select(...)` — there `When` returns `true`, `If` returns its `then` items (condition unevaluated), and `Select` returns `""`.
- Inside `SetOnApply`, `Select` sees the resolved value — but an unmapped choice still yields `""`. `Target.AddFiles`/`AddIncludes`/`AddDefines`/`AddCFlags`/`AddLinks` and the other `...any` target setters expand slices via `flattenAny`, which silently drops empty strings; `AddDeps` also skips exactly empty entries. `AddProvidedLibs` and all `AddGlobal*` take `...string` and would pass an empty argument through. Guard before use either way:

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

Registry packages sometimes need source modifications before building (e.g., enabling a `#define` in a config header). Since `SrcDir()` points to the downloaded source, you can patch files inside `SetBuildFunc` using Go's standard `os` and `strings` packages. Note: the script wrapper covers `os.Open`/`OpenFile`/`Create`/`ReadFile`/`WriteFile`/`Stat`/`Lstat`/`Mkdir`/`MkdirAll`/`Remove`/`RemoveAll`/`Rename`/`ReadDir`/`Getwd`/`TempDir`/`CreateTemp`, plus `exec.Command` and `filepath.Walk`/`WalkDir`, and resolves relative paths against the package's `SourceDir()` once it is bound (the build.go's directory before that); unwrapped APIs such as `os.Readlink`/`os.Symlink` and `exec.CommandContext` follow the process cwd (during `OnBuild`/`OnInstall`/`OnClean` that is `SourceDir()`; during `OnConfig` it is the loaded build.go directory — the registry wrapper checkout for registry packages; a `TargetVoid` `SetBuildFunc` runs later and sees the original process cwd), and `os.Chdir` is rejected. Build source paths from `p.SrcDir()`. Raw writes persist in the shared working tree and are not part of the BuildKey — prefer `AddPatches` when the change must trigger a rebuild:

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
- **Remote packages**: patches apply in place to the package's single working tree at `.vmake_deps/<repo>/<owner>/src[/<member>]` (a native member carrying its own repository gets its own tree under the member directory). Ordered patch content contributes to the build key; a changed patch set restores the tree to its pinned commit before patches are applied.
- **Local packages**: ordinary local sources are patched in place. Local SetGit uses the `.vmake_deps/local/<pkg>/src` working tree exposed through `SrcDir()`. Already-applied patches are detected and skipped.
- Patch files are relative to the directory containing the package's `build.go`.

Use this when wrapping a library that needs compilation fixes (e.g., cross-compilation `CFLAGS` in a Makefile, missing `#include` guards, hardcoded toolchain assumptions). Raw `os.WriteFile` patching (shown above) is better for simple single-line changes; git patches handle multi-file, multi-line modifications reliably.

## `[]string` Arguments — Spread Only Where the Parameter Is `...string`

Parameters typed `...any` (`AddFiles`, `AddIncludes`, `AddPublicIncludes`, `AddDefines`, `AddLinks`, `AddCFlags`, `AddCxxFlags`, `AddLdFlags`, `AddBinHeader`, `RemoveFiles`) expand slices via `flattenAny`, which also drops empty strings. Pass the slice whole; `xs...` does not compile for these because Go will not convert `[]string` to `[]any` (yaegi reports the same call-site error through `reflect.CallSlice`).

Parameters typed `...string` (`AddDeps`, `AddProvidedLibs`, `AddExcludeLibs`, `AddPostLinkDeps`, `AddPostLinkOutputs`, the scalar `Remove*` methods such as `RemoveCFlags`/`RemoveDeps`, and `SetLanguages`) accept `xs...` normally.

```go
flags := []string{"-O2", "-Wall"}
target.AddCFlags(flags)    // correct: ...any flattens the slice
target.AddCFlags(flags...) // does not compile: []string is not []any
deps := []string{"utils", "official/zlib"}
target.AddDeps(deps...)    // correct: ...string accepts a spread
```

## AddRequires vs AddDeps

`AddRequires` (in `OnRequire`) only resolves and downloads a package; `AddDeps` (in `OnBuild`) creates the build-graph edge that links its artifact and propagates public includes. Third-party packages normally need both, and a resolved package that is unreachable from a local root is not built.

## Options After the Config Phase

Declare options in `OnConfig`. `BuildContext`, `InstallContext`, and `CleanContext` lock option declaration when the config phase ends, so `ctx.Option(...)` there is a build error. `RequireContext` and `*Package` do not have that guard, but an option declared outside `OnConfig` never participates in value resolution — reads fall back to its default/zero value. Resolved values are readable in `OnBuild`/`OnInstall`/`OnClean` and inside `SetOnApply`; during `OnRequire` discovery use the discovery-aware `When`/`If`/`Select`.
