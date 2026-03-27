# Simple C Project

The most minimal VMake build script - builds a single C binary from source files.

## build.go

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("hello").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c")
	})
}
```

## What This Demonstrates

- **`package main`** with `func Main(p *api.Package)` - required entry point
- **`p.OnBuild`** - Build phase hook (Phase 3)
- **`ctx.Target("hello")`** - Create a target named "hello"
- **`SetKind(api.TargetBinary)`** - Target produces an executable
- **`AddFiles("src/*.c")`** - Source files with glob pattern

## Project Structure

```
myproject/
├── build.go
└── src/
    └── main.c
```

## Running

```bash
vmake build
./build/app/hello    # Output goes to build/app/
```

## Key Points

- No `OnConfig` needed if no build options
- No `OnRequire` needed if no third-party dependencies
- Glob patterns (`src/*.c`) match multiple files
- Output binary goes to `build/<pkg>/<target>`

## See Also

- references/api.md - Target setters and TargetKind constants
- examples/config.md - Adding build options