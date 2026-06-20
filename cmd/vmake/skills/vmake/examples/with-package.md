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

- **`p.OnRequire`** - Require phase hook (Phase 1 and Phase 2c)
- **`ctx.AddRequires("repo/name >=version")`** - Declare dependency
- **`ctx.Target(...).AddDeps("repo/name")`** - Link against package in target
- **Version constraints** - semver syntax

## How It Works

1. **Phase 1 (OnRequire)**: `AddRequires` registers `"official/zlib >=1.2"` for graph discovery. Remote packages are deferred (not yet compiled).
2. **Phase 2a (ResolveDeferred)**: Remote packages are downloaded, compiled, and their own `OnRequire` runs to discover transitive dependencies.
3. **Phase 2c (FilterDeps)**: After config is resolved, `OnRequire` runs **again** with real config values from `config.json`, recomputing the dependency list. This enables option-conditional dependencies.
4. **Phase 3 (OnBuild)**: `autoWireRequireDeps` wires require entries to build dependencies; when target links zlib, includes/libs are linked automatically.

## Version Constraint Syntax

| Syntax | Meaning |
|--------|---------|
| `>=1.2` | Version 1.2 or higher |
| `<=1.2` | Version 1.2 or lower |
| `>1.0` | Version higher than 1.0 |
| `<2.0` | Version lower than 2.0 |
| `~1.2.0` | Pessimistic (>=1.2.0, <1.3.0) |
| `=1.2.0` | Exact match |
| `@1.2.13` | Constrain version (treated as `>=`, major-locked) |

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

`OnRequire` can depend on options. This works because `OnRequire` runs twice — the second pass (Phase 2c `FilterDeps`) sees the user's actual config values:

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Option("use_ssl").SetType(api.OptionBool).SetDefault(false)
})

p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("test_build/mathlib >=1.0") // always needed

    if ctx.Bool("use_ssl") {
        ctx.AddRequires("official/openssl")       // only when use_ssl=true
    }
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").
        SetKind(api.TargetBinary).
        AddFiles("src/*.c").
        AddDeps("test_build/mathlib")
    // autoWireRequireDeps handles openssl dep automatically when use_ssl=true
})
```

## Key Points

- Package refs use `/`: `repo/name`
- Version constraints use semver syntax
- `AddDeps("official/zlib")` auto-links; no need for manual `-lz`
- `OnRequire` runs twice: first with nil config (Phase 1/2a), then with real config (Phase 2c `FilterDeps`)
- Option-conditional deps work because Phase 2c has resolved config values
- `autoWireRequireDeps` uses the final dependency list from `FilterDeps`

## See Also

- references/api.md - RequireContext, Package metadata
- SKILL.md - Package Dependencies