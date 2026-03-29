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
| 2 | `OnConfig` | Build options (debug/release, features, etc.) |
| 3 | `OnBuild` | Always — define compilation targets |
| 4 | `OnInstall` | Custom install logic |

`OnPackage` runs for all packages (local and remote). Use it to describe the package (`SetDescription`, `SetLicense`, `SetHomepage`). `SetGit`/`AddVersion` inside `OnPackage` is only for index repo packages — prefix repo and local packages should NOT use these.

## Decision Guide

- **New project, no options, no deps** → Only `OnBuild`. Start from `examples/simple.md`.
- **Need configurable features** → Add `OnConfig`. See `examples/config.md`.
- **Conditional compilation** → Options + `ctx.If()`/`ctx.Select()`. See `examples/conditional.md`.
- **Multiple targets (lib + binary + tests)** → See `examples/multi-target.md`.
- **Multi-module workspace** → Cross-package deps with `pkg:target` format. See `examples/multi-target.md`.
- **Third-party packages** → `OnRequire` + `AddRequires` + `AddDeps`. See `examples/with-package.md`.
- **Wrap external C/C++ library (CMake/Autotools)** → `TargetVoid` + `SetBuildFunc`. See `examples/third-party-wrapper.md`.
- **Code generation / host tools** → `BuildSubGraph` + `DepOutput` + `Exec`. See `examples/subbuild.md`.
- **Embedded / RTOS firmware** → `SetLinkerScript` + `AddPostLink*` + `AddBinHeader`. See RTOS section below.

## Build Script Template

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        // Phase 2: Define build options (optional)
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        // Phase 3: Define targets (required)
    })
}
```

## Key Patterns & Gotchas

### Fluent API
All setters return the receiver for chaining:
```go
ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
```

### Conditional expressions return slices
`ctx.If()` and `ctx.Select()` return `[]string` — spread with `...`:
```go
AddCFlags(ctx.If("debug", "-g", "-O0")...)   // correct
AddCFlags(ctx.If("debug", "-g", "-O0"))      // WRONG — compile error
```

### `AddXxx` methods accept strings and []string
```go
AddFiles("src/main.c", "src/utils.c")         // individual strings
AddFiles("src/*.c")                            // glob pattern
files := []string{"a.c", "b.c"}
AddFiles(files)                                // slice (via ...any)
```

### Third-party deps require TWO steps
`AddDeps` alone is not enough — you must also declare the dependency:
```go
// Step 1: Declare (Phase 1)
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("official/zlib >=1.2")
})

// Step 2: Use (Phase 3)
ctx.Target("app").AddDeps("official/zlib")
```

### Prefix repo vs Index repo
- **Index repo**: `build.go` wraps external C/C++ libs using `OnPackage` + `SetGit` + `AddVersion`
- **Prefix repo**: `build.go` is a normal build descriptor, NO `SetGit`/`AddVersion` — system handles automatically

## Target API at a Glance

```go
ctx.Target("app").
    SetKind(api.TargetBinary).           // Binary/Static/Shared/Object/Void
    AddFiles("src/*.c").                 // Source files (globs, strings, []string)
    AddIncludes("include").              // Include directories
    AddPublicIncludes("include").        // Propagated to dependents
    AddDefines("DEBUG=1").               // Preprocessor defines
    AddCFlags("-Wall").                  // C compiler flags
    AddCxxFlags("-stdlib=libc++").       // C++ compiler flags
    AddLdFlags("-lm").                   // Linker flags
    AddLinks("ssl", "crypto").           // Libraries to link
    AddDeps("lib:utils").                // Dependencies (pkg:target / pkg/name / local)
    SetDefault(false).                   // Exclude from default build
    SetLanguages("c++17").               // C/C++ standard
```

Remove flags: `RemoveCFlags`, `RemoveDefines`, `RemoveIncludes`, etc.

Third-party packages with external build systems (CMake, Autotools, etc.) use `TargetVoid` with `SetBuildFunc` to provide custom build logic.

## RTOS / Embedded

```go
ctx.Target("firmware").
    SetKind(api.TargetBinary).
    AddFiles("src/*.c").
    SetLinkerScript("ld/stm32f4.ld").
    AddBinHeader("assets/logo.bin").      // Binary → .h hex header (auto-included)
    AddPostLinkSize().                    // Print size info
    AddPostLinkHex().                     // Generate .hex
    AddPostLinkBin()                      // Generate .bin
```

- `SetLinkerScript(path)` — passes `-T` to linker
- `AddPostLink(tool, args...)` — generic post-link step, `{output}` placeholder
- Shorthands: `AddPostLinkHex()`, `AddPostLinkBin()`, `AddPostLinkSize()`, `AddPostLinkStrip()`
- `AddBinHeader(inputs...)` — converts binary files to `.h` headers; output to `build/<tc>-<mode>/generated/`; include path auto-added; incremental via mtime
- RTOS tool accessors: `Package.ObjCopy()`, `Size()`, `ObjDump()`, `NM()`

## Sub-Graph Build (Code Generation)

```go
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.BuildSubGraph("codegen")                      // Build codegen + deps
    ctx.Exec(ctx.DepOutput("codegen:codegen"), "output/generated.h")

    ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
})
```

Use `ctx.ToolchainOption()` to allow per-package toolchain switching for sub-graph builds (e.g., building a code generator with the host toolchain while cross-compiling firmware with an embedded toolchain).

## Package Dependencies

**Consuming a package:**
```go
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("official/zlib >=1.2")
})
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").AddDeps("official/zlib")
})
```

**Defining a package (index repo):**
```go
p.OnPackage(func(p *api.Package) {
    p.SetGit("https://github.com/madler/zlib.git")
    p.AddVersion("1.2.13", "v1.2.13")
    p.SetLibs("z")
})
```

## Unified Dependency Format

`AddDeps` handles all types:
- `"utils"` — same-package target
- `"lib:utils"` — cross-package target (build order + link + PublicIncludes)
- `"official/zlib"` — third-party package (must also be in `OnRequire`)

Package refs (containing `/`) are expanded into all targets defined by that package plus its transitive dependency targets.

## Option & Conditional

```go
// Define (OnConfig)
ctx.Option("debug").SetType(api.OptionBool).SetDefault(false)

// Use (OnBuild)
AddCFlags(ctx.If("debug", "-g", "-O0")...)        // bool → flags
AddCFlags(ctx.IfNot("debug", "-O2")...)           // inverted
AddCFlags(ctx.Select("opt", map[string]string{    // choice → flag
    "O0": "-O0", "O2": "-O2",
}))...

ctx.String("name")    // read string
ctx.Int("count")      // read int
ctx.Bool("debug")     // read bool
ctx.When("x", "val")  // compare → bool (for if statements)
```

## Install

| Flag | Description |
|------|-------------|
| `--install` / `-i` | Install after build |
| `--prefix` / `-p` | Prefix (default: `./install/`) |
| `--install-type` | `runtime` (binaries+shared) or `sdk` (everything) |

Custom install entries: `p.AddInstalls("src/file.conf", "etc/file.conf")`

## CLI Quick Reference

| Command | Description |
|---------|-------------|
| `vmake build` | Build |
| `vmake rebuild` | Clean + build |
| `vmake config` | TUI for options |
| `vmake clean` | Remove artifacts |
| `vmake query` | Dependency tree |
| `vmake toolchain list/show` | Toolchain info |
| `vmake repo add/list/remove/update` | Package repos |
| `vmake pkg list/search/clean/update` | Packages |
| `vmake manifest show/checkout` | Install manifest |
| `vmake git tag` | Version tagging |
| `vmake skill install/uninstall/path` | AI skill management |
| `vmake version` | Version info |

Build flags: `--force/-f`, `--mode`, `--toolchain`, `--install/-i`, `--prefix/-p`, `--install-type`
Verbosity: `-v` verbose, `-V` very-verbose, `-q` quiet

## Reading Guide

- **Learning the basics** → Start with `examples/simple.md`, then `examples/config.md`
- **Writing a build.go** → Follow the Decision Guide above to pick the right example
- **Looking up a specific API** → See `references/api.md` for complete method signatures
- **CLI usage** → See `references/cli.md` for full command tree
- **Advanced patterns** → `examples/complete.md` (full API demo), `examples/subbuild.md` (code gen)

## Key Conventions

- Use `filepath.Join()` for filesystem paths
- Package IDs use `/`: `official/zlib`
- Target IDs use `:`: `lib:utils`
- `OnPackage` with `SetGit`/`AddVersion` is ONLY for index repo packages
