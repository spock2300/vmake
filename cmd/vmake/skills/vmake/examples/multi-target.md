# Multi-Target Project

Multiple targets in one package: static library, main binary, and test binary.

## build.go

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("mylib").
			SetKind(api.TargetStatic).
			AddFiles("src/mylib.c").
			AddIncludes("include")

		ctx.Target("myapp").
			SetKind(api.TargetBinary).
			AddFiles("src/main.c").
			AddIncludes("include").
			AddDeps("mylib")

		ctx.Target("tests").
			SetKind(api.TargetBinary).
			AddFiles("tests/*.c").
			AddIncludes("include").
			AddDeps("mylib").
			SetDefault(false)
	})
}
```

## What This Demonstrates

- **`api.TargetStatic`** - Build a static library
- **`AddIncludes("include")`** - Add include directories
- **`AddDeps("mylib")`** - Intra-package target dependency
- **`SetDefault(false)`** - Exclude target from default build

## Project Structure

```
myproject/
├── build.go
├── src/
│   ├── mylib.c
│   └── main.c
├── tests/
│   └── main.c
└── include/
    └── mylib.h
```

## Build Output

```
build/
└── <toolchain>-<mode>/
    ├── libmylib.a    # Static library
    ├── myapp         # Main executable
    └── tests         # Test executable (not built by default)
```

## Key Points

- Multiple targets in single `OnBuild`
- Dependencies resolved automatically - "mylib" builds before "myapp"
- `SetDefault(false)` useful for test targets, benchmarks
- `AddIncludes` adds -I flags for both compilation and linking

## See Also

- references/api.md - Target setters, TargetKind
- SKILL.md - Target Quick Reference