---
name: vmake
description: >
  VMake C/C++ build system assistant. Use when writing build.go files,
  configuring build options, managing third-party packages, or using
  vmake CLI commands.
---

# VMake AI Skill

VMake is a Go-plugin-based C/C++ build system. Build instructions are written in Go (`build.go`) using a fluent API.

## What is VMake

VMake compiles `build.go` into a Go plugin (`.so`) and executes it through a multi-phase lifecycle. It's an alternative to CMake/Meson/Bazel, but uses Go as the configuration language.

Key concepts:
- **Fluent API**: All setters return the receiver for chaining
- **Option system**: Build-time options with bool/string/int/choice types
- **Conditional expressions**: Adapt flags based on option values
- **Package management**: Third-party packages via Git repositories

## Build Script Template

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        // Phase 2: Define build options
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        // Phase 3: Define targets
    })
}
```

## Lifecycle Phases

| Phase | Hook | Purpose |
|-------|------|---------|
| 1 | `OnRequire` | Declare third-party dependencies |
| 2 | `OnConfig` | Define build options |
| 3 | `OnBuild` | Generate build targets |
| 4 | `OnInstall` | Post-build install logic |

`OnPackage` runs during plugin extraction for third-party packages.

## Target Quick Reference

```go
ctx.Target("app").
    SetKind(api.TargetBinary).           // Binary/Static/Shared/Object/Void
    AddFiles("src/*.c").                 // Source files (globs supported)
    AddIncludes("include").              // Include directories
    AddPublicIncludes("include").        // Propagated to dependents
    AddDefines("DEBUG=1").               // Preprocessor defines
    AddCFlags("-Wall").                  // C compiler flags
    AddCxxFlags("-stdlib=libc++").       // C++ compiler flags
    AddLdFlags("-lm").                   // Linker flags
    AddLinks("ssl", "crypto").           // Libraries to link
    AddDeps("lib:utils").                // Dependencies (pkg:target for cross-package, pkg/name for third-party)
    SetDefault(false).                   // Exclude from default build
```

Remove flags: `RemoveCFlags`, `RemoveDefines`, `RemoveIncludes`, etc.

Third-party packages with external build systems (CMake, Autotools, etc.) use `TargetVoid` with `SetBuildFunc` to provide custom build logic.

## Option & Conditional

**Define options in OnConfig:**
```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Option("debug").
        SetType(api.OptionBool).
        SetDefault(false).
        SetDescription("Enable debug mode")
    
    ctx.Option("optimization").
        SetType(api.OptionChoice).
        SetDefault("O2").
        SetValues("O0", "O1", "O2", "O3")
})
```

**Use conditional expressions in OnBuild:**
```go
AddCFlags(ctx.If("debug", "-g", "-O0")...)        // If bool is true
AddCFlags(ctx.IfNot("debug", "-O2")...)           // If bool is false
AddCFlags(ctx.Select("optimization", map[string]string{
    "O0": "-O0", "O2": "-O2",
}))                                              // Map option value
```

## Package Dependencies

**In consuming package:**
```go
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("official/zlib >=1.2")
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").AddDeps("official/zlib")
})
```

**In third-party package (defines metadata):**
```go
p.OnPackage(func(p *api.Package) {
    p.SetGit("https://github.com/madler/zlib.git")
    p.AddVersion("1.2.13", "v1.2.13")
    p.SetLibs("z")
})
```

## Unified Dependencies

`AddDeps` handles all dependency types:
- `AddDeps("utils")` — same-package target
- `AddDeps("lib:utils")` — cross-package target (controls build order, links artifact, propagates PublicIncludes)
- `AddDeps("official/zlib")` — third-party package (declared via `OnRequire` + `AddRequires`; transitive deps automatically resolved)

Package refs (containing `/`) are expanded into all targets defined by that package plus its transitive dependency targets.

## Multi-Module Projects

Cross-package dependencies use `package:target` format:
```go
// In app/build.go
ctx.Target("app").AddDeps("lib:utils")
```

Public includes are automatically propagated:
```go
// In lib/build.go
ctx.Target("utils").
    SetKind(api.TargetStatic).
    AddPublicIncludes("include")  // Consumers automatically get this
```

## CLI Quick Reference

| Command | Description |
|---------|-------------|
| `vmake build` | Build the project |
| `vmake rebuild` | Clean and rebuild |
| `vmake config` | Open TUI for option management |
| `vmake clean` | Remove build artifacts |
| `vmake repo add <name> <url>` | Add package repository |
| `vmake pkg list` | List installed packages |
| `vmake ext list` | List extensions |
| `vmake toolchain list` | Show available toolchains |
| `vmake query` | Show dependency tree (AI integration) |

Flags: `-v` verbose, `-V` very-verbose, `-q` quiet.

## For More Details

- **API Reference**: See `references/api.md` for complete API documentation
- **CLI Reference**: See `references/cli.md` for full CLI command tree
- **Examples**: See `examples/` for annotated build.go files
- **Runtime docs**: Run `vmake doc` (if installed) for live documentation

## Key Conventions

- Use `filepath.Join()` for filesystem paths
- Package IDs use `/`: `official/zlib`
- Target IDs use `:`: `lib:utils`
- All public API returns receiver for chaining