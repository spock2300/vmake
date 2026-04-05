# Third-Party Package Dependencies

Demonstrates depending on and consuming remote packages - the core package management workflow.

## build.go

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("official/zlib >=1.2")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("zlib_test").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDeps("official/zlib")
	})
}
```

## What This Demonstrates

- **`p.OnRequire`** - Require phase hook (Phase 1)
- **`ctx.AddRequires("repo/name >=version")`** - Declare dependency
- **`ctx.Target(...).AddDeps("repo/name")`** - Link against package in target
- **Version constraints** - semver syntax

## How It Works

1. **OnRequire**: AddRequires registers "official/zlib >=1.2"
2. **Resolver**: Finds zlib in official repo, downloads source, resolves version
3. **OnBuild**: When target links zlib, includes/libs are linked automatically

## Version Constraint Syntax

| Syntax | Meaning |
|--------|---------|
| `>=1.2` | Version 1.2 or higher |
| `<=1.2` | Version 1.2 or lower |
| `>1.0` | Version higher than 1.0 |
| `<2.0` | Version lower than 2.0 |
| `~1.2.0` | Pessimistic (>=1.2.0, <1.3.0) |
| `=1.2.0` | Exact match |
| `@1.2.13` | Pin exact version |

## Package Repository

```bash
# List available repositories
vmake repo list

# Add custom package repo
vmake repo add mylib https://github.com/user/mylib.git
```

## Consuming a Package

After adding to `OnRequire`:
- `AddDeps("official/zlib")` on target links against it
- Auto-includes include dirs
- Auto-links libraries
- Works for static and shared builds

## Third-Party Package Definition

A package must define metadata:

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://github.com/madler/zlib.git")
		p.AddVersion("1.2.13", "v1.2.13")
		p.SetLibs("z")
		p.SetDescription("A massively multi-threaded portable C library")
		p.SetLicense("Zlib")
	})
}
```

## Key Points

- Package refs use `/`: `repo/name`
- Version constraints use semver syntax
- `AddDeps("official/zlib")` auto-links; no need for manual `-lz`
- Packages first resolved in Phase 1, available in Phase 3

## See Also

- references/api.md - RequireContext, Package metadata
- SKILL.md - Package Dependencies