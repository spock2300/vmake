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
    ctx.Option("toolchain").
        SetType(api.OptionChoice).
        SetValues("host", "arm-gcc").
        SetDefault("host")
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
    │       └── builds tools/build/<tc>-<mode>/codegen
    │
    ├── DepOutput("tools:codegen")
    │       └── Returns path: tools/build/host-debug/codegen
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
- Targets built by the sub-graph are excluded from the main build graph

## See Also

- references/api.md - BuildContext methods
- examples/with-package.md - Package dependency pattern
