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

| Phase | Hook | When you need it |
|-------|------|-----------------|
| 1 | `OnRequire` | Third-party dependencies (git packages) |
| 2a | `ResolveDeferred` | Remote packages cloned, build.go compiled (automatic) |
| 2b | `OnConfig` | Build options (debug/release, features, etc.) |
| 3 | `OnBuild` | Always — define compilation targets |
| 4 | `OnInstall` | Custom install logic |

`OnPackage` runs for all packages right after `Main()` is called (before any lifecycle phases). Use it to describe the package (`SetDescription`, `SetLicense`, `SetHomepage`). `SetGit`/`AddVersion` inside `OnPackage` is only for **registry repo** packages — native repo and local packages must NOT use these.

## Decision Guide

- **New project, no options, no deps** → Only `OnBuild`. Start from `examples/simple.md`.
- **Need configurable features** → Add `OnConfig`. See `examples/config.md`.
- **Conditional compilation** → Options + `ctx.If()`/`ctx.Select()`. See `examples/conditional.md`.
- **Multiple targets (lib + binary + tests)** → See `examples/multi-target.md`.
- **Multi-module workspace (lib/ + app/ directories)** → See `examples/multi-module.md`.
- **Third-party packages** → `OnRequire` + `AddRequires` + `AddDeps`. See `examples/with-package.md`.
- **Wrap external C/C++ library (CMake/Autotools)** → `TargetVoid` + `SetBuildFunc`. See `examples/third-party-wrapper.md`.
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

### `SetGit`/`AddVersion` is registry-only

Local packages and native repo packages must NOT use `SetGit`/`AddVersion` in `OnPackage`. Only registry repo wrapper packages use these.

### `pkg.Run()` calls `os.Exit` on failure

`pkg.Run()` and `pkg.RunIn()` use `exec.RunFatal` internally — they never return a non-nil error. Only `pkg.RunEnv()` returns a real error that you should check.

### `vmake clean` vs `vmake distclean`

`vmake clean` removes build artifacts (compiled objects, binaries, libraries) but keeps the compiled `build.so` plugin and dependency cache. If you modify `build.go` (add/remove targets, change options, change includes) and the build seems to ignore your changes, use `vmake distclean` — it clears the plugin cache, build script cache, and all installed packages, forcing a full re-evaluation of the build graph.

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

## Directory Reference

| Property | What it returns | When to use |
|----------|----------------|-------------|
| `SourceDir()` | Package root (where build.go lives) | Package metadata files, overlay dirs |
| `SrcDir()` | Source code dir (`SourceDir()/src/` when `SetGit` downloads source) | Source files for firmware/third-party builds |
| `BuildDir()` | Scratch dir (`SourceDir()/build/<key>/`) | Intermediate artifacts, stamps |
| `InstallDir()` | Installation prefix | Headers/libs installed by third-party packages |
| `ScriptDir()` | Same as `SourceDir()` | Legacy alias |

Key distinction: for a registry package like U-Boot, `SourceDir()` is where `build.go` lives, but the actual U-Boot source is at `SrcDir()` (= `SourceDir()/src/`). For a local package without `SetGit`, `SourceDir()` == `SrcDir()`.

Within `SetBuildFunc`, built-in helpers (`CMakeConfigure`, `CMakeBuild`, `CMakeInstall`) automatically use the correct directories. If you need to read or patch source files manually, use `SrcDir()` to locate the downloaded source tree.

## Package Types

| Type | How identified | `OnPackage` metadata | Source code location |
|------|---------------|---------------------|---------------------|
| **Local** | build.go in project directory | `SetDescription`, `SetLicense` only | `SourceDir()` (same as build.go) |
| **Registry** | `vmake repo add name url` | `SetGit`, `AddVersion`, `SetLibs` required | `SrcDir()` (downloaded to `SourceDir()/src/`) |
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
    SetBuildFunc(func(p *api.Package) error { ... })
```

`AddPublicIncludes` implies `AddIncludes` — directories set via `AddPublicIncludes` are automatically available to the target itself and propagated to all dependents. There is no need to duplicate them with `AddIncludes`.

`AddFiles` accepts glob patterns and can be called with multiple globs to collect sources from different directories:

```go
AddFiles("src/common/*.c", "src/network/*.c", "src/stun/*.c")
```

Remove flags: `RemoveCFlags`, `RemoveDefines`, `RemoveIncludes`, etc.

Third-party packages with external build systems use `TargetVoid` with `SetBuildFunc`.

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

After all `OnBuild` callbacks execute, vmake runs `autoWireRequireDeps`. For each local target, for each `AddRequires` entry, it adds `AddDeps("pkg:target")` for ALL targets defined by that dependency package. This means:

- `AddRequires("busybox")` + busybox defines target `"busybox"` → all your targets automatically get `AddDeps("busybox:busybox")`
- You do NOT need explicit `AddDeps` when depending on a whole package via `OnRequire`

Explicit `AddDeps` IS needed for:
- Same-package target deps: `AddDeps("mylib")`
- Specific cross-package target: `AddDeps("lib:utils")`
- Third-party packages: both `AddRequires` (Phase 1) and `AddDeps` (Phase 3) needed

### Dependency Format

- `"utils"` — same-package target
- `"lib:utils"` — cross-package target (build order + link + PublicIncludes)
- `"official/zlib"` — third-party package (expanded to all targets from that package)

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
```

## RTOS / Embedded

```go
ctx.Target("firmware").
    SetKind(api.TargetBinary).
    AddFiles("src/*.c").
    SetLinkerScript("ld/stm32f4.ld").
    AddBinHeader("assets/logo.bin").
    AddPostLinkSize().
    AddPostLinkHex().
    AddPostLinkBin()
```

- `SetLinkerScript(path)` — passes `-T` to linker
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
| `vmake clean` | Remove build artifacts (objects, binaries) |
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
- `OnPackage` with `SetGit`/`AddVersion` is ONLY for registry repo packages
- `SetLanguages()` exists but has no effect — language is auto-detected from file extension
