# Multi-Target Project

Multiple targets in one package: static library, main binary, and test binary.

## build.go

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("mylib").
			SetKind(api.TargetStatic).
			AddFiles("src/mylib.c").
			AddPublicIncludes("include")

		ctx.Target("myapp").
			SetKind(api.TargetBinary).
			AddFiles("src/main.c").
			AddDeps("mylib")

		ctx.Target("tests").
			SetKind(api.TargetBinary).
			AddFiles("tests/*.c").
			AddDeps("mylib").
			SetTest(true)
	})
}
```

## What This Demonstrates

- **`api.TargetStatic`** - Build a static library
- **`AddPublicIncludes("include")`** - Include dirs for this target AND propagated to dependents
- **`AddDeps("mylib")`** - Intra-package target dependency (inherits public includes)
- **`SetTest(true)`** - Mark as test target (excluded from default build, never installed)

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
└── <buildKey>/
    ├── libmylib.a    # Static library
    ├── myapp         # Main executable
    └── tests         # Test executable (not built by default)
```

## Running Tests

```bash
vmake build --tests  # Build everything including test targets
vmake test           # Build + run test targets, report pass/fail
```

## Key Points

- Multiple targets in single `OnBuild`
- Dependencies resolved automatically - "mylib" builds before "myapp"
- `SetTest(true)` useful for test targets — auto-sets `isDefault=false`, skipped by `vmake build` and install
- `AddPublicIncludes` implies `AddIncludes` — no need to call both for the same directory
- `AddDeps("mylib")` propagates public includes — `myapp` and `tests` get `-Iinclude` automatically

## Cross-Package Dependencies

Use the `pkg:target` format to depend on targets from other packages in a multi-module workspace:

```go
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("myapp").
        SetKind(api.TargetBinary).
        AddFiles("src/main.c").
        AddDeps("lib:utils")
})
```

`lib:utils` means the `utils` target from the `lib` package. vmake ensures `lib:utils` builds first, links its output, and propagates its public includes.

## AddPublicIncludes

`AddPublicIncludes` on a target makes its include directories available to the target itself and all dependents. It implies `AddIncludes` — no need to call both:

```go
ctx.Target("mylib").
    SetKind(api.TargetStatic).
    AddFiles("src/mylib.c").
    AddPublicIncludes("include")
```

Consumers that `AddDeps("mylib")` automatically get `-Iinclude` without specifying it themselves.

## See Also

- references/api.md - Target setters, TargetKind
- SKILL.md - Target Quick Reference