# Sub-Build and Code Generation

Demonstrates the sub-build system - building a separate host tool, then executing it to produce generated source files.

## build.go

```go
package main

import (
	"os"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.SubBuild("host", "./tools")

		os.MkdirAll("output", 0755)
		ctx.Exec("tools/build/host-debug/codegen", "output/generated.h")

		ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddCFlags("-I.")
	})
}
```

## What This Demonstrates

- **`ctx.SubBuild(tcName, dir)`** - Trigger nested build in another directory
- **`ctx.Exec(binary, args...)`** - Run a built binary as build step
- Standard library integration (`os.MkdirAll`)

## Use Cases

1. **Code generation**: Build a codegen tool, run it to generate headers, then compile main sources
2. **Cross-compilation**: Build host tools with native toolchain, target binaries with cross toolchain
3. **Build utilities**: Build helper tools (protoc, flex, bison) before main compile

## Project Structure

```
myproject/
├── build.go
├── tools/
│   ├── build.go          # Nested build for host tool
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
    ├── SubBuild("host", "./tools")
    │       └── Phase 1-3: tools/build.go
    │               └── builds tools/build/host-debug/codegen
    │
    ├── Exec("tools/build/host-debug/codegen", "output/generated.h")
    │       └── Runs codegen to create generated.h
    │
    └── ctx.Target("app").AddFiles("src/*.c")
            └── Compiles using the generated header
```

## Key Points

- `SubBuild` runs a complete vmake build in subdirectory
- First argument is toolchain name ("host" = native)
- `Exec` runs after SubBuild completes
- `Exec` logs the command and its output
- Generated files must exist before main target compiles

## See Also

- references/api.md - BuildContext methods
- examples/with-package.md - Package dependency pattern