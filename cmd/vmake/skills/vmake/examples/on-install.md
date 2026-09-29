# OnInstall Lifecycle

Demonstrates registering extra install entries with `OnInstall`. This phase runs during `vmake build --install` (there is no standalone `vmake install` command), after all builds succeed but BEFORE any files are copied into the prefix — the hook only registers entries and filters, it cannot run shell commands. The registered entries are copied together with the target outputs.

## Prerequisites

- A project that builds successfully; installation runs only after the build phase succeeds.
- Toolchain and remote dependencies available in the current configuration.

## build.go

```go
package main

import (
	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")

		ctx.AddInstalls("config/default.conf", "etc/app/default.conf")
	})

	p.OnInstall(func(ctx *api.InstallContext) {
		ctx.AddInstalls("docs/README.md", "share/doc/app/README.md")
		ctx.AddInstalls("LICENSE", "share/licenses/app/LICENSE")
	})
}
```

## What This Demonstrates

- **`p.OnInstall(func(ctx *api.InstallContext))`** — Install-declaration hook (runs after builds, before any copy)
- **`ctx.AddInstalls(source, dest)`** — Queue a file (or directory) to copy from `source` (package SourceDir-relative) to `dest` (prefix-relative)
- **`ctx.SetInstallFilter(func(path string, isTargetOutput bool) bool)`** — Filter which entries are copied

## Key Points

- `OnInstall` runs during `vmake build --install` (or `vmake rebuild --install`), right after all targets are compiled and linked, BEFORE files are copied — its `AddInstalls` items are copied together with target outputs
- `AddInstalls` in both `OnBuild` (via `BuildContext`) and `OnInstall` (via `InstallContext`) accept the same `(source, dest)` signature; `source` resolves against the package `SourceDir()`, `dest` is relative to the prefix
- Use `OnInstall` for entries that don't belong to any specific target (docs, licenses, config templates)
- `InstallContext` exposes option reads plus install registration only: there is no `Run`/`RunIn`/`Exec`/`Make`/`SrcDir`. Do shell work in `OnBuild`/`SetBuildFunc` or in a post-link step
- The CLI `--prefix` flag is the only effective prefix override. `ctx.SetPrefix` writes `Prefix()`/`PrefixSet()` on the hook context, but the CLI installer never reads them, so it silently has no effect — do not use it
- Filter precedence: the `InstallContext` filter wins; when it is not set, the `BuildContext` filter is used. It receives `(path, isTargetOutput)`, where `path` is the built output path for target artifacts and the SourceDir-relative source for extra items
- `AddInstalls` entries registered from `OnInstall` are installed regardless of `--install-type`; the type filter only applies to target outputs:

| `--install-type` | Installed target outputs |
|---|---|
| `runtime` (default) | binaries + shared libraries; static libraries are skipped (`use --install-type sdk`) |
| `sdk` | binaries + shared + static libraries + **public includes** (`AddPublicIncludes`) |

- Test targets are never installed; on Windows the `.dll.a` import library is installed next to a shared library

## Running / Verifying

```bash
vmake build --install                    # default prefix: <project>/install
vmake build --install -p /opt/myapp      # explicit prefix
vmake build --install --install-type sdk # also static libs + public headers
```

- The prefix directory is removed and re-created before installation, then `manifest.json` (toolchain, mode, package versions/refs) is written into it.
- Expected output: `Installing...`, `INSTALL <file> -> <prefix>/...` lines, then `Install succeeded!`.
- A missing source file is only reported as `SKIP <source> (not found)` and does **not** fail the install. A stat error other than "not found" does fail.

## When to Use OnInstall

- Copying documentation, license files, or READMEs into the install tree
- Installing configuration templates that don't belong to any specific target
- Filtering install entries with `ctx.SetInstallFilter(func(path string, isTargetOutput bool) bool)`

## See Also

- SKILL.md - Install
- SKILL.md - OnInstall Lifecycle
- examples/third-party-wrapper.md — `CMakeInstall` pattern (automatic install)
- examples/firmware.md — Custom install via `api.CopyFile` in `SetBuildFunc`
