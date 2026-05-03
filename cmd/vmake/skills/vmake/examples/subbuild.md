# BuildSubGraph and Code Generation

Demonstrates building a separate host tool as an independent sub-graph, then executing it to produce generated source files.

## build.go

```go
package main

import (
    "os"

    "gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.BuildSubGraph("tools")

        os.MkdirAll("output", 0755)
        ctx.Exec(ctx.DepOutput("tools:codegen"), "output/generated.h")

        ctx.Target("app").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c").
            AddCFlags("-I.")
    })
}
```

## What This Demonstrates

- **`ctx.BuildSubGraph(pkgName)`** - Build a package (and its deps) as an independent sub-graph
- **`ctx.DepOutput(depRef)`** - Get the output path of a dependency target
- **`ctx.Exec(binary, args...)`** - Run a built binary as build step
- **`ctx.DepBuildDir(depRef)`** - Get the build directory of a dependency target

## Use Cases

1. **Code generation**: Build a codegen tool, run it to generate headers, then compile main sources
2. **Cross-compilation**: Build host tools with native toolchain, target binaries with cross toolchain
3. **Build utilities**: Build helper tools (protoc, flex, bison) before main compile

## Configuring Toolchain

The sub-graph reads its toolchain from `vmake config`:

```json
{
  "entries": {
    "tools": {
      "options": {
        "toolchain": "host"
      }
    }
  }
}
```

Or declare it as an option in the tools package:

```go
// tools/build.go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.ToolchainOption()
})
```

## Project Structure

```
myproject/
├── build.go
├── tools/
│   ├── build.go          # Host tool package
│   └── src/
│       └── codegen.c     # Code generator source
├── src/
│   └── main.c            # Uses generated.h
└── output/
                        # Generated files go here
```

## Build Flow

```
Phase 1-3: Main build.go
    │
    ├── BuildSubGraph("tools")
    │       └── OnBuild for tools → collects targets
    │       └── NewBuildGraph → Scheduler with tools' toolchain
    │       └── builds tools/build/<buildKey>/codegen
    │
    ├── DepOutput("tools:codegen")
    │       └── Returns path: tools/build/<buildKey>/codegen
    │
    ├── Exec(path, "output/generated.h")
    │       └── Runs codegen to create generated.h
    │
    └── ctx.Target("app").AddFiles("src/*.c")
            └── Compiles using the generated header
```

## Key Points

- `BuildSubGraph` runs in-process, sharing config with the parent build
- Toolchain is read from the package's config entry (or global default)
- `DepOutput("pkg:target")` returns the deterministic output path
- `DepBuildDir("pkg:target")` returns the directory containing that output (useful for locating generated headers or other build artifacts)
- Targets built by the sub-graph are excluded from the main build graph

## Linking Sub-Graph Libraries

When a sub-graph produces a **static library** (not a codegen binary), pass its output path to the main target via `AddLdFlags`:

```go
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.BuildSubGraph("sublib")

    sublibPath := ctx.DepOutput("sublib:sublib")

    ctx.Target("app").
        SetKind(api.TargetBinary).
        AddFiles("src/*.c").
        AddLdFlags(sublibPath)
})
```

`DepOutput("sublib:sublib")` returns the path to `libsublib.a`. Adding it via `AddLdFlags` passes the full `.a` path directly to the linker.

**Why `AddDeps` won't work:** Sub-graph targets are excluded from the main build graph — the scheduler marks them as "built by subgraph" and skips them. If you write `AddDeps("sublib:sublib")`, the scheduler tries to compile it as part of the main graph AND the subgraph, hitting a double-build error or a target-not-found error. `AddLdFlags(DepOutput(...))` is the only way to link a subgraph-produced artifact.

### Nested Sub-Graphs

A sub-graph can itself call `BuildSubGraph` — the `DepOutput` function chains through parent contexts automatically:

```go
// tools/build.go — sub-graph package
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.BuildSubGraph("codegen")                      // nested subgraph
    ctx.Exec(ctx.DepOutput("codegen:gen"), "output/ids.h")  // resolves correctly

    ctx.Target("sublib").SetKind(api.TargetStatic).AddFiles("lib.c")
})

// build.go — root package
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.BuildSubGraph("tools")

    libPath := ctx.DepOutput("tools:sublib")  // resolves through nested subgraph

    ctx.Target("app").SetKind(api.TargetBinary).
        AddFiles("src/*.c").AddLdFlags(libPath)
})
```

If a dependency is inside the same subgraph, `DepOutput` computes its output locally. If it's in a different subgraph, it delegates to the parent's dep-output resolver. This fallback chain allows arbitrary nesting depth.

## See Also

- references/api.md - BuildContext methods
- examples/with-package.md - Package dependency pattern
