# OnInstall Lifecycle

Demonstrates post-install tasks using `OnInstall`. This phase runs after all builds succeed and targets are installed — use it for copying documentation, config templates, license files, or adjusting the install tree layout.

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
- **`ctx.AddInstalls(source, dest)`** — Copy a file from source to dest prefix
- **`ctx.SetPrefix(path)`** — Override install prefix for this package

## Key Points

- `OnInstall` runs during `--install`, right after all targets are compiled and linked — its `AddInstalls` items are copied together with target outputs
- `AddInstalls` in both `OnBuild` (via `BuildContext`) and `OnInstall` (via `InstallContext`) accept the same `(source, dest)` signature; dest is relative to the prefix
- Use `OnInstall` for entries that don't belong to any specific target (docs, licenses, config templates)
- `SetPrefix` overrides the `--prefix` flag per-package; useful for system-wide installs (`/opt`, `/usr/local`)
- Test targets are never installed; without `--install-type sdk`, static libraries are skipped at install time

## When to Use OnInstall

- Copying documentation, license files, or READMEs into the install tree
- Installing configuration templates that don't belong to any specific target
- Adjusting the install layout after all targets have been placed (e.g., moving files between subdirs)
- Installing with a system-wide prefix (`/opt`, `/usr/local`) while keeping build artifacts local

## See Also

- examples/third-party-wrapper.md — `CMakeInstall` pattern (automatic install)
- examples/firmware.md — Custom install via `pkg.CopyFile` in `SetBuildFunc`
