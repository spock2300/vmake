# OnInstall Lifecycle

Demonstrates post-install tasks using `OnInstall`. This phase runs during `vmake build --install` (there is no standalone `vmake install` command), after all builds succeed but BEFORE files are copied into the prefix — use it to register extra files (documentation, config templates, license files) that are copied alongside the target outputs.

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

### Per-Package Prefix Override

```go
p.OnInstall(func(ctx *api.InstallContext) {
	ctx.SetPrefix("/opt/mycompany")
	ctx.AddInstalls("docs/README.md", "share/doc/app/README.md")
})
```

## What This Demonstrates

- **`p.OnInstall(func(ctx *api.InstallContext))`** — Post-build install hook
- **`ctx.AddInstalls(source, dest)`** — Queue a file (or directory) to copy from `source` (package SourceDir-relative) to `dest` (prefix-relative)
- **`ctx.SetPrefix(path)`** — Override install prefix for this package

## Key Points

- `OnInstall` runs during `vmake build --install` (or `vmake rebuild --install`), right after all targets are compiled and linked, BEFORE files are copied — its `AddInstalls` items are copied together with target outputs
- `AddInstalls` in both `OnBuild` (via `BuildContext`) and `OnInstall` (via `InstallContext`) accept the same `(source, dest)` signature; dest is relative to the prefix
- Use `OnInstall` for entries that don't belong to any specific target (docs, licenses, config templates)
- `SetPrefix` overrides the `--prefix` flag per-package; useful for system-wide installs (`/opt`, `/usr/local`)
- `AddInstalls` entries from `OnInstall` are installed regardless of `--install-type`; only the type filter (e.g. static libs in runtime mode) applies to target outputs
- Test targets are never installed; without `--install-type sdk`, static libraries are skipped at install time

## When to Use OnInstall

- Copying documentation, license files, or READMEs into the install tree
- Installing configuration templates that don't belong to any specific target
- Copying extra files into the install tree with a per-package `SetPrefix` override (e.g., a system-wide prefix like `/opt` while other packages use the default)
- Filtering install entries with `ctx.SetInstallFilter(func(path string, isTargetOutput bool) bool)`
- Installing with a system-wide prefix (`/opt`, `/usr/local`) while keeping build artifacts local

## See Also

- examples/third-party-wrapper.md — `CMakeInstall` pattern (automatic install)
- examples/firmware.md — Custom install via `api.CopyFile` in `SetBuildFunc`
