# VMake v2 Migration Guide

This document describes behavioral changes in VMake v2 and how to migrate
existing projects. All changes are detected by `vmake doctor` — run it first.

## Quick Check

```bash
vmake doctor
vmake check-symbols
```

`doctor` reports `autoWire` warnings and `noRoot` warnings. `check-symbols`
now runs pure `nm` auto-detection (no per-target declaration needed).

## 1. `autoWireRequireDeps` Removed (breaking)

### Before

A package could declare dependencies via `OnRequire`/`AddRequires` without
explicitly wiring them to its targets:

```go
func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires("libfoo")
    })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c")
        // No AddDeps — vmake auto-wired libfoo:libfoo as a dep
    })
}
```

### After

The implicit auto-wire fallback has been removed (violates the No-Fallbacks
principle). Each target must declare its dependencies explicitly:

```go
func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires("libfoo")
    })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").
            SetKind(api.TargetBinary).
            AddFiles("src/*.c").
            AddDeps("libfoo:libfoo")  // explicit
    })
}
```

### Migration Steps

1. Run `vmake doctor` to find affected `build.go` files.
2. For each target that uses `ctx.DepOutput("pkg:target")` or `ctx.DepBuildDir(...)`,
   add the matching `.AddDeps("pkg:target")` call.
3. Use wildcards for multi-target packages: `.AddDeps("pkg:*")`.
4. Rebuild to verify — `compile_commands.json` and install output should be
   unchanged (verified by `test_data/_snapshot` baselines).

## 2. `SetRoot(true)` Now Recommended (soft migration)

### Before

Build roots were inferred heuristically: all local packages that aren't
depended-on library-only packages became seeds. Worked but was surprising
in mixed app/library projects.

### After

Projects are encouraged to declare their entry-point package explicitly:

```go
func Main(p *api.Package) {
    p.SetRoot(true)  // mark as the build entry point
    // ...
}
```

If no package declares `SetRoot(true)`, vmake prints a hint and falls back
to the legacy heuristic. Set `VMAKE_LEGACY_ROOT=1` to silence the hint.

In a future version the hint may become an error. Run `vmake doctor` to
detect projects that need migration.

### Migration Steps

1. Run `vmake doctor`. Address any `noRoot` warning.
2. Identify the entry-point package — the one a user would build directly
   (e.g. `app`, `firmware`, the final binary).
3. Add `p.SetRoot(true)` to its `Main` function.
4. Ensure only ONE package per project declares `SetRoot(true)`. Multiple
   roots are a hard error.
5. Rebuild.

## 3. Other Improvements (non-breaking)

These internal improvements don't require migration but are documented for
completeness:

- **Eager resolution**: remote packages are now resolved inline during
  `ResolveAll`. The `ResolveDeferred` method is a no-op kept for backward
  compatibility.
- **SubGraph simplification**: the `subGraphStack`, `subBuildKeys`, and
  `BuildKeyOverrides` mechanisms have been removed. Synchronous nested
  `pipeline.Run()` semantics are preserved.
- **Deterministic `.o` ordering**: parallel compile workers no longer
  produce non-deterministic linker input order. Shared libraries now have
  stable `build-id` across rebuilds.
- **Filename fix**: `cmd/vmake/mainfest_cmd.go` renamed to `manifest_cmd.go`.
- **Renamed internal symbols**: `collectNeeded` → `computeReachable`,
  `runPostPhase1` → `runConfigurePhase`.

## FAQ

### My build fails with "no package declares SetRoot(true)" after upgrade

This shouldn't happen — the current version only warns. If you see the
warning and want to silence it without migrating:

```bash
export VMAKE_LEGACY_ROOT=1
```

### My build fails with "dependency not found" after removing autoWire

You need to add explicit `AddDeps`. Check `vmake doctor` output, then for
each target using `ctx.DepOutput(...)` or `ctx.DepBuildDir(...)`, add the
matching `.AddDeps(...)` call.

### What about `OnRequire` only (no `AddRequires`)?

A package with `OnRequire` callback but no `AddRequires` calls inside is
not flagged by `autoWire` removal. The auto-wire only triggered for
packages that actually declared deps via `AddRequires`.

### Can I keep autoWire as a convenience?

No. The No-Fallbacks principle (AGENTS.md) requires that build intent be
explicit. If you find yourself repeatedly typing the same `AddDeps`, factor
the pattern into a helper in your build.go.

## 4. `SetExpectedExports` Removed (breaking)

### Before

Targets declared expected exported symbols via `SetExpectedExports`, and
`vmake check-symbols` compared the declared list against actual `nm` output:

```go
ctx.Target("libfoo").
    SetKind(api.TargetShared).
    SetVersionScript("foo.map").
    SetExpectedExports("foo_api", "foo_init", "foo_cleanup")  // redundant
```

### After

`SetExpectedExports` has been removed entirely. `vmake check-symbols` now
does **pure `nm` auto-detection** — no per-target declaration required.

Detection covers:

- **duplicate-export**: same symbol exported by multiple `.so`/binaries
- **mangled-leak**: C++ Itanium-mangled symbols (`_Z*`) in any artifact
- **reserved-prefix**: glibc/runtime internal leaks (`__libc_*`, `_IO_*`, `_Jv_*`, `__cxa_*`)
- **version-script-violation**: when `SetVersionScript` is set, actual exports are verified against the `.map` `global:` section
- **no-version-script** (info): `TargetShared` without a version-script

### Migration Steps

1. Remove all `SetExpectedExports(...)` calls from your `build.go` files.
2. Run `vmake check-symbols` — it now scans all built Shared/Binary targets automatically.
3. For strict per-target export control, rely on `SetVersionScript` (the linker's
   source of truth) rather than re-declaring in Go.
4. Use `vmake check-symbols --strict` in CI to fail on duplicate exports,
   mangled leaks, reserved-prefix leaks, and version-script violations.

