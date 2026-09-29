# BuildSubGraph and Code Generation

Demonstrates building a separate host tool as an independent sub-graph, then executing it to produce generated source files.

## Prerequisites

- The sub-graph package must be reachable in the dependency graph. Without `SetRoot(true)` every local package is a root, so `tools/` is included automatically; with an explicit root, declare it with `ctx.AddRequires("tools")` in `OnRequire`.
- The sub-graph package's own dependencies must resolve and build (fixture `test_data/07_subbuild_codegen` adds `official/tinyexpr` to the codegen binary).
- A working toolchain for the tool package — typically `host` selected via `ctx.ToolchainOption()`/`vmake config` while the main package cross-compiles. The sub-graph builds with the tool package's own toolchain.

## build.go

```go
package main

import (
    "os"

    "github.com/spock2300/vmake/pkg/api"
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
    │       └── runs tools' OnBuild and builds its targets synchronously
    │       └── builds tools/build/<buildKey>/codegen with tools' toolchain
    │
    ├── DepOutput("tools:codegen")
    │       └── Returns path: tools/build/<buildKey>/codegen
    │
    ├── Exec(path, "output/generated.h")
    │       └── Runs codegen with cwd = the current package's SourceDir
    │
    └── ctx.Target("app").AddFiles("src/*.c")
            └── Compiles using the generated header
```

## Key Points

- `BuildSubGraph` runs synchronously and in-process, sharing the parent invocation's session and already-completed targets: a target built by the sub-graph is not rebuilt when the main graph reaches it.
- The sub-graph package must be in the needed/required set. Without `SetRoot(true)` every local package is treated as a root, so this example works as written. Under an explicit `p.SetRoot(true)` the root must declare `ctx.AddRequires("tools")` in `OnRequire`; local nested `build.go` files are registered under their **directory basename** (`tools/` → package `tools`). Requesting a package outside the needed set fails with "not found in required packages".
- Call `BuildSubGraph` from `OnBuild` only. It runs before the next statement and is rejected while a target callback (`SetBuildFunc`) is executing ("not allowed while target ... is running"); a recursive request for a sub-graph that is still being built is rejected too.
- Toolchain is read from the package's config entry (or global default). A later request for the same sub-graph package with an incompatible toolchain/option scope fails with "already requested with an incompatible build configuration".
- `DepOutput("pkg:target")` returns the deterministic output path; `DepBuildDir("pkg:target")` returns the directory containing that output (useful for locating generated headers or other build artifacts).
- Targets declared by the sub-graph remain in the shared target table, so the main graph can still reference them (see "Linking Sub-Graph Libraries").
- `ctx.Exec` runs with the process working directory set to the executing package's `SourceDir()`. Relative arguments such as `output/generated.h` land in the project root when the **root** package runs it, but in `tools/` when the `tools` package's own `OnBuild` runs the same call. Use `ctx.DepBuildDir(...)` or absolute paths when a file must be shared between packages.
- `ctx.Exec` outputs have no incremental declaration: every `vmake build` invocation runs the command again, even when nothing changed. Make the tool itself incremental/idempotent, or use declared generation (for example `AddBinHeader`) where applicable.

## Running / Verifying

```bash
vmake build
# Expected: [subgraph] Building tools ... done, then app links src/*.c against output/generated.h
ls output/generated.h
vmake build
# Expected: codegen runs again (ctx.Exec is not tracked); the sub-graph targets are not rebuilt
```

Fixture `test_data/07_subbuild_codegen` is this exact example (`tools/build.go` adds `official/tinyexpr` to the codegen binary).

## Linking Sub-Graph Libraries

When a sub-graph produces a **static library**, `AddDeps("sublib:sublib")` now resolves: sub-graph targets stay registered in the shared target table, so the main graph accepts the edge and links the archive through normal dependency resolution. The recommended **toolchain-decoupled** pattern is still to pass the full artifact path with `AddLdFlags`:

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

`DepOutput("sublib:sublib")` returns the path to `libsublib.a`. Adding it via `AddLdFlags` passes the full `.a` path directly to the linker without asking the main graph to resolve the sub-graph package's toolchain; with `AddDeps` make sure the package's `ctx.ToolchainOption()` selects the same toolchain that produced the artifact.

Fixtures `test_data/15_subgraph_siblings` and `test_data/16_subgraph_cross_tc` use this pattern: 15 nests `t15gen` inside `t15lib`, and 16 selects a separate toolchain for the sub-graph package via `ctx.ToolchainOption()`.

### Nested Sub-Graphs

A sub-graph can itself call `BuildSubGraph` — `DepOutput` resolves against the shared target table, so nested subgraph outputs stay visible:

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

`DepOutput` looks the package up in the target table populated by each package's `OnBuild`, and every sub-graph package's targets stay in that table while callbacks run. Nested refs like `codegen:gen` and `tools:sublib` therefore resolve regardless of depth. Each nested `BuildSubGraph` still requires its package to be part of the needed set and runs synchronously inside the enclosing call.

## See Also

- SKILL.md - Cross-Compiling
- SKILL.md - Dependencies
- SKILL.md - Build Scope
- references/api.md — BuildContext methods
- examples/with-package.md — Package dependency pattern
- Fixtures: `test_data/07_subbuild_codegen`, `test_data/15_subgraph_siblings`, `test_data/16_subgraph_cross_tc`
