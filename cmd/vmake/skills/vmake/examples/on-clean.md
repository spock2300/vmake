# OnClean Lifecycle

Demonstrates custom clean logic using `OnClean`. Needed when your package has build artifacts that `vmake clean` doesn't know about (e.g., Makefile-generated files in the source tree, code-generated outputs, temporary test data).

## build.go

```go
package main

import (
	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
	})

	p.OnClean(func(ctx *api.CleanContext) {
		srcDir := ctx.SourceDir()
		ctx.RunIn(srcDir, "make", "clean")
	})
}
```

## What This Demonstrates

- **`p.OnClean(func(ctx *api.CleanContext))`** — Custom clean hook (runs during `vmake clean`)
- **`ctx.Run(name, args...)`** — Run command in BuildDir, `os.Exit` on failure
- **`ctx.RunIn(dir, name, args...)`** — Run command in specified directory
- **`ctx.SourceDir()`** — Package root directory

## Key Points

- `OnClean` is a **separate pipeline** from build — it doesn't run during `vmake build`
- `vmake clean` executes `OnClean` hooks first, then removes build artifacts (compiled objects, binaries)
- `vmake distclean` removes local build dirs, install/, and `vmake_deps/` — `OnClean` does NOT run for distclean
- Use `ctx.Run()` / `ctx.RunIn()` (not `RunEnv`) since these call `os.Exit` on failure — same as `pkg.Run()` in build scripts
- `OnClean` has access to `SourceDir()`, `BuildDir()`, and `SrcDir()` — same directory model as other phases

## When to Use OnClean

- Your `SetBuildFunc` runs `make` in `SrcDir()` and produces build artifacts there (U-Boot, Linux kernel, busybox)
- Your build generates derived files (code generation, template expansion, asset processing)
- You have a custom build system that needs `make clean` or equivalent cleanup

## See Also

- examples/simple.md — Minimal build without clean
- examples/third-party-wrapper.md — TargetVoid with SetBuildFunc pattern
- examples/firmware.md — Real-world firmware with KConfig and OnClean
