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

## Prerequisites

- Git on PATH for the registry clone and package source checkout
- Network access for the first clone (later builds reuse the cache and `.vmake/vmake.lock`)
- The package repository registered and trusted. Repositories are **not** built in — a fresh install has none (`vmake repo list` prints `No repositories found`). The repo name in `AddRequires("official/zlib")` must match the name you registered:

```bash
# Could be any name; "official" is the convention used by this example.
vmake repo add official <your-registry-url>    # `official` is the local name you choose

# Confirm trust if you skipped the interactive prompt; records the repo in ~/.vmake/config.json.
vmake repo trust official
```

`vmake repo add` clones the registry and, on a TTY, offers to trust its `build.go` scripts (they run with full system access). On a non-TTY stdin (CI, piped input) the prompt is skipped and trust is not recorded; the first build then fails with `its build.go scripts are not trusted; pass --yes or run 'vmake repo trust official'`. Use `--yes`/`-y` or `VMAKE_TRUST_ALL=1` in CI. `vmake repo update` fetches new content and resets trust, so re-trust after updating.

## How It Works

1. **Phase 1 (OnRequire)**: `AddRequires` registers `"official/zlib >=1.2"` for graph discovery. Top-level packages (registry and native) are resolved eagerly; native sub-packages load only when depended on.
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

Constraints are parsed by `ParseConstraint` in `pkg/api/semver.go`: **one operator plus one version**, where missing minor/patch components default to 0 (`1.2` parses as `1.2.0`). Compound ranges such as `>=1.2 <2.0` are not supported — split them into separate `AddRequires` calls for the same package if needed. A version given without an operator (`"official/zlib 1.2"`) is treated as `>=1.2`; a requirement with no version at all (`"official/zlib"`) is treated as `>=0.0.0`, so any version matches and no major lock applies.

The **highest satisfying version** is selected. When several packages constrain the same dependency, all constraints must match (`SelectVersionMulti`); mutually unsatisfiable constraints fail resolution. Prereleases are excluded unless a constraint pins that same `major.minor.patch` prerelease — a `>=1.2.0-rc1` constraint admits `1.2.0-rc2`, but not `1.2.1-rc1`.

Version pins in entries of the active configuration (`.vmake/config.json` by default, set via the TUI) take precedence over latest matching tags, and `.vmake/vmake.lock` pins survive until `vmake lock update`.

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

## Sub-Packages

A nested `build.go` inside a **native** remote package's checkout becomes a sub-package named `parent/sub`, with its own options, targets, and build directories. Reference it from outside by full name, listing the parent before its sub-packages in the same `AddRequires`:

```go
ctx.AddRequires("subtest/mother >=1.0.0", "subtest/mother/sub_a")
```

Sub-packages are lazy: their `build.go` is interpreted only when depended on (top-level packages are resolved eagerly). Registry (wrapper) packages have no sub-packages — by design. A working example lives in `test_data/25_subpackage`. See `SKILL.md - Sub-Packages`.

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

**Same-set rule:** pass 2 must not add a package that pass 1 did not declare — a dependency that first appears in the FilterDeps pass (e.g. the guard was false with defaults, then the user enabled the option) is a build error; hoist the unconditional `AddRequires` out of the guard. The reverse direction is fine: pass 2's returned list replaces `node.Deps`, so a dependency dropped in pass 2 is pruned from the graph (not an error). See `SKILL.md - OnRequire Two-Phase Execution`.

## AddDeps After FilterDeps

`AddDeps` uses the **final post-FilterDeps dependency list**. Target edges are expanded at build-graph time over packages that survived `FilterDeps` and the BFS from local roots. If `FilterDeps` pruned a package (or it became unreachable from the roots) while a target still references it, the build fails at build-graph time:

- `AddDeps("official/x")` → `package not found in build graph: official/x`
- `AddDeps("official/x:target")` → `dependency not found: ...`

Keep `OnRequire` and target dependencies consistent.

## Where Files Land

- Registry clone: `~/.vmake/repos/<repo>/`; trust: `~/.vmake/config.json` (`trustedRepos`)
- Global cache: `~/.vmake/cache/v2/<repo>/<pkg>/<version>/{src,out}` (`VMAKE_CACHE` overrides the root)
- Project links: `vmake_deps/<repo>/<pkg>/{src,out}` point into the cache
- Pins: `.vmake/vmake.lock`

## Key Points

- Package refs use `/`: `repo/name`
- Version constraints use semver syntax (`>=`/`>` are major-locked when major > 0); one operator plus one version, no compound ranges
- `AddDeps("official/zlib")` auto-links; no need for manual `-lz`
- `OnRequire` runs twice: first with nil config (Phase 1), then with real config (Phase 3 `FilterDeps`)
- Option-conditional deps work because FilterDeps has resolved config values
- `AddDeps` uses the final dependency list from `FilterDeps`; a pruned package referenced by `AddDeps` fails at build-graph time
- Remote packages are resolved from `.vmake/vmake.lock` pins (run `vmake lock update` to re-resolve)
- Native sub-packages load lazily and use full `parent/sub` names; registry wrappers have none

## Running / Verifying

```bash
vmake repo list    # "Repositories:" with "official (registry)" after the bootstrap above
vmake build        # resolves + downloads + builds; ends with "Build succeeded!" and writes .vmake/vmake.lock
vmake query        # dependency tree including official/zlib (with version for native packages)
vmake lock show    # e.g. "official/zlib 1.2.13 (registry, <commit>)"; before the first build: "vmake.lock is empty ..."
```

## See Also

- references/api.md - RequireContext, Package metadata
- SKILL.md - Dependencies
- SKILL.md - Version Constraints
- SKILL.md - OnRequire Two-Phase Execution
- SKILL.md - Sub-Packages
- examples/third-party-wrapper.md - Authoring a package that others depend on
