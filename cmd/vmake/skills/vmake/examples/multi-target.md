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
			AddFiles("src/main.c").
			AddDeps("mylib")

		ctx.Target("tests").
			AddFiles("tests/*.c").
			AddDeps("mylib").
			SetTest(true)
	})
}
```

`ctx.Target("name")` defaults to `api.TargetBinary` and to a default (buildable) target. `SetKind` is only required for non-binary outputs: `api.TargetStatic`, `api.TargetShared`, `api.TargetObject`, or `api.TargetVoid` (external build via `SetBuildFunc`). Above, only `mylib` needs `SetKind`; `myapp` and `tests` are binaries by default.

## What This Demonstrates

- **Target defaults** — kind `api.TargetBinary`, `IsDefault() == true`
- **`api.TargetStatic`** — Build a static library
- **`AddPublicIncludes("include")`** — Include dirs for this target AND propagated to dependents
- **`AddDeps("mylib")`** — Intra-package target dependency (inherits public includes)
- **`SetTest(true)`** — Mark as test target (skipped by `vmake build` unless `--tests`, never installed; does NOT clear `IsDefault`)

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

## Install Rules

`vmake build` only builds — nothing is copied to `install/`. Installation is opt-in:

| Command | Result |
|---------|--------|
| `vmake build` | Build only |
| `vmake build --install` | Runtime install: installs binaries and shared libraries (`install/bin/myapp`). Static libraries are skipped with `SKIP libmylib.a (static lib, use --install-type sdk)` |
| `vmake build --install --install-type sdk` | Also installs static libraries (`install/lib/libmylib.a`) and public includes (`install/include/mylib.h`) |
| `vmake test` / `vmake build --tests` | Test targets are never installed |

The install prefix defaults to `./install/` (`--prefix`/`-p` overrides it). `TargetObject` outputs are never installed, and `SetInstall(false)` on a target excludes it from installation. Fixture `test_data/03_multi_target` installs only `bin/myapp` under `vmake build --install`, confirming that the static library and test target are skipped.

## Test Target Selection

`vmake build` excludes test targets unless `--tests` is passed. `vmake test` builds and runs only targets where `IsTest() && IsDefault()` and `Kind() == api.TargetBinary`. Because new targets are default, plain `SetTest(true)` keeps a test runnable; combining `SetTest(true)` with `SetDefault(false)` makes `vmake test` silently skip the target (it is not an error).

## Prerequisites

- A C compiler reachable by the host toolchain (`cc`/`gcc`/`clang`) — run `vmake doctor` to check
- Run from the project directory containing `build.go` (or a subdirectory inside it)

## Running / Verifying

```bash
vmake build                                # "Build succeeded!"; build/<buildKey>/libmylib.a + myapp; no tests binary
vmake build --tests                        # also builds build/<buildKey>/tests
vmake test                                 # builds and runs the test binary; expected "1/1 test(s) passed."
vmake build --install                      # install/bin/myapp only
vmake build --install --install-type sdk   # adds install/lib/libmylib.a + install/include/mylib.h
```

## Reusing Sources Across Targets

Objects are isolated per target: the same source listed in two targets is compiled once per target (the object path hashes the target name and the source path). To compile shared code once, build an `api.TargetObject` target and let other targets depend on it:

```go
ctx.Target("common").
    SetKind(api.TargetObject).
    AddFiles("src/common.c")

ctx.Target("myapp").
    AddFiles("src/main.c").
    AddDeps("common")
```

The object artifact is linked directly into dependents. See `examples/complete.md` for `TargetShared` / `TargetObject` patterns.

## Key Points

- Multiple targets in single `OnBuild`
- New targets default to `api.TargetBinary` and `IsDefault() == true`; use `SetKind` for static, shared, object, and void targets
- Dependencies resolved automatically - "mylib" builds before "myapp"
- `SetTest(true)` targets are skipped by plain `vmake build` and never installed; `SetTest(true)` plus `SetDefault(false)` silently excludes the target from `vmake test` (which requires `IsTest() && IsDefault()`)
- `AddPublicIncludes` implies `AddIncludes` — no need to call both for the same directory
- `AddDeps("mylib")` propagates public includes — `myapp` and `tests` get `-Iinclude` automatically

## Cross-Package Dependencies

Use the `pkg:target` format to depend on targets from other packages:

```go
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("myapp").
        AddFiles("src/main.c").
        AddDeps("lib:utils")
})
```

`lib:utils` means the `utils` target from the `lib` package. vmake ensures `lib:utils` builds first, links its output, and propagates its public includes.

For a package in the same local workspace, `AddDeps` alone is enough — every local `build.go` is scanned and loaded (see `examples/multi-module.md`). A **remote** package additionally needs `AddRequires` in `OnRequire`; `AddDeps` alone neither resolves nor downloads it:

```go
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("official/zlib >=1.2")
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("myapp").AddFiles("src/main.c").AddDeps("official/zlib")
})
```

The explicit wiring rule is documented in `SKILL.md - Dependencies`.

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
- SKILL.md - Target API at a Glance
- SKILL.md - Test Targets
- examples/multi-module.md - Cross-package workspace wiring
