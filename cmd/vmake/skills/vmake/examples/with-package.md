# Third-Party Package Dependencies

Demonstrates depending on and consuming remote packages - the core package management workflow.

## build.go

```go
package main

import "github.com/spock2300/vmake/pkg/api"

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

- **`p.OnRequire`** - Require phase hook (Phase 1 and Phase 3 FilterDeps)
- **`ctx.AddRequires("repo/name >=version")`** - Declare dependency
- **`ctx.Target(...).AddDeps("repo/name")`** - Link against package in target
- **Version constraints** - semver syntax

## How It Works

1. **Phase 1 (OnRequire)**: `AddRequires` registers `"official/zlib >=1.2"` for graph discovery. All packages are resolved eagerly.
2. **Phase 2 (OnConfig)**: Build options are defined and resolved via `OnConfig` callbacks.
3. **Phase 3 (FilterDeps)**: After config is resolved, `OnRequire` runs **again** with real config values from `config.json`, recomputing the dependency list. This enables option-conditional dependencies.
4. **Phase 4 (OnBuild)**: Targets must declare explicit `AddDeps("official/zlib")` to create build-graph edges. The scheduler links the library and propagates public includes automatically.

## Version Constraint Syntax

| Syntax | Meaning |
|--------|---------|
| `>=1.2` | Version 1.2 or higher, same major (major-locked when major > 0) |
| `<=1.2` | Version 1.2 or lower |
| `>1.0` | Higher than 1.0, same major |
| `<2.0` | Lower than 2.0 |
| `~1.2.0` | Pessimistic (>=1.2.0, <1.3.0) |
| `=1.2.0` | Exact match |

## Package Repository

```bash
# List available repositories
vmake repo list

# Add custom package repo (offers to trust its build.go scripts at add time)
vmake repo add mylib https://github.com/user/mylib.git

# Trust management (remote build.go runs with full system access)
vmake repo trust mylib
vmake repo untrust mylib
```

Resolved versions and commits are pinned into `.vmake/vmake.lock` — commit it for reproducible builds; `vmake lock update` re-resolves.

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

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://github.com/madler/zlib.git")
		p.AddVersion("1.2.13", "v1.2.13")
		p.SetDescription("A massively multi-threaded portable C library")
		p.SetLicense("Zlib")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("zlib").
			SetKind(api.TargetVoid).
			AddProvidedLibs("z").
			SetBuildFunc(func(p *api.Package) error {
				p.CMakeConfigure()
				p.CMakeBuild()
				p.CMakeInstall()
				return nil
			})
	})
}
```

## Conditional Dependencies

`OnRequire` can depend on options. This works because `OnRequire` runs twice — the second pass (Phase 3 `FilterDeps`) sees the user's actual config values. During the first pass (nil config) direct reads like `ctx.Bool(...)` are **build errors** — use the discovery-aware `ctx.When(...)` (returns `true` on the first pass):

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Option("use_ssl").SetType(api.OptionBool).SetDefault(false)
})

p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("test_build/mathlib >=1.0") // always needed

    if ctx.When("use_ssl", true) {
        ctx.AddRequires("official/openssl")       // only when use_ssl=true
    }
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").
        SetKind(api.TargetBinary).
        AddFiles("src/*.c").
        AddDeps("test_build/mathlib")
    // AddDeps("official/openssl") needed here too -- no auto-wiring
})
```

**Same-set rule:** both `OnRequire` passes must declare the same set of requires — only guard values may differ. A dependency that appears only in the second pass (guard false with defaults, then the user enables the option) is a build error.

## Key Points

- Package refs use `/`: `repo/name`
- Version constraints use semver syntax (`>=`/`>` are major-locked when major > 0)
- `AddDeps("official/zlib")` auto-links; no need for manual `-lz`
- `OnRequire` runs twice: first with nil config (Phase 1), then with real config (Phase 3 `FilterDeps`)
- Option-conditional deps work because FilterDeps has resolved config values
- `AddDeps` uses the final dependency list from `FilterDeps`
- Remote packages are resolved from `.vmake/vmake.lock` pins (run `vmake lock update` to re-resolve)

## See Also

- references/api.md - RequireContext, Package metadata
- SKILL.md - Package Dependencies