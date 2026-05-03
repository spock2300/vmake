---
name: vmake
description: >
  VMake C/C++ build system assistant for writing build.go files, configuring
  build options, managing third-party packages, using vmake CLI commands,
  embedded/RTOS firmware builds, cross-compilation, linker scripts, and code
  generation. Also use when the user is working on a C/C++ project that uses
  vmake, modifying existing build.go files, debugging vmake build errors,
  setting up package repositories, or asking about vmake concepts like targets,
  dependencies, options, or lifecycle phases.
---

# VMake Build Assistant

VMake is a Go-plugin-based C/C++ build system. Build scripts are Go files
(`build.go`) compiled to plugins and executed through a multi-phase lifecycle.
It's an alternative to CMake/Meson/Bazel, using Go as the configuration language.

## Mental Model: Build Phases

Every build.go follows the same lifecycle. You don't need all phases — only
include the ones your project needs:

| Phase | Hook / Step | When you need it |
|-------|-------------|-----------------|
| 1 | `OnRequire` | Declare deps (runs with nil config; remote packages deferred) |
| 2a | `ResolveDeferred` | Remote packages cloned, compiled; their `OnRequire` runs (loops until no more deferred) |
| 2b | `OnConfig` | Build options (debug/release, features, etc.) |
| 2c | `FilterDeps` | Re-runs `OnRequire` with real config values; recomputes deps; BFS collects needed packages |
| 3 | `OnBuild` | Define targets + `autoWireRequireDeps` + compile/link |
| 4 | `OnInstall` | Custom install logic |
| clean | `OnClean` | Custom clean logic (runs during `vmake clean`; separate from build pipeline) |

`OnPackage` runs for all packages right after `Main()` is called (before any lifecycle phases). Use it to describe the package (`SetDescription`, `SetLicense`, `SetHomepage`). `SetGit`/`AddVersion` inside `OnPackage` downloads remote source to `SourceDir()/src/` — works for both registry packages and local packages that need to wrap a downloaded library.

## Decision Guide

- **New project, no options, no deps** → Only `OnBuild`. Start from `examples/simple.md`.
- **Need configurable features** → Add `OnConfig`. See `examples/config.md`.
- **Conditional compilation** → Options + `ctx.If()`/`ctx.Select()`. See `examples/conditional.md`.
- **Multiple targets (lib + binary + tests)** → See `examples/multi-target.md`.
- **Multi-module workspace (lib/ + app/ directories)** → See `examples/multi-module.md`.
- **Third-party packages** → `OnRequire` + `AddRequires` + `AddDeps`. See `examples/with-package.md`.
- **Wrap external C/C++ library (CMake/Autotools)** → `TargetVoid` + `SetBuildFunc`. See `examples/third-party-wrapper.md`.
- **Pre-compiled libraries (.a/.so)** → `SetPrebuilt`. See `examples/prebuilt.md`.
- **Code generation / host tools** → `BuildSubGraph` + `DepOutput` + `Exec`. See `examples/subbuild.md`.
- **Embedded / RTOS firmware (linker script, hex/bin)** → See `examples/embedded-rtos.md`.
- **Embedded firmware (KConfig/partitions)** → `EnsureConfig` + `PatchKConfig` + `DepBuildDir`. See `examples/firmware.md`.

## Build Script Template

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
    })

    p.OnClean(func(ctx *api.CleanContext) {
    })
}
```

## Common Mistakes

### `pkg.Make()` runs in BuildDir, not SourceDir

`pkg.Make()` always runs `make` in `BuildDir`. For most third-party packages (U-Boot, Linux, Busybox, etc.), the Makefile is in the source tree, so you need `pkg.RunIn()`:

```go
SetBuildFunc(func(p *api.Package) error {
    srcDir := p.SrcDir()
    p.EnsureConfig(srcDir)
    pkg.RunIn(srcDir, "make", "-j"+strconv.Itoa(runtime.NumCPU()))
    return nil
})
```

Use `pkg.Make()` only when the build process should run in the scratch `BuildDir` (rare — mainly custom builds that generate Makefiles via CMake/Configure into BuildDir).

### `$(nproc)` won't work — use `runtime.NumCPU()`

`exec.Command` doesn't expand shell features. `$(nproc)`, `$(pwd)`, and pipes won't work. Use Go APIs instead:

```go
"-j" + strconv.Itoa(runtime.NumCPU())
```

### `ctx.If()` returns `[]string` — must spread with `...`

```go
AddCFlags(ctx.If("debug", "-g", "-O0")...)   // correct
AddCFlags(ctx.If("debug", "-g", "-O0"))      // compile error
```

### `filepath.Join` with absolute paths

`filepath.Join("/a/b", "/a/b/c")` returns `/a/b/a/b/c`, NOT `/a/b/c`. The second absolute path wins and the first becomes a segment. Use string concatenation or trim leading `/` for logical path components.

### `SetGit`/`AddVersion` — works for local packages too

`SetGit`/`AddVersion` in `OnPackage` is the primary mechanism for registry packages, but it also works for local packages that need to download and compile a remote library (e.g., FreeRTOS, mbedtls). When a local package uses `SetGit`, source is downloaded to `SourceDir()/src/` and `SrcDir()` returns that path, just like registry packages. This is the easiest way to wrap a third-party C library that doesn't need a full registry setup.

### Path resolution for packages using `SetGit`

When a package uses `SetGit`, `SourceDir()` and `SrcDir()` differ: `SourceDir()` is where `build.go` lives, `SrcDir()` = `SourceDir()/src/` is the downloaded source. This causes non-obvious path resolution behavior in `OnBuild`:

- **`AddFiles`** paths resolve from `SourceDir()`. Use `"src/tasks.c"`, NOT `"tasks.c"`. Single filenames without glob characters (`*`, `?`) may silently match nothing — always use the `src/` prefix.
- **`AddIncludes`** paths resolve from `SourceDir()`. Use `"src/include"` to reach downloaded headers.
- **`AddPublicIncludes`** paths propagate to dependents resolved from `SrcDir()`. Use `"include"` (resolves to `SrcDir()/include/` = `SourceDir()/src/include/`). Since `AddPublicIncludes` implies `AddIncludes`, the target itself also gets these paths resolved from `SrcDir()`.
- **Relative parent paths** (`"../include"`) in `AddPublicIncludes` produce incorrect doubled paths (e.g., `/path/src/src/include`). Avoid them — place config headers inside `SrcDir()/include/` instead.
- **Absolute paths** in `AddFiles`/`AddPublicIncludes` get `SrcDir()` prepended, creating wrong paths like `/src/home/user/project/src/file`. Always use relative paths.

```go
// Correct pattern for a local package wrapping a SetGit download:
p.OnPackage(func(p *api.Package) {
    p.SetGit("https://github.com/FreeRTOS/FreeRTOS-Kernel.git")
    p.AddVersion("11.3.0", "V11.3.0")
})
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("freertos").SetKind(api.TargetStatic).
        AddFiles(
            "src/tasks.c",          // SourceDir()-relative
            "src/portable/GCC/ARM_CM4F/port.c",
        ).
        AddPublicIncludes(
            "include",              // SrcDir()-relative for propagation
            "src/include",          // SourceDir()-relative for self-compilation
            "src/portable/GCC/ARM_CM4F",
        )
})
```

### Static library deps with symbols not referenced by your code

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

### `pkg.Run()` calls `os.Exit` on failure

`pkg.Run()` and `pkg.RunIn()` use `exec.RunFatal` internally — they never return a non-nil error. Only `pkg.RunEnv()` returns a real error that you should check.

### `vmake clean` vs `vmake distclean`

`vmake clean` executes `OnClean` hooks for all packages (which run custom clean commands like `make clean` in source directories), then removes build artifacts (compiled objects, binaries, libraries). It keeps the compiled `build.so` plugin and dependency source in `vmake_deps/`. If plugin loading fails, it falls back to scan-only directory cleanup.

`vmake distclean` is a deeper clean: it removes all local build dirs, build.so, go.mod/go.sum, install/, and the entire `vmake_deps/` directory. Use this when modifying `build.go` (add/remove targets, change options, change includes) and the build seems to ignore your changes. The actual source data in `~/.vmake/sources/` is preserved — only the symlinks are removed.

### Patching source before build in registry packages

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

This pattern is useful for libraries that use header-based configuration (mbedtls 2.x, some RTOS SDKs) where CMake options don't cover all config flags.

### `ctx.Select()` returns `""` during discoverAll — guard before passing to global flags

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

This only affects `AddGlobalCFlags/LdFlags` — per-target `AddCFlags` does not have this problem because per-target flags don't persist across discoverAll/build phases via a singleton.

## Directory Reference

| Property | What it returns | When to use |
|----------|-----------------|-------------|
| `SourceDir()` | Package root (where build.go lives) | Package metadata files, overlay dirs |
| `SrcDir()` | Source code dir (`SourceDir()/src/` when `SetGit` downloads source, falls back to `SourceDir()`) | Source files for firmware/third-party builds |
| `SrcDirRaw()` | Raw srcCodeDir without fallback (empty if `SetSrcDir` not called) | Detecting whether source dir was explicitly set |
| `BuildDir()` | Scratch dir for intermediate artifacts | Build outputs, stamps |
| `InstallDir()` | Installation prefix | Headers/libs installed by third-party packages |
| `ScriptDir()` | Same as `SourceDir()` | Legacy alias |

BuildDir path differs by package origin:
- **Local packages**: `<SourceDir>/build/<key>/`
- **Remote packages**: `vmake_deps/<repo>/<pkg>/out/<key>/build/`

Key distinction: for any package using `SetGit` (registry or local), `SourceDir()` is where `build.go` lives, but the actual downloaded source is at `SrcDir()` (= `SourceDir()/src/`). For a local package without `SetGit`, `SourceDir()` == `SrcDir()` (the fallback). Use `SrcDirRaw()` to check whether `SetSrcDir` was explicitly called (returns empty string if not).

Within `SetBuildFunc`, built-in helpers (`CMakeConfigure`, `CMakeBuild`, `CMakeInstall`) automatically use the correct directories. If you need to read or patch source files manually, use `SrcDir()` to locate the downloaded source tree.

## Storage Layout

Third-party package source code is **shared globally** across projects via symlinks, while compiled buildscript plugins and build artifacts remain **per-project** in `vmake_deps/`. This directory is auto-added to `.gitignore` on first build.

```
project/
├── build.go
├── src/
├── vmake_deps/                    # Auto-managed, gitignored
│   └── <repo>/<pkg>/
│       ├── src/ → ~/.vmake/sources/<repo>/<pkg>/src/   # Symlink to global cache
│       ├── build.so               # Compiled buildscript plugin (project-local)
│       └── out/<buildKey>/
│           ├── build/             # Build artifacts (project-local)
│           └── install/           # Install staging (project-local)
```

Both registry and native packages use the same `<repo>/<pkg>/` structure. There is no version layer in paths — each package has one version at a time. The `src/` entry is a symlink pointing into the global source cache at `~/.vmake/sources/`. All projects sharing the same package version use a single git checkout. File locking (`flock`) serializes concurrent access across projects.

Global storage in `~/.vmake/`:
- `~/.vmake/sources/<repo>/<pkg>/src/` — shared git source checkouts (symlinked into each project's `vmake_deps/`)
- `~/.vmake/repos/` — registry repo clones (buildscript metadata only)
- `~/.vmake/toolchains/` — toolchain manifests
- `~/.vmake/extensions/` — extension repos

vmake locates the project root by walking upward from cwd to find `.vmake/` or `build.go` (via `findProjectDir()`). This ensures commands and shell completion work from subdirectories.

## Package Types

| Type | How identified | `OnPackage` metadata | Source code location |
|------|---------------|---------------------|---------------------|
| **Local** | build.go in project directory | `SetDescription`, `SetLicense`, or `SetGit`/`AddVersion` for remote source | `SourceDir()` (same as build.go), or `SrcDir()` = `SourceDir()/src/` if `SetGit` used |
| **Registry** | `vmake repo add name url` | `SetGit`, `AddVersion` required | `SrcDir()` (downloaded to `SourceDir()/src/`) |
| **Native** | `vmake repo add --native name url` | No `SetGit`/`AddVersion` — version from git tag | `SrcDir()` (downloaded to `SourceDir()/src/`) |

Registry packages wrap external C/C++ libraries. Native packages are independent vmake projects consumed as dependencies. The resolver checks registry first, then native.

## Target API at a Glance

```go
ctx.Target("app").
    SetKind(api.TargetBinary).
    AddFiles("src/*.c").
    AddPublicIncludes("include").
    AddDefines("DEBUG=1").
    AddCFlags("-Wall").
    AddCxxFlags("-stdlib=libc++").
    AddLdFlags("-lm").
    AddLinks("ssl", "crypto").
    AddDeps("lib:utils").
    SetDefault(false).
    SetBuildFunc(func(p *api.Package) error { ... }).
    SetPrebuilt("/path/to/libfoo.a")
```

`AddPublicIncludes` implies `AddIncludes` — directories set via `AddPublicIncludes` are automatically available to the target itself and propagated to all dependents. There is no need to duplicate them with `AddIncludes`. Use `@"pattern"` as the last argument to filter propagated files: `AddPublicIncludes(".", "@*.h")` only propagates headers matching `*.h`.

`AddFiles` accepts glob patterns and can be called with multiple globs to collect sources from different directories:

```go
AddFiles("src/common/*.c", "src/network/*.c", "src/stun/*.c")
```

Remove flags: `RemoveCFlags`, `RemoveDefines`, `RemoveIncludes`, etc. Use `RemoveFiles("src/test_*.c")` to exclude files matched by `AddFiles` globs — it filters glob results at build time using pattern matching.

Third-party packages with external build systems use `TargetVoid` with `SetBuildFunc`.

## Prebuilt Libraries

Use `SetPrebuilt(path)` on `TargetStatic`, `TargetShared`, or `TargetBinary` to export a pre-compiled artifact without any compilation. The scheduler creates a **symlink** from the expected output path to the prebuilt file — no copy, zero disk overhead.

```go
ctx.Target("drv").
    SetKind(api.TargetStatic).
    SetPrebuilt(filepath.Join(p.SourceDir(), "lib", "libdrv.a")).
    AddPublicIncludes("include")
```

Downstream packages link against it automatically through normal dependency resolution (`AddDeps`). Combine with `AddPublicIncludes` for headers and `SetProvidedLinkerScript` for linker scripts.

- Works with `TargetStatic` (.a), `TargetShared` (.so), `TargetBinary`
- No source compilation — scheduler skips compile phase entirely
- Incremental: compares symlink target, recreates only if path changed
- `AddProvidedLibs` on Target declares library names provided to consumers (e.g., `.AddProvidedLibs("drv", "m", "pthread")` propagates `-ldrv -lm -lpthread` to consumers)
- Multiple prebuilt libraries: define one target per `.a`/`.so` file

```go
ctx.Target("ssl").SetKind(api.TargetStatic).
    SetPrebuilt(filepath.Join(p.SourceDir(), "lib", "libssl.a"))
ctx.Target("crypto").SetKind(api.TargetStatic).
    SetPrebuilt(filepath.Join(p.SourceDir(), "lib", "libcrypto.a"))
```

## Test Targets

Use `SetTest(true)` to mark a target as a test. Test targets are excluded from `vmake build` by default:

```go
ctx.Target("tests").
    SetKind(api.TargetBinary).
    SetTest(true).
    AddFiles("tests/*.c").
    AddDeps("mylib")
```

- `vmake build` — skips test targets
- `vmake build --tests` — builds everything including tests
- `vmake test` — builds all test targets, then executes `TargetBinary` tests, reports pass/fail with timing
- Test targets are never installed (`--install` skips them)
- Test targets can depend on other test targets (e.g., a `TargetStatic` test lib); only `TargetBinary` tests are executed

Always define test targets unconditionally in `OnBuild` (not guarded by options). `SetTest(true)` controls visibility — `vmake build` skips them, `vmake build --tests` includes them. If you gate the target definition behind `if ctx.Bool("tests") { ... }`, it won't exist when `--tests` is passed, because option values are resolved from `config.json` (which may not have `tests=true`).

## Dependencies

### Declaring and Using Dependencies

```go
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("official/zlib >=1.2")
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").AddDeps("official/zlib")
})
```

### Auto-Wire: When AddDeps is Automatic

After all `OnBuild` callbacks execute, vmake runs `autoWireRequireDeps`. It uses the dependency list recomputed by `FilterDeps` (Phase 2c). For each local target, for each `AddRequires` entry from the final require list, it adds `AddDeps("pkg:target")` for ALL targets defined by that dependency package. This means:

- `AddRequires("busybox")` + busybox defines target `"busybox"` → all your targets automatically get `AddDeps("busybox:busybox")`
- You do NOT need explicit `AddDeps` when depending on a whole package via `OnRequire`

Explicit `AddDeps` IS needed for:
- Same-package target deps: `AddDeps("mylib")`
- Specific cross-package target: `AddDeps("lib:utils")`
- Wildcard dep on all targets of a package: `AddDeps("chip:*")` or `AddDeps("official/zlib:*")`
- Selective dep on a third-party package: `AddDeps("official/zlib:target")` to link against a specific target rather than all targets from that package

### Dependency Format

- `"utils"` — same-package target
- `"lib:utils"` — specific cross-package target (build order + link + PublicIncludes)
- `"lib:*"` or `"official/zlib:*"` — wildcard: all targets from that package + transitive deps
- `"official/zlib"` — third-party package (expanded to all targets from that package)

### Version Constraints

AddRequires accepts an optional semver constraint after the package name:

```go
ctx.AddRequires("official/zlib >=1.2")   // constraint: >=1.2
ctx.AddRequires("official/curl ~8.5")    // constraint: ~8.5
ctx.AddRequires("test_build/mathlib")    // no constraint = any version
```

**Constraint operators:**

| Op | Meaning | Example | Matches | Excludes |
|----|---------|---------|---------|----------|
| `>=` | ≥ with major lock | `>=1.2` | `1.2.0`–`1.x.x` | `2.0.0`, `1.1.0` |
| `>` | > no lock | `>1.0` | `1.0.1`, `2.0.0` | `1.0.0` |
| `<=` | ≤ no lock | `<=2.0` | all ≤ 2.0.0 | `2.0.1` |
| `<` | < no lock | `<3.0` | all < 3.0.0 | `3.0.0` |
| `=` | exact | `=1.2.3` | `1.2.3` | everything else |
| `~` | lock major.minor | `~1.2.3` | `1.2.x` | `1.3.0` |
| (none) | same as `>=` | `1.2` | same as `>=1.2` | — |

**Major compatibility lock:** `>=` with major > 0 restricts to the same major version (`>=1.2` won't match `2.0.0`). Empty constraint (no version specified) matches all versions. `>`, `<=`, `<` do not lock major — they allow cross-version range comparisons.

**Selection:** From all versions satisfying the constraint, the highest is chosen. When multiple packages depend on the same package with different constraints, all constraints must be mutually satisfiable.

**Multi-constraint selection:** Use `Package.SelectVersionMulti(constraints []string)` in `OnPackage`/`SetBuildFunc` to match against multiple constraints: `p.SelectVersionMulti([]string{">=1.0", "<2.0"})` finds the highest version satisfying all constraints simultaneously.

### OnRequire Two-Phase Execution

`OnRequire` callbacks execute **twice** — once for discovery, once with real configuration:

| Pass | Phase | Config values | Purpose |
|------|-------|--------------|---------|
| 1 | Phase 1 / 2a | `nil` | Discover initial dependency graph. Remote packages are deferred and compiled in Phase 2a; their `OnRequire` runs for the first time here. |
| 2 | Phase 2c (`FilterDeps`) | Real values from `config.json` | After `OnConfig` has resolved all option values, `FilterDeps` re-runs every package's `OnRequire` with actual config. The returned dependencies **replace** `node.Deps`, then topology is re-sorted and needed packages are collected via BFS. |

This is what enables **option-conditional dependencies** — if `OnRequire` only ran once with nil config, your `ctx.Bool("use_ssl")` check would never see the user's actual choice:

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Option("use_ssl").SetType(api.OptionBool).SetDefault(false)
})

p.OnRequire(func(ctx *api.RequireContext) {
    if ctx.Bool("use_ssl") {
        ctx.AddRequires("official/openssl")
    }
})
```

**Mechanism:** `FilterDeps` calls `Package.UpdateRequireContext(cfgVals, options)`, which creates a fresh `RequireContext` with populated `ConfigAccessor`, runs all `OnRequire` callbacks, and replaces `p.requires` with the result. This runs for **every** package in the graph, not just local ones.

**Key implication:** `AddRequires` alone does not guarantee a package will be built. A package is only built if reachable from a local root via BFS (`collectNeeded`), which runs after `FilterDeps` updates the dependency graph.

## Option & Conditional

```go
ctx.Option("debug").SetType(api.OptionBool).SetDefault(false)

AddCFlags(ctx.If("debug", "-g", "-O0")...)
AddCFlags(ctx.IfNot("debug", "-O2")...)
AddCFlags(ctx.Select("opt", map[string]string{
    "O0": "-O0", "O2": "-O2",
}))

ctx.String("name")
ctx.Int("count")
ctx.Bool("debug")
ctx.When("x", "val")

ctx.Option("chip").SetType(api.OptionChoice).SetValues("stm32f4", "esp32").
    SetOnApply(func(ctx *api.ConfigContext, val any) {
        ctx.SetProvidedLinkerScript("linker/" + val.(string) + ".ld")
    })

ctx.Option("trace").SetType(api.OptionBool).SetDefault(false).
    SetOnApply(func(ctx *api.ConfigContext, val any) {
        if val.(bool) {
            ctx.AddGlobalCFlags("-DTRACE=1", "-finstrument-functions")
            ctx.AddGlobalLdFlags("-ltrace")
        }
    })
```

- `SetOnApply(fn)` — callback invoked after all option values are resolved, receives `*ConfigContext` and `val any` (typed: `bool` for OptionBool, `int`/`float64` for OptionInt, `string` for OptionString/OptionChoice; note: JSON round-trip decodes numbers as `float64`); used to react to options (e.g., set global flags, choose linker script based on chip)

### Global Flags

`AddGlobalCFlags/CxxFlags/LdFlags/Links` are only available on `ConfigContext`, effective inside `SetOnApply` callbacks.

Global flags apply to ALL targets in ALL packages. They are deduplicated ("appends if not already present"). During compilation, global C/C++ flags are prepended before per-target flags. During linking, global LD flags are appended after per-target flags. Global links are placed inside `--start-group`/`--end-group` alongside target-level links.

```go
ctx.Option("chip").SetType(api.OptionChoice).SetValues("stm32f4", "esp32").
    SetOnApply(func(ctx *api.ConfigContext, val any) {
        ctx.AddGlobalCFlags("-Wall", "-ffunction-sections", "-fdata-sections")
        ctx.AddGlobalLdFlags("-Wl,--gc-sections", "-nostdlib")
        ctx.AddGlobalLinks("c_nano", "gcc")
        ctx.SetProvidedLinkerScript("linker/" + val.(string) + ".ld")
    })
```

## RTOS / Embedded

### Simple chip package (no compilation, linker script only)

```go
// chip/build.go — provides linker script, no compiled output
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.SetProvidedLinkerScript("linker/sim.ld")
})
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("chip").SetKind(api.TargetVoid)
})
```

### Full HAL package with global flags (realistic firmware)

For real embedded projects, the chip/HAL package compiles startup code as a static library and sets global compiler/linker flags via `AddGlobalCFlags`/`AddGlobalLdFlags` in `SetOnApply`. This ensures all packages build with the same target-specific flags. Add more packages (RTOS, BSP) following the same pattern — each as a `TargetStatic` with `AddDeps` on its dependencies.

```go
// chip/build.go — HAL layer: startup code, global flags, linker script
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Option("mcu").SetType(api.OptionChoice).
        SetDefault("stm32f405").
        SetValues("stm32f405").
        SetOnApply(func(ctx *api.ConfigContext, val any) {
            ctx.AddGlobalCFlags("-mcpu=cortex-m4", "-mthumb",
                "-ffreestanding", "-mfpu=fpv4-sp-d16", "-mfloat-abi=hard",
                "-DSTM32F405RG")
            ctx.AddGlobalLdFlags("-nostartfiles", "-nostdlib",
                "-mcpu=cortex-m4", "-mthumb",
                "-mfpu=fpv4-sp-d16", "-mfloat-abi=hard",
                "-Wl,--gc-sections", "-Wl,--print-memory-usage")
            ctx.AddGlobalLinks("c_nano", "gcc")
        })
    ctx.SetProvidedLinkerScript("linker/stm32f405.ld")
})
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("chip").SetKind(api.TargetStatic).
        AddFiles("src/*.S", "src/*.c").
        AddPublicIncludes("include")
})

// firmware/build.go — application, consumes linker script
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("chip")
})
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("firmware").SetKind(api.TargetBinary).
        AddFiles("src/*.c").
        AddDeps("chip:*").
        UseDependencyLinkerScript().
        AddPostLinkSize().AddPostLinkHex().AddPostLinkBin()
})
```

Key points for embedded firmware:
- **Target-specific flags in both CFLAGS and LDFLAGS**: Flags like `-mfloat-abi=hard -mfpu=fpv4-sp-d16` must appear in both `AddGlobalCFlags` and `AddGlobalLdFlags`, otherwise the linker reports ABI mismatches with libc.
- **`-nostdlib` + `AddGlobalLinks`**: Use `-nostdlib` in LdFlags to prevent GCC from auto-linking system libraries. Then use `AddGlobalLinks("c_nano", "gcc")` in `SetOnApply` to link libc/libgcc inside the `--start-group`/`--end-group` for all targets. This ensures libc's references to your dep's symbols (e.g., syscalls from a BSP static library) resolve during group scanning.
- If you use `-specs=nano.specs` instead (libc linked after the group), and a dep provides symbols only libc references, use `EXTERN` in the linker script — see Common Mistake above.

- `SetProvidedLinkerScript(path)` — chip/bsp package declares linker script for consumers (on `ConfigContext`; fatal on double-set)
- `UseDependencyLinkerScript()` — firmware target auto-inherits `-T` from first dependency that provides one
- `SetLinkerScript(path)` — direct linker script on target (fatal on double-set; use when no dependency pattern needed)
- `AddPostLink(tool, args...)` — generic post-link step, `{output}` placeholder
- Shorthands: `AddPostLinkHex()`, `AddPostLinkBin()`, `AddPostLinkSize()`, `AddPostLinkStrip()`
- `AddBinHeader(inputs...)` — converts binary files to `.h` headers; output to `build/<tc>-<mode>/generated/`; include path auto-added; incremental via mtime
- RTOS tool accessors: `Package.ObjCopy()`, `Size()`, `ObjDump()`, `NM()`

### KConfig Preset Management (Firmware)

1. **Declare presets** in `OnConfig`: `ctx.KConfig("u-boot").AddPreset("rk3568_defconfig").SetDefault("sandbox_defconfig")`
2. **Select preset** via `vmake config` TUI — saves to `config.json`
3. **EnsureConfig**: Call `pkg.EnsureConfig(srcDir)` in `SetBuildFunc` — checks `.config` exists, runs `make <preset>` if missing, applies `PatchKConfig` patches
4. **PatchKConfig**: Override specific config options after defconfig: `ctx.KConfig("u-boot").PatchKConfig(map[string]string{"CONFIG_FOO=y"})`
5. **SetConfigFiles**: Register files that invalidate the stamp on change: `p.SetConfigFiles(".config")` (in `OnPackage`)

## Sub-Graph Build (Code Generation)

```go
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.BuildSubGraph("codegen")
    ctx.Exec(ctx.DepOutput("codegen:codegen"), "output/generated.h")
    ctx.DepBuildDir("codegen:codegen")

    ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
})
```

Use `ctx.ToolchainOption()` to allow per-package toolchain switching for sub-graph builds.

## Stamp-Based Skip (Void Targets)

Local void targets use `.vmake_stamp` in `BuildDir` for incremental builds. Stale when:
- Config files registered via `p.SetConfigFiles(".config")` are newer than the stamp
- The stamp file is deleted
- `.config` size becomes 0

Use `SetConfigFiles` on `*Package` (in `OnPackage`) to declare which files invalidate the stamp.

## Install

| Flag | Description |
|------|-------------|
| `--install` / `-i` | Install after build |
| `--prefix` / `-p` | Prefix (default: `./install/`) |
| `--install-type` | `runtime` (binaries+shared) or `sdk` (everything) |

Custom install entries: `ctx.AddInstalls("src/file.conf", "etc/file.conf")` (available in `OnBuild` and `OnInstall`).

## Build Scope

vmake builds packages by BFS from local (directory-based) packages. Remote packages are only built if reachable from a local package's transitive dependency chain. If you `AddRequires("pkg")` but no local package depends on it, the package won't be built.

## CLI Quick Reference

| Command | Description |
|---------|-------------|
| `vmake build` | Build |
| `vmake build --tests` | Build including test targets |
| `vmake test` | Build + run test targets |
| `vmake rebuild` | Clean + build |
| `vmake config` | TUI for options |
| `vmake clean` | Execute OnClean hooks then remove build artifacts |
| `vmake distclean` | Deep clean: artifacts + plugin cache + installed packages |
| `vmake query` | Dependency tree |
| `vmake toolchain list/show` | Toolchain info |
| `vmake repo add/list/remove/update` | Package repos |
| `vmake pkg list/search/clean/update` | Packages |
| `vmake ext add/list/remove/update` | Extension repos |
| `vmake manifest show/checkout` | Install manifest |
| `vmake git tag` | Version tagging |
| `vmake skill install/uninstall/path` | AI skill management |
| `vmake version` | Version info |

Build flags: `--force/-f`, `--mode`, `--toolchain`, `--install/-i`, `--prefix/-p`, `--install-type`, `--manifest`, `--tests`
Verbosity: `-v` verbose, `-V` very-verbose, `-q` quiet

## Reading Guide

- **Learning the basics** → Start with `examples/simple.md`, then `examples/config.md`
- **Writing a build.go** → Follow the Decision Guide above to pick the right example
- **Multi-module workspace** → `examples/multi-module.md`
- **Looking up a specific API** → See `references/api.md` for complete method signatures
- **CLI usage** → See `references/cli.md` for full command tree
- **Advanced patterns** → `examples/complete.md`, `examples/subbuild.md`, `examples/embedded-rtos.md`, `examples/firmware.md`

## Key Conventions

- Use `filepath.Join()` for filesystem paths
- Package IDs use `/`: `official/zlib`
- Target IDs use `:`: `lib:utils`
- `OnPackage` with `SetGit`/`AddVersion` works for both registry packages and local packages wrapping remote libraries
- `SetLanguages()` exists but has no effect — language is auto-detected from file extension
- For packages using `SetGit`, `AddFiles` paths resolve from `SourceDir()` — always prefix with `"src/"`
