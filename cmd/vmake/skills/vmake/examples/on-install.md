# OnInstall Lifecycle

Demonstrates post-install tasks using `OnInstall`. This phase runs after all builds succeed and targets are installed — use it for copying documentation, config templates, license files, or adjusting the install tree layout.

## build.go

```go
package main

import (
	"gitee.com/spock2300/vmake/pkg/api"
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

- `OnInstall` runs **after** all targets are compiled, linked, and installed — targets are already in the install tree
- `AddInstalls` in both `OnBuild` (via `BuildContext`) and `OnInstall` (via `InstallContext`) accept the same `(source, dest)` signature
- `OnBuild AddInstalls` copies files after the target is built; `OnInstall AddInstalls` copies after all targets are installed
- Use `OnInstall` for tasks that need the final install directory layout (e.g., merging config files from multiple targets)
- `SetPrefix` overrides the `--prefix` flag per-package; useful for system-wide installs (`/opt`, `/usr/local`)
- `OnInstall` is **not** called for test targets or `sdk` install type

## When to Use OnInstall

- Copying documentation, license files, or READMEs into the install tree
- Installing configuration templates that don't belong to any specific target
- Adjusting the install layout after all targets have been placed (e.g., moving files between subdirs)
- Installing with a system-wide prefix (`/opt`, `/usr/local`) while keeping build artifacts local

## See Also

- examples/third-party-wrapper.md — `CMakeInstall` pattern (automatic install)
- examples/firmware.md — Custom install via `pkg.CopyFile` in `SetBuildFunc`
