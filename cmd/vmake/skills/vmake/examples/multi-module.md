# Multi-Module Workspace

Multiple packages in a single workspace, each with its own `build.go`.
Demonstrates cross-package dependencies with the `<pkg>:<target>` format.

## Prerequisites

- Run from the workspace root (the directory holding the top-level `build.go`)
- A C compiler for the host toolchain — `vmake doctor` checks toolchain and platform prerequisites

## Project Structure

```
myproject/
├── vmake.json            # Declares "roots": ["//", "//app"] (or ["//app"])
├── build.go              # Optional root ("//"): global options only
├── lib/
│   ├── build.go          # Static library package ("//lib")
│   ├── include/
│   │   └── utils.h
│   └── src/
│       └── utils.c
└── app/
    ├── build.go          # Application package ("//app")
    └── src/
        └── main.c
```

## Root vmake.json

```json
{
  "roots": ["//", "//app"]
}
```

## Root build.go (Global Options)

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.GlobalOption("debug").
            SetType(api.OptionBool).
            SetDefault(false).
            SetDescription("Enable debug mode")
    })
}
```

The root `build.go` (canonical ID `"//"`) defines options shared across all packages via `GlobalOption`. A plain `ctx.Option` is **package-local**: another package cannot read it by bare name, but it can read it explicitly as `"//:<option>"` in `OnBuild` or `OnConfigureDeps` (those are the cross-package read phases). If you don't need root-level global options, you can omit root `build.go` completely and set `"roots": ["//app"]` in `vmake.json`.

## lib/build.go

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("utils").
            SetKind(api.TargetStatic).
            AddFiles("src/*.c").
            AddPublicIncludes("include")
    })
}
```

`AddPublicIncludes("include")` makes the include directory available to any target that depends on `"//lib:utils"`.

## app/build.go

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.AddRequires("//lib")
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c").
            AddDeps("//lib:*")
    })
}
```

## Root Selection

vmake loads packages **lazily on demand** starting from **local roots**: a package is only loaded and built if it is listed as a root or reachable via `AddRequires`. See `SKILL.md - Build Scope`.

- Declare entry packages in `vmake.json` `"roots"` (e.g. `["//app"]` or `["//", "//app"]`). When a root `build.go` exists and `vmake.json` omits `roots`, `"//"` is the default root.
- Configurations under `configs/*.json` can override the entry point per profile via `"root": "//app"` or `"roots": ["//app"]`.
- BFS follows **package-level** dependencies declared with `AddRequires`. Every local or external dependency must be listed in `AddRequires` so the lazy loader loads it, plus wired into targets with `AddDeps` or `UseRequires()`.

## Package Naming and Discovery

- **Path is identity**: every local package ID is `"//"` (project root) or `"//<rel/path>"` (`lib/detail/build.go` → `"//lib/detail"`). There is no directory scanning and no basename collision risk.
- Unreferenced directories containing `build.go` are never loaded.

## Local vs External Dependencies

Both local (`"//lib"`) and external (`"official/zlib"`, `"myrepo/sdk/hal"`) dependencies follow the same rule: declare the pure package ID in `AddRequires` to load it into the dependency graph, and wire the target edge with `AddDeps("//lib:utils")` or `UseRequires()`. External module containers must also be declared in `vmake.json` `"modules"`. See `SKILL.md - Dependencies`.

If a package is not in the needed set (not a root and not reachable via `AddRequires`), its targets are never declared and a target referencing it fails at build-graph time with `dependency not found`.

## Path Resolution

`AddFiles` and `AddPublicIncludes` paths are resolved from `SourceDir()` (the `build.go` directory). `SetGit`/`SetSrcDir` change the layout: a local `SetGit` package downloads into `SourceDir()/src`, so prefix paths with `src/`, while registry/native packages build from the checkout root. See `references/dirs.md` for the full rules.

## Build Flow

```
1. Load vmake.json roots ("//", "//app") and follow AddRequires lazily ("//lib")
2. Phase 1: OnRequire discovery pass
3. Phase 2: OnConfig callbacks from resolved packages (global options merged)
4. Phase 3: FilterDeps (prune conditional branches)
5. Phase 4: OnBuild callbacks (only needed packages declare targets)
   - //lib: creates target "utils" (static library)
   - //app: creates target "app" (binary), depends on "//lib:utils"
6. Topological sort: //lib:utils → //app:app
7. Compile & link (all targets execute serially; source compilation within a target follows the jobs budget)
```

## Adding More Packages

```go
// app/build.go — depend on multiple packages
p.AddRequires("//lib", "//net", "//math")
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").
        SetKind(api.TargetBinary).
        AddFiles("src/*.c").
        AddDeps("//lib:utils", "//net:socket", "//math:vector")
})
```

Each dependency is a separate local package in its own directory. The `<pkg>:<target>` format ensures correct build order and propagates public includes.

## GlobalOption

One declaring package is enough. Multiple packages may declare the same `GlobalOption` only if `Type` and `SetExportDefine` match and `Default` matches, except that `chip` may leave one default unset (another fills it) and unions `SetValues`; `SetValues` must also match for the other non-builtin options — otherwise `MergeGlobalOptions` fails the build with a mismatch. Merging is deterministic: packages are processed in sorted name order and the first definition wins, so a later `SetDescription` does not replace an earlier one; a later non-empty `SetMacroName` is adopted only when the earlier definition left it empty. Prefer a single declaring package for shared metadata.

## OnConfig Caveats

`ctx.Bool/String/Int` are strict, per-package reads. In `OnConfig`, a package only sees the options it declared itself: reading another package's option (including its `GlobalOption`) fails with `unknown option` unless this package declares the same `GlobalOption` with identical `Type`/`Default` (and `SetValues` for non-builtin options). Bare-name global reads — all packages' global options plus configured values — work in `OnBuild`/`OnInstall`/`OnClean` contexts. In `OnBuild` and `OnConfigureDeps` you can also read another package's option explicitly as `<pkg>:<option>`; the value is that owner's effective configuration, and cross-package reads are unavailable in `OnConfig`, `OnRequire`, `OnInstall`, and `OnClean`. In `OnRequire`, use the discovery-aware `ctx.When`/`ctx.If`/`ctx.Select`.

## Running / Verifying

```bash
vmake build                    # "Build succeeded: ..."; //lib:utils builds before //app:app
vmake config --set debug=true  # non-interactive write to the active configuration
vmake doctor                   # checks toolchain/platform/manifest readiness
vmake query                    # dependency tree with targets and package dirs (native packages also show versions)
```

Expected: `vmake build -v` prints a `Build order:` list containing `//lib:utils` before `//app:app`; plain `vmake build` ends with a `Build succeeded: ...` summary.

## Key Points

- Package ID = canonical path (`"//"` at root, `"//lib"` for `lib/`, `"//drivers/spi"` for `drivers/spi/`)
- Cross-package deps use `:` separator: `"//lib:utils"` means target `"utils"` from package `"//lib"`
- Declare every cross-package dependency in `AddRequires` (lazy loading) plus `AddDeps` or `UseRequires()` (target edge)
- Same-package deps use just the target name: `AddDeps("utils")`
- Options that every package should read by bare name must be declared with `ctx.GlobalOption`; plain `ctx.Option` is package-local and can be read explicitly as `<pkg>:<option>` in `OnBuild` or `OnConfigureDeps`
- Declare multi-package workspace roots in `vmake.json` `"roots"` (or per-profile `"root"`/`"roots"` in `configs/*.json`)

## See Also

- references/api.md - Target, AddPublicIncludes, dependency format
- SKILL.md - Dependencies
- SKILL.md - Build Scope
- examples/multi-target.md - Multiple targets within a single package
