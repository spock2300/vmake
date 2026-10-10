# Prebuilt Libraries

Exporting pre-compiled `.a`/`.so` files as vmake targets using `SetPrebuilt`.
The scheduler points the expected output path at the prebuilt file — a symlink
when the target has no post-link steps, a copy when it does — and downstream
packages link against it through normal dependency resolution.

## Prerequisites

- The prebuilt file exists in the package (e.g. `drv/lib/libdrv.a`) before the first build.
- The provider package is reachable: `ctx.AddRequires("drv")` in `OnRequire` plus `AddDeps("drv:drv")` on the consumer target (or `official/openssl` style for remote packages).
- On Windows, real symlink support is required (Developer Mode or an elevated shell); VMake does not substitute junctions.

## Single Static Library

```go
package main

import (
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
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

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("drv")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("firmware").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDeps("drv:drv").
			UseDependencyLinkerScript()
	})
}
```

For a local sibling package the package name is its **directory basename** (`drv/` → `drv`); a repository path like `vendor/drv` is never scanned (`vendor` is skipped) and is not a package name. For a remote registry/native package use the `{repo}/{pkg}` reference form (e.g. `official/openssl`) in both `AddRequires` and `AddDeps`.

`AddDeps("drv:drv")` creates a build graph edge. The scheduler resolves
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

Consumers can depend on specific targets. For a local package named `openssl`:

```go
ctx.Target("app").
	AddDeps("openssl:ssl", "openssl:crypto")
```

Or use the wildcard syntax to link all targets from a package:

```go
ctx.Target("app").AddDeps("openssl:*")
```

For a remote package, the qualified package reference implies the wildcard form:
`AddDeps("official/openssl")` expands to all of that package's targets and their
transitive deps. A bare name without `/` or `:` is always a **same-package target**
reference — `AddDeps("openssl")` on a target in another package is an error, not
a package-level dependency.

## With System Library Dependencies

If the prebuilt library depends on system libraries (e.g., `-lpthread`, `-lm`),
declare them with `AddProvidedLibs` on the Target:

```go
ctx.Target("drv").
	SetKind(api.TargetStatic).
	SetPrebuilt(filepath.Join(p.SourceDir(), "lib", "libdrv.a")).
	AddProvidedLibs("drv", "pthread", "m")
```

The scheduler attaches the provided names to the link line of the target that
declares the `AddDeps` edge (the **direct dependent**). Each name other than the
target's own is resolved to `lib<name>.a` in the provider's install directory when
the provider is an installed/remote package; otherwise it is passed as `-l<name>`
(so `pthread` and `m` become `-lpthread -lm`). The forwarding does not cross an
intermediate static library: `ar` archives do not record link flags, so a final
binary that consumes only such an archive must declare the system libraries again
(`AddLdFlags("-lpthread", "-lm")` or `AddLinks("pthread", "m")`). Names matching
the target name (`"drv"` above) are skipped since the artifact itself already
provides the library.

## Prebuilt Shared Library

```go
ctx.Target("core").
	SetKind(api.TargetShared).
	SetPrebuilt(filepath.Join(p.SourceDir(), "lib", "libcore.so")).
	AddPublicIncludes("include")
```

Same mechanism as static libraries — the output is linked/copied into `BuildDir()`,
downstream packages link automatically. On Windows the artifact is a real `.dll`
and consumers link the DLL itself (the `.dll.a` import-library substitution only
happens for shared libraries VMake links itself).

## Project Structure

```
myproject/
├── build.go
├── src/
│   └── main.c
└── drv/                  # local package name = directory basename
    ├── build.go
    ├── include/
    │   └── drv.h
    ├── lib/
    │   └── libdrv.a
    └── linker/
        └── aic8800.ld
```

Local scanning skips `vendor/`, `build/`, `.vmake_deps/`, `.git/`, `node_modules/`
and hidden directories, and registers each `build.go` under its directory
basename. If the provider is a remote package instead, keep the `{repo}/{pkg}`
reference form (e.g. `official/drv`) — a repository-relative path such as
`vendor/drv` is not a package reference.

## What This Demonstrates

- **`SetPrebuilt(path)`** — Declare a pre-compiled artifact; the scheduler symlinks or copies it to the expected output path instead of compiling
- **`TargetStatic` + `SetPrebuilt`** — Export a prebuilt `.a` file
- **`TargetShared` + `SetPrebuilt`** — Export a prebuilt `.so`/`.dll` file
- **`AddPublicIncludes`** — Propagate header paths to consumers
- **`SetProvidedLinkerScript`** — Provide linker script to consumers via `UseDependencyLinkerScript()`
- **`AddProvidedLibs`** — Declare library names this target provides to its direct dependents

## Output Name and Path Resolution

The expected output is `api.TargetFilename(kind, name, target_os)` inside the
package's `BuildDir()` — `libdrv.a` for `TargetStatic`, `libdrv.so` on ELF and
`libdrv.dll` on Windows for `TargetShared`. A relative `SetPrebuilt` path resolves
from the package's `SourceDir()`, so `filepath.Join(p.SourceDir(), "lib", "libdrv.a")`
and `"lib/libdrv.a"` are equivalent. For a local `SetGit` package the downloaded
tree is `SrcDir()` (`SourceDir()/src`), so use `filepath.Join(p.SrcDir(), ...)`
when the artifact ships with the downloaded source — see `references/dirs.md`.
`AddFiles` on a prebuilt target is ignored (no compilation runs), and calling
`SetPrebuilt` twice on the same target is a fatal script error.

## Incremental Behavior

The scheduler uses a link-state success record (signature plus input/output
content hashes) for the target; the prebuilt source file is one of the recorded
inputs:

1. **First build**: no record → materialize the prebuilt output (log line `PREBUILT libdrv.a`)
2. **Unchanged**: signature, inputs and outputs all match → skip
3. **Prebuilt file changed**: its content hash changed → re-materialize the output
4. **Path changed in build.go**: the buildscript hash is part of the BuildKey, so a fresh `BuildDir` is used and the artifact is materialized there

Source file existence is verified — a clear error is returned if the prebuilt
file is not found.

## Running / Verifying

```bash
vmake build
# Expected: PREBUILT libdrv.a (symlink when the target has no post-link steps)
ls -l drv/build/*/libdrv.a   # -> drv/lib/libdrv.a
vmake build                  # second run prints no PREBUILT line (record matches)
```

With `SetSymbolPrefix` or another post-link step on the target, the output is a
regular copied file (post-link rewrites it), and `vmake build --install
--install-type sdk` copies it again into the install prefix.

## When to Use

| Scenario | Approach |
|----------|----------|
| Pre-compiled `.a`/`.so` from vendor | `SetPrebuilt` |
| Third-party library built from source | `TargetVoid` + `SetBuildFunc` (see `third-party-wrapper.md`) |
| Library compiled from your own source | `TargetStatic`/`TargetShared` with `AddFiles` |

## See Also

- SKILL.md - Prebuilt Libraries
- references/api.md — `SetPrebuilt`, `Prebuilt()`, Target setters
- references/dirs.md — `SourceDir` vs `SrcDir`, BuildKey, and the prebuilt output path
- examples/embedded-rtos.md — Linker scripts, post-link steps
- examples/third-party-wrapper.md — Building third-party libs from source
