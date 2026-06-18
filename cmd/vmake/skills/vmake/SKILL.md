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
- **Config options → C compiler defines (-D flags)** → Three mechanisms. See `examples/config-to-define.md`.
- **Multiple targets (lib + binary + tests)** → See `examples/multi-target.md`.
- **Multi-module workspace (lib/ + app/ directories)** → See `examples/multi-module.md`.
- **Third-party packages** → `OnRequire` + `AddRequires` + `AddDeps`. See `examples/with-package.md`.
- **Wrap external C/C++ library (CMake/Autotools)** → `TargetVoid` + `SetBuildFunc`. See `examples/third-party-wrapper.md`.
- **Pre-compiled libraries (.a/.so)** → `SetPrebuilt`. See `examples/prebuilt.md`.
- **Cross-package config propagation (GenerateConfigDefines, ExportConfig, ImportConfig)** → See `examples/config-propagate.md`.
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

When a package uses `SetGit`, `SourceDir()` and `SrcDir()` differ — all `AddFiles` / `AddIncludes` / `AddPublicIncludes` paths resolve from `SourceDir()`, so you must prefix with `"src/"`. See `references/dirs.md` for full rules, edge cases, and the correct code pattern.

### Static library deps with symbols not referenced by your code

vmake wraps `AddDeps` archives in `--start-group`/`--end-group`. If a static lib dep provides symbols only referenced by post-group libraries (e.g., libc from `-specs`), the linker won't pull the archive. Fix with `-nostdlib` + `AddGlobalLinks`. See `references/gotchas.md` for all three fix patterns with code.

### `pkg.Run()` calls `os.Exit` on failure

`pkg.Run()` and `pkg.RunIn()` use `exec.RunFatal` internally — they never return a non-nil error. Only `pkg.RunEnv()` returns a real error that you should check.

### `vmake clean` vs `vmake distclean`

`vmake clean` runs `OnClean` hooks then removes build artifacts (objects, binaries); keeps `build.so` and `vmake_deps/`.

`vmake distclean` removes all local build dirs, build.so, go.mod/go.sum, install/, and `vmake_deps/` (symlinks only — `~/.vmake/sources/` is preserved). Use when modifying `build.go` and the build ignores your changes.

### Patching source before build in registry packages

To patch downloaded source inside `SetBuildFunc`, use Go's `os.ReadFile` + `os.WriteFile` (simple single-line changes) or `AddPatches("patches/fix.patch")` in `OnPackage` (multi-file git patches with deduplication). See `references/gotchas.md` for code examples of both patterns.

### `ctx.Select()` returns `""` during discoverAll — guard before passing to global flags

vmake runs an internal `discoverAll` phase where `ctx.Select()` returns `""`. If passed to `AddGlobalCFlags/LdFlags` in `SetOnApply`, the empty string persists in the global singleton and GCC interprets it as a filename. **Fix:** guard with `if optFlag != ""` before appending. See `references/gotchas.md` for the full explanation and code.

## Directory Reference

| Property | What it returns | When to use |
|----------|-----------------|-------------|
| `SourceDir()` | Package root (where build.go lives) | Package metadata files, overlay dirs |
| `SrcDir()` | Source code dir (`SourceDir()/src/` when `SetGit` downloads source, falls back to `SourceDir()`) | Source files for firmware/third-party builds |
| `BuildDir()` | Scratch dir for intermediate artifacts | Build outputs, stamps |
| `InstallDir()` | Installation prefix | Headers/libs installed by third-party packages |

For `BuildKey` naming, `SourceDir` vs `SrcDir` distinction, and `SetGit` path resolution rules, see `references/dirs.md`.

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

Remove flags: `RemoveCFlags`, `RemoveDefines`, `RemoveIncludes`, etc. These perform **immediate exact-match deletion** from internal slices — calling `RemoveCFlags("-Wall")` after `AddCFlags("-Wall")` removes the flag instantly.

`RemoveFiles` works differently: it stores patterns and applies them at build time against **glob-expanded paths**, not against the raw `AddFiles` argument strings. This deferred matching means:
- `AddFiles("src/*.c").RemoveFiles("src/test_*.c")` — works (globs expand, patterns match expanded paths)
- `AddFiles("src/main.c", "src/test.c").RemoveFiles("src/test.c")` — works even though no glob was used (patterns match the final file paths, not the AddFiles strings)
- `RemoveFiles` does NOT remove entries from the `AddFiles` rule list — it adds exclusion patterns to a separate filter applied during compilation

This is the only Remover method that uses deferred matching; all others (`RemoveCFlags`, `RemoveDeps`, `RemoveLinks`, `RemoveProvidedLibs`, etc.) delete immediately.

Third-party packages with external build systems use `TargetVoid` with `SetBuildFunc`. The callback function `func(p *api.Package) error` returns a real error — unlike `pkg.Run()` (which calls `os.Exit`), `SetBuildFunc` errors are returned to the scheduler and fail the build gracefully. Use `return fmt.Errorf(...)` for controlled failure, `return nil` for success.

## Prebuilt Libraries

Use `SetPrebuilt(path)` on `TargetStatic`, `TargetShared`, or `TargetBinary` to export a pre-compiled artifact. The scheduler creates a symlink from the expected output path (no copy, zero disk overhead). Incremental: compares symlink target, recreates only if path changed. Multiple libraries: one target per `.a`/`.so`.

```go
ctx.Target("drv").SetKind(api.TargetStatic).
    SetPrebuilt(filepath.Join(p.SourceDir(), "lib", "libdrv.a")).
    AddPublicIncludes("include").AddProvidedLibs("drv")
```

See `examples/prebuilt.md` for full patterns, `AddProvidedLibs`, shared libraries, and when to use `SetPrebuilt` vs `TargetVoid`+`SetBuildFunc`.

## Test Targets

Mark targets with `SetTest(true)`. Test targets are excluded from `vmake build` by default; `vmake build --tests` includes them; `vmake test` builds and runs `TargetBinary` tests, reporting pass/fail with timing.

```go
ctx.Target("tests").SetKind(api.TargetBinary).SetTest(true).
    AddFiles("tests/*.c").AddDeps("mylib")
```

Always define test targets unconditionally in `OnBuild` — `SetTest(true)` controls visibility, not option guards. Test targets are never installed and can depend on other test targets. See `examples/multi-target.md`.

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

After all `OnBuild` callbacks execute, `autoWireRequireDeps` auto-adds `AddDeps("pkg:target")` for ALL targets defined by each `AddRequires` dependency package. So `AddRequires("busybox")` + busybox's target `"busybox"` → all your targets automatically get `AddDeps("busybox:busybox")`. You do NOT need explicit `AddDeps` when depending on a whole package via `OnRequire`.

Explicit `AddDeps` IS needed for: same-package deps (`"mylib"`), specific cross-package targets (`"lib:utils"`), wildcard deps (`"chip:*"`), or selective third-party targets (`"official/zlib:target"`).

### Dependency Format

- `"utils"` — same-package target
- `"lib:utils"` — specific cross-package target (build order + link + PublicIncludes)
- `"lib:*"` or `"official/zlib:*"` — wildcard: all targets from that package + transitive deps
- `"official/zlib"` — third-party package (expanded to all targets from that package)

### Version Constraints

AddRequires accepts semver constraints: `"official/zlib >=1.2"`, `"official/curl ~8.5"`, `"test_build/mathlib"` (no constraint = any version).

Operators: `>=` (major-locked), `>` / `<=` / `<` (no major lock), `=` (exact), `~` (major.minor lock). Highest satisfying version is selected; multi-package constraints must be mutually satisfiable. See `references/api.md` for the full operator table and major lock semantics.

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

**Mechanism:** `FilterDeps` re-runs `OnRequire` for every package with real config values, replacing `node.Deps`. Then topology is re-sorted and needed packages collected via BFS from local roots.

**Key implication:** `AddRequires` alone does not guarantee a package is built — it must be reachable from a local root via BFS.

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

### OptionChoice Generates Dual Macros

When `GenerateConfigDefines` or `GenerateConfigHeader` processes a `Choice` option, it produces **two** entries:

- `CONFIG_{NAME}="<value>"` — the selection itself
- `CONFIG_{NAME}_{VALUE}=1` — a boolean for the specific choice

For example, `OptionChoice("platform").SetValues("linux", "windows")` with value `"linux"` generates:
```
CONFIG_PLATFORM="linux"
CONFIG_PLATFORM_LINUX=1
```

This lets code use either `#if CONFIG_PLATFORM_LINUX` (specific check) or switch on `CONFIG_PLATFORM` (general check). Note: `-D` defines use comma-delimited `#define` syntax (e.g., `-DCONFIG_PLATFORM_LINUX`), while `autoconf.h` uses `#define CONFIG_PLATFORM_LINUX 1`.

### SetConfigValue: Programmatic Override

`ctx.SetConfigValue(name, val)` in `OnConfig` changes an option value programmatically. Unlike `SetOnApply` (which only reacts), `SetConfigValue` changes the value that other parts of `OnConfig` will see:

```go
if ctx.String("chip") == "stm32f4" {
    ctx.SetConfigValue("use_fpu", true)
}
```

### Global Flags & Mode Flags

`AddGlobalCFlags/CxxFlags/LdFlags/Links` are only available on `ConfigContext`, effective inside `SetOnApply`. They apply to ALL targets in ALL packages and are deduplicated.

The compile merge order is: per-target flags → mode flags → global flags → dedup. Additionally, global CFlags are prepended before the resolved list: `[globalCFlags prepended] [per-target + mode + global deduped]`. For GCC, the last occurrence of repeated flags (`-O`) wins — mode overrides per-target, globals override mode unless dedup eliminates the global copy.

Mode auto-injected flags (injected by scheduler, not via `AddGlobalCFlags`):

| Mode | Flags injected |
|------|---------------|
| `release` | `-O2 -DNDEBUG` |
| `debug` | `-O0 -g` |

During linking, global LD flags are appended after per-target flags; global links go inside `--start-group`/`--end-group`.

See `references/gotchas.md` for the `ctx.Select()` empty-string guard when using option-dependent values in `SetOnApply`.

### GlobalOption Cross-Package Consistency

If two packages define the same global option via `GlobalOption()`, their `Type` and `Default` must be **identical** — otherwise the build fails with a fatal error. This constraint ensures all packages agree on the option's meaning. For example, if `chip/build.go` defines `GlobalOption("mcu").SetType(api.OptionString).SetDefault("stm32f405")` and `bsp/build.go` defines `GlobalOption("mcu").SetType(api.OptionChoice)`, the build will fail with a type mismatch error.

Only one package needs to define a global option's `SetValues` for `OptionChoice` — values from multiple definitions are merged. `SetDescription` and `SetOnApply` are also merged across packages.

### Toolchain DefaultFlags

Each toolchain declares default C/C++/linker flags in its manifest. These are injected as the **base** of every new target — when you call `ctx.Target("app")`, the target's initial CFlags/CxxFlags/LdFlags are set from the toolchain's defaults. Calling `AddCFlags(...)` appends to this base.

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

For real embedded projects, the chip/HAL package compiles startup code as a static library and sets global compiler/linker flags via `AddGlobalCFlags`/`AddGlobalLdFlags` in `SetOnApply`. See `examples/embedded-rtos.md` for the complete two-package pattern (chip + firmware with `UseDependencyLinkerScript`, post-link steps, `AddBinHeader`).

Key embedded rules: (1) Target-specific flags must appear in both CFLAGS and LDFLAGS to avoid ABI mismatches. (2) Use `-nostdlib` + `AddGlobalLinks("c_nano", "gcc")` in `SetOnApply` — this places libc inside `--start-group`/`--end-group` so arc dep symbols resolve. (3) `-specs=nano.specs` links libc after the group; if a dep provides symbols only libc references, use `EXTERN` in the linker script. See `references/gotchas.md` for the full explanation.

- `SetProvidedLinkerScript(path)` — chip/bsp declares linker script for consumers (fatal on double-set)
- `UseDependencyLinkerScript()` — firmware target auto-inherits `-T` from first dependency that provides one
- `SetLinkerScript(path)` — direct linker script on target (fatal on double-set)
- `AddPostLink(tool, args...)` — generic post-link, shorthands: `AddPostLinkHex/Bin/Size/Strip`
- `AddBinHeader(inputs...)` — binary files → `.h` headers
- RTOS tool accessors: `Package.ObjCopy()`, `Size()`, `ObjDump()`, `NM()`

### KConfig Preset Management (Firmware)

Use `ctx.KConfig("u-boot").AddPreset("rk3568_defconfig").SetDefault(...)` in `OnConfig`; select via `vmake config` TUI; call `pkg.EnsureConfig(srcDir)` in `SetBuildFunc`; use `PatchKConfig(map[string]string{...})` for post-defconfig overrides; register config files with `p.SetConfigFiles(".config")`. See `examples/firmware.md` for the full multi-package firmware pattern.

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

Local void targets use `.vmake_stamp` in `BuildDir` for incremental builds. Stale when the **content hash** (SHA-256) of files registered via `p.SetConfigFiles(".config")` changes, the git HEAD revision changes, or the stamp file is deleted. Mtime is NOT checked — only SHA-256 content hash and git commit hash.

Use `SetConfigFiles` on `*Package` (in `OnPackage`) to declare which files invalidate the stamp.

**InstallDir changes the skip mechanism entirely.** When a void target has `InstallDir` set (remote packages, or `p.CMakeInstall()` in `SetBuildFunc`), the scheduler checks whether `InstallDir` exists and contains files — if it does, the target is skipped. `.vmake_stamp` is **not** consulted. Force rebuild by deleting the install directory or running `vmake clean --all`.

## Install

| Flag | Description |
|------|-------------|
| `--install` / `-i` | Install after build |
| `--prefix` / `-p` | Prefix (default: `./install/`) |
| `--install-type` | `runtime` (binaries+shared) or `sdk` (everything) |

Custom install entries: `ctx.AddInstalls("src/file.conf", "etc/file.conf")` (available in `OnBuild` and `OnInstall`).

### OnInstall Lifecycle

`OnInstall` runs after all builds succeed and targets are installed. Use `ctx.SetPrefix()` for per-package prefix overrides and `ctx.AddInstalls()` for post-install file copies (docs, configs, licenses). See `examples/on-install.md`.

## Build Scope

vmake builds packages by BFS from local (directory-based) packages. Remote packages are only built if reachable from a local package's transitive dependency chain. If you `AddRequires("pkg")` but no local package depends on it, the package won't be built.

## Reproducible Builds (--manifest)

For CI/CD reproducibility, pin package versions in a manifest file and pass `--manifest` to `vmake build`:

```bash
# First build: create manifest recording exact versions/revisions
vmake build --install -i --manifest install.json

# Later build: restore exact versions from manifest
vmake build --manifest install.json
```

The manifest records git remote URLs, refs, and revisions for every package. `vmake manifest show install.json` displays its contents; `vmake manifest checkout install.json` restores sources to the recorded revisions without building.

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
- **Mapping config to defines** → `examples/config-to-define.md` (three mechanisms compared)
- **Multi-module workspace** → `examples/multi-module.md`
- **OnClean / OnInstall lifecycles** → `examples/on-clean.md`, `examples/on-install.md`
- **Looking up a specific API** → See `references/api.md` for complete method signatures
- **CLI usage** → See `references/cli.md` for full command tree
- **Directory / path resolution details** → `references/dirs.md` (BuildKey, SetGit paths, SourceDir vs SrcDir)
- **Advanced gotchas** → `references/gotchas.md` (static lib deps, discoverAll guard, source patching)
- **Advanced patterns** → `examples/complete.md`, `examples/subbuild.md`, `examples/embedded-rtos.md`, `examples/firmware.md`, `examples/config-propagate.md`, `examples/prebuilt.md`, `examples/third-party-wrapper.md`

## Key Conventions

- Use `filepath.Join()` for filesystem paths
- Package IDs use `/`: `official/zlib`
- Target IDs use `:`: `lib:utils`
- `OnPackage` with `SetGit`/`AddVersion` works for both registry packages and local packages wrapping remote libraries
- `SetLanguages()` exists but has no effect — language is auto-detected from file extension
- For packages using `SetGit`, `AddFiles` paths resolve from `SourceDir()` — always prefix with `"src/"`
