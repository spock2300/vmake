# Prebuilt Libraries

Exporting pre-compiled `.a`/`.so` files as vmake targets using `SetPrebuilt`.
The scheduler creates a symlink from the expected output path to the prebuilt
file — no copy, zero disk overhead. Downstream packages link against it
automatically through normal dependency resolution.

## Single Static Library

```go
package main

import (
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.SetProvidedLinkerScript("linker/aic8800.ld")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.GenerateConfigDefines()

		ctx.Target("drv").
			SetKind(api.TargetStatic).
			SetPrebuilt(filepath.Join(p.SourceDir(), "lib", "libdrv.a")).
			AddPublicIncludes("include")
	})
}
```

## Consuming the Prebuilt Library

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("vendor/drv")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("firmware").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDeps("vendor/drv:drv").
			UseDependencyLinkerScript()
	})
}
```

`AddDeps("vendor/drv:drv")` creates a build graph edge. The scheduler resolves
the artifact path for the `drv` target and passes `libdrv.a` to the linker
automatically. The linker script declared via `SetProvidedLinkerScript` is
inherited through `UseDependencyLinkerScript()`.

## Multiple Libraries (OpenSSL-like)

When a package provides multiple prebuilt libraries, define one target per
`.a`/`.so` file:

```go
p.OnBuild(func(ctx *api.BuildContext) {
	ctx.Target("ssl").SetKind(api.TargetStatic).
		SetPrebuilt(filepath.Join(p.SourceDir(), "lib", "libssl.a"))
	ctx.Target("crypto").SetKind(api.TargetStatic).
		SetPrebuilt(filepath.Join(p.SourceDir(), "lib", "libcrypto.a"))
})
```

Consumers can depend on specific targets:

```go
ctx.Target("app").
	AddDeps("vendor/openssl:ssl", "vendor/openssl:crypto")
```

Or let auto-wire handle it by using `AddRequires("vendor/openssl")` — all
targets from that package are added as dependencies automatically.

## With System Library Dependencies

If the prebuilt library depends on system libraries (e.g., `-lpthread`, `-lm`),
declare them with `AddProvidedLibs` on the Target:

```go
ctx.Target("drv").
	SetKind(api.TargetStatic).
	SetPrebuilt(filepath.Join(p.SourceDir(), "lib", "libdrv.a")).
	AddProvidedLibs("drv", "pthread", "m")
```

The scheduler propagates `-lpthread -lm` to all consumers of the `drv` target.
Libs matching the target name (`"drv"`) are skipped since the artifact itself
already provides the library.

## Prebuilt Shared Library

```go
ctx.Target("core").
	SetKind(api.TargetShared).
	SetPrebuilt(filepath.Join(p.SourceDir(), "lib", "libcore.so")).
	AddPublicIncludes("include")
```

Same mechanism as static libraries — symlink to the `.so` file, downstream
packages link automatically.

## Project Structure

```
vendor/drv/
├── build.go
├── include/
│   └── drv.h
├── lib/
│   └── libdrv.a
└── linker/
    └── aic8800.ld
```

## What This Demonstrates

- **`SetPrebuilt(path)`** — Declare a pre-compiled artifact; scheduler creates a symlink instead of compiling
- **`TargetStatic` + `SetPrebuilt`** — Export a prebuilt `.a` file
- **`TargetShared` + `SetPrebuilt`** — Export a prebuilt `.so` file
- **`AddPublicIncludes`** — Propagate header paths to consumers
- **`SetProvidedLinkerScript`** — Provide linker script to consumers via `UseDependencyLinkerScript()`
- **`AddProvidedLibs`** — Declare library names this target provides to consumers

## Incremental Behavior

The scheduler checks the symlink target on each build:

1. **First build**: symlink doesn't exist → create it
2. **Incremental**: symlink points to correct path → skip
3. **Path changed**: symlink points to old path → remove + recreate

Source file existence is verified — a clear error is returned if the prebuilt
file is not found.

## When to Use

| Scenario | Approach |
|----------|----------|
| Pre-compiled `.a`/`.so` from vendor | `SetPrebuilt` |
| Third-party library built from source | `TargetVoid` + `SetBuildFunc` (see `third-party-wrapper.md`) |
| Library compiled from your own source | `TargetStatic`/`TargetShared` with `AddFiles` |

## See Also

- references/api.md - `SetPrebuilt`, `Prebuilt()`, Target setters
- examples/embedded-rtos.md - Linker scripts, post-link steps
- examples/third-party-wrapper.md - Building third-party libs from source
- SKILL.md - Prebuilt Libraries section
