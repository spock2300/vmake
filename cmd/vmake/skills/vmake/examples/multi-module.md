# Multi-Module Workspace

Multiple packages in a single workspace, each with its own `build.go`.
Demonstrates cross-package dependencies with the `pkg:target` format.

## Prerequisites

- Run from the workspace root (the directory holding the top-level `build.go`)
- A C compiler for the host toolchain — `vmake doctor` checks toolchain and platform prerequisites

## Project Structure

```
myproject/
├── build.go              # Root: global options only
├── lib/
│   ├── build.go          # Static library package
│   ├── include/
│   │   └── utils.h
│   └── src/
│       └── utils.c
└── app/
    ├── build.go          # Application package
    └── src/
        └── main.c
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

The root `build.go` defines options shared across all sub-packages via `GlobalOption` (options declared with plain `ctx.Option` are **package-local** — a sub-package reading them is an unknown-option build error). It has no `OnBuild` — all targets are in sub-packages. It also declares no `SetRoot`, so this example relies on the default root heuristic (see Root Selection below). If you don't need shared options, the root `build.go` can be empty (`func Main(p *api.Package) {}`).

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

`AddPublicIncludes("include")` makes the include directory available to any target that depends on `"lib:utils"`.

## app/build.go

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c").
            AddDeps("lib:*")
    })
}
```

## Root Selection

vmake builds from **local roots**: a package is only built if it is reachable from a root. See `SKILL.md - Build Scope`.

- At most one package may declare an explicit root with `p.SetRoot(true)`. Two declarations fail with `multiple root packages found; only one is allowed`.
- When no package declares a root, vmake falls back to a heuristic and prints a hint. In this example nothing declares `OnRequire`, so every local package (the top-level package, `lib`, and `app`) becomes a root. `vmake doctor` reports the `noRoot` warning when no `SetRoot(true)` exists.
- BFS follows **package-level** dependencies declared with `AddRequires` only. `AddDeps` target edges do not pull a package into the build set.
- A top-level `build.go` that only declares options must therefore not be the **sole** root: with `SetRoot(true)` on it and no `AddRequires`, BFS reaches nothing else, no targets are declared, and the build succeeds with zero targets. Either leave the root implicit (as here), mark the real entry package (`app`) as root and list `lib` in its `AddRequires`, or have the options-only root declare the packages it wants built:

```go
// root/build.go — reach the entry package
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("app")
})

// app/build.go — reach the library it links
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("lib")
})
```

- With an explicit root, local dependencies also need `AddRequires`: `test_data/21_root_package` marks `app` with `SetRoot(true)` and declares `AddRequires("lib_a")` so `lib_a` is pulled into the build set.

## Package Naming and Discovery

- Every `build.go` found while scanning the project becomes a local package named after its directory **basename**: `lib/detail/build.go` → package `detail`, not `lib/detail`.
- The scan skips `build/`, `vendor/`, `.vmake_deps/`, `node_modules/`, `.git/`, `.vmake/`, and any other hidden directory.
- Duplicate names are silently dropped — only the first package with a given basename is loaded, later directories with the same name are ignored. Keep directory names unique.

## Local vs Remote Dependencies

All local `build.go` files are scanned and loaded up front, so a local dependency only needs a target edge (`AddDeps("lib:utils")`); `AddRequires` is not required for it unless an explicit root made it unreachable. Remote dependencies need both: `AddRequires` for resolution/download and `AddDeps` for the build-graph edge. See `SKILL.md - Dependencies`.

If a local package is not in the needed set (for example an explicit root does not require it), its targets are never declared and a target referencing it fails at build-graph time with `dependency not found` or `package not found in build graph`.

## Path Resolution

`AddFiles` and `AddPublicIncludes` paths are resolved from `SourceDir()` (the `build.go` directory). `SetGit`/`SetSrcDir` change the layout: a local `SetGit` package downloads into `SourceDir()/src`, so prefix paths with `src/`, while registry/native packages build from the checkout root. See `references/dirs.md` for the full rules.

## Build Flow

```
1. Scan: root/build.go, lib/build.go, app/build.go
2. Phase 1: No OnRequire — local packages loaded
3. Phase 2: Config callbacks from all packages (global options merged)
4. Phase 3: FilterDeps (no OnRequire declared — deps unchanged)
5. Phase 4: Build callbacks (only needed packages declare targets)
   - lib: creates target "utils" (static library)
   - app: creates target "app" (binary), depends on "lib:utils"
6. BFS from roots (all local packages here) → topological sort: lib:utils → app:app
7. Compile & link (all targets execute serially; source compilation within a target follows the jobs budget)
```

## Adding More Packages

```go
// app/build.go — depend on multiple packages
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").
        SetKind(api.TargetBinary).
        AddFiles("src/*.c").
        AddDeps("lib:utils", "net:socket", "math:vector")
})
```

Each dependency is a separate local package in its own directory. The `pkg:target` format ensures correct build order and propagates public includes.

## GlobalOption

One declaring package is enough. Multiple packages may declare the same `GlobalOption` only if `Type` and `Default` are identical — otherwise `MergeGlobalOptions` fails the build with a type/default mismatch. Prefer a single declaring package for `SetValues`/`SetDescription`: which definition supplies them in the merged view is unspecified.

## OnConfig Caveats

`ctx.Bool/String/Int` are strict, per-package reads. In `OnConfig`, a package only sees the options it declared itself: reading another package's option (including its `GlobalOption`) fails with `unknown option` unless this package declares the same `GlobalOption` with identical `Type`/`Default`. The merged global view — all packages' global options plus configured values — is available in `OnBuild`/`OnInstall`/`OnClean` contexts, so do cross-package reads there. In `OnRequire`, use the discovery-aware `ctx.When`/`ctx.If`/`ctx.Select`.

## Running / Verifying

```bash
vmake build                    # "Build succeeded!"; lib:utils builds before app:app
vmake config --set debug=true  # non-interactive write to the active configuration
vmake doctor                   # toolchain/platform findings + "[warn] ... (noRoot)" because this example declares no root
vmake query                    # dependency tree with targets, versions and package dirs
```

Expected: `vmake build` prints a `Build order:` list containing `lib:utils` before `app:app` and ends with `Build succeeded!`. `vmake doctor` prints the `noRoot` warning for this example; once a root is declared the warning disappears and the summary reports `OK: no issues` (or only unrelated platform findings).

## Key Points

- Package name = directory basename (e.g., `lib/` → package name `"lib"`); skipped directories and duplicate basenames are ignored
- Cross-package deps use `:` separator: `"lib:utils"` means target `"utils"` from package `"lib"`
- Local deps need only `AddDeps` (all local `build.go` files are loaded); remote deps need `AddRequires` + `AddDeps`
- Same-package deps use just the target name: `AddDeps("utils")`
- Options shared across packages must be declared with `ctx.GlobalOption` (identical `Type`/`Default` required from every declaring package); plain `ctx.Option` is package-local
- With an explicit `SetRoot(true)`, BFS starts only from that package — list every package you still want built in `AddRequires`

## See Also

- references/api.md - Target, AddPublicIncludes, dependency format
- SKILL.md - Dependencies
- SKILL.md - Build Scope
- examples/multi-target.md - Multiple targets within a single package
