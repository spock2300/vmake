# Multi-Module Workspace

Multiple packages in a single workspace, each with its own `build.go`.
Demonstrates cross-package dependencies with the `pkg:target` format.

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

The root `build.go` defines options shared across all sub-packages via `GlobalOption` (options declared with plain `ctx.Option` are **package-local** — a sub-package reading them is an unknown-option build error). It has no `OnBuild` — all targets are in sub-packages. If you don't need shared options, the root `build.go` can be empty (`func Main(p *api.Package) {}`).

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

## What This Demonstrates

- **Multi-package workspace** — Each sub-directory is an independent package with its own `build.go`
- **Root build.go** — Defines shared options, no targets of its own
- **`AddPublicIncludes`** — Include dirs propagated to dependents automatically
- **`AddDeps("lib:*")`** — Wildcard dependency: links all targets from `lib` package, propagates public includes
- **Global options** — The `debug` global option defined in root is accessible in all sub-packages via `ctx.Bool("debug")`; plain `ctx.Option` declarations are package-local

## Build Flow

```
1. Scan: root/build.go, lib/build.go, app/build.go
2. Phase 1: No OnRequire
3. Phase 2: Config callbacks from all packages (global options merged)
4. Phase 3: FilterDeps (no OnRequire declared — deps unchanged)
5. Phase 4: Build callbacks
   - lib: creates target "utils" (static library)
   - app: creates target "app" (binary), depends on "lib:utils"
6. Topological sort: lib:utils → app:app
7. Compile & link (packages build in parallel by default)
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

Each dependency is a separate package in its own directory. The `pkg:target` format ensures correct build order and propagates public includes.

## Key Points

- Package name = directory name (e.g., `lib/` → package name `"lib"`)
- Cross-package deps use `:` separator: `"lib:utils"` means target `"utils"` from package `"lib"`
- `AddPublicIncludes` on the library target makes its includes available to dependents
- Same-package deps use just the target name: `AddDeps("utils")`
- Options shared across packages must be declared with `ctx.GlobalOption` (identical `Type`/`Default` required from every declaring package); plain `ctx.Option` is package-local

## See Also

- references/api.md - Target, AddPublicIncludes, dependency format
- SKILL.md - Dependencies section
- examples/multi-target.md - Multiple targets within a single package
