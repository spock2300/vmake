# Migrating to vmake v3

v3 tightens correctness and API strictness, introduces a lockfile plus a
content-addressed global cache, and adds supply-chain trust gating. Most
projects need no changes; the items below describe behavior differences you
may hit when upgrading.

## Automatic cleanup on first run

The storage layout version is tracked in `.vmake/layout`. On the first v3 run,
legacy caches are removed automatically (no migration):

- `~/.vmake/sources/` (legacy global source checkouts)
- `vmake_deps/` when the layout marker is missing or outdated

Dependencies are re-resolved and rebuilt from scratch on the first v3 build.

## vmake.lock (reproducible builds)

After resolution, remote dependency versions AND commits are written to
`.vmake/vmake.lock` (commit it, alongside `.vmake/config.json`). Subsequent
builds reuse the locked versions — new upstream tags no longer change what you
build.

- `vmake lock update` re-resolves latest and rewrites the lock.
- `vmake lock show` prints locked versions.
- `vmake build --manifest x.json` imports manifest pins into the lock BEFORE
  resolution (the dependency graph is now built from the pinned versions).
- With a valid lock entry and an already-cached checkout, native version
  resolution is fully offline (**no network access**). Refs are refreshed
  (git fetch) only when resolving fresh (no lock entry) or in `vmake lock
  update` mode.

## Content-addressed global cache

```
~/.vmake/cache/<repo>/<pkg>/<version>/src   immutable per-version source
~/.vmake/cache/<repo>/<pkg>/<version>/out   shared binary cache (build/install)
~/.vmake/cache/_locks/<repo>_<pkg>.lock      build/source locks (never deleted)
```

- Different projects needing different versions of the same package no longer
  thrash a single global checkout.
- Build keys now fold in the source version, commit and the global flags hash:
  global flag changes and version switches correctly invalidate artifacts.
- `VMAKE_CACHE=<dir>` overrides the cache root (CI/test isolation).
- Lock files moved OUTSIDE the guarded directories; clones publish via
  temp-dir + atomic rename (network failures never nuke the cache).
- Local `SetGit` packages share clones under the global cache keyed by URL.
- `VMAKE_FETCH_TIMEOUT=<seconds>` overrides the git fetch timeout (default 120s).

## Supply-chain trust

Scripts from remote repositories execute with full system access. On first use
of an untrusted remote repository, vmake prompts for confirmation (TTY) or
refuses (non-TTY). Trust is recorded in `~/.vmake/config.json`.

- `vmake repo add` offers to trust the repository at add time.
- `vmake repo trust <name>` / `vmake repo untrust <name>` manage trust.
- `--yes/-y` auto-trusts (CI), `VMAKE_TRUST_ALL=1` bypasses entirely.

## New CLI

- `vmake build -j N` — parallel compile jobs per target (default: NumCPU).
- `vmake build -k` — keep building independent targets after a failure
  (dependents of failed packages are skipped).
- `vmake query targets` — list targets without building.
- `vmake query config <pkg>` — effective option values and generated defines.
- `vmake distclean --purge-cache` — distclean keeps the shared global cache by
  default (rebuild re-links without recompiling); `--purge-cache` also deletes
  the global cache entries for every remote package the project materialized.
  Affects other projects using the same packages.
- `vmake init-editor` — writes a scratch `go.mod` so gopls fully supports
  build.go (autocomplete, type-check, rename).
- `vmake pkg update <repo/name>[@version] [--dry-run]`.

## Strict config accessors

Script contexts (`OnConfig`/`OnBuild`/`OnRequire`/`OnClean`/install) now fail
loudly on misuse instead of silently returning zero values:

- Reading an **unknown option** (`ctx.Bool("dbgu_enbaled")` typo) → build error.
- Reading an option with a **mismatched accessor** (`ctx.String("x")` where `x`
  is `OptionBool`) → build error. Use the accessor matching `SetType`.
- Direct value reads (`ctx.Bool/String/Int`) inside `OnRequire` during the
  discovery phase → build error. Use `ctx.When(...)` / `ctx.If(...)`, which are
  discovery-aware.
- `ctx.When(name, 4)` now compares numerics across `int`/`float64` (JSON
  round-trips decode numbers as `float64`; this used to be silently `false`).
- `FilterDeps` re-runs `OnRequire` with the real configuration. If a dependency
  is declared there but was NOT declared during discovery (e.g. it was guarded
  by `ctx.When(...)` that evaluated false with default option values, and the
  user then enabled that option) → build error. Both phases must declare the
  same set of requires; only the guard values may differ.

The TUI/CLI read paths are unaffected.

## Option validation

After `OnConfig` runs, options are validated:

- `SetDefault` value type must match `SetType` (the classic trap: forgetting
  `SetType` — the zero type is `OptionBool` — while setting a string default).
- `OptionChoice` defaults must be one of `SetValues(...)`.

## OnApply callbacks

- Callbacks now run in **sorted option-name order** (was: random map order).
- The context passed to `OnApply` now carries real option values: reading
  other options inside the callback works.
- The `val any` argument is normalized to the option's declared type
  (`float64` → `int` for `OptionInt`).

## Test targets

`SetTest(true)` no longer clears `IsDefault`. Test inclusion is decided by the
scheduler: `vmake build` skips test targets, `vmake build --tests` and
`vmake test` include them. Ordering of `SetTest`/`SetDefault` no longer
matters.

## Renamed APIs (old names still work; `vmake doctor` flags them)

| Old | New | Why |
|-----|-----|-----|
| `target.SetExcludeLibs(...)` | `target.AddExcludeLibs(...)` | it appends despite the `Set`-name |
| `kentry.PatchKConfig(...)` | `kentry.SetKConfigPatches(...)` | `Set`-named method that replaces the map |
| `kentry.SetDefault(preset)` | `kentry.SetDefaultPreset(preset)` | avoid confusion with `Option.SetDefault` |
| `kentry.SetSelectedPreset(p)` | `kentry.SelectPreset(p)` | verb consistency |
| `ctx.Equal(...)` / `ctx.IfNot(...)` | `ctx.When(...)` / restructure | deprecated conditional helpers |

## Removed APIs

- **Fake `error` returns**: `Package.Run`, `Package.RunIn`,
  `Package.CMakeConfigure`, `Package.CMakeBuild`, `Package.CMakeInstall` and
  their `CleanContext` wrappers no longer return an always-nil `error` (they
  exit on failure via `RunFatal`). `RunEnv`, `Make`, `Configure` still return
  real errors. Scripts using `return pkg.Run(...)` must switch to a statement
  call plus `return nil`.

## Other behavior changes

- `OnPackage` may only be registered once per package (second registration is
  a build error — it used to silently discard the first).
- `AddKConfig` accepts a single entry per package (only the first was ever
  consulted).
- Non-`BuildScriptError` panics inside build.go are reported as
  `package <name>: panic: <message>` instead of a raw Go panic trace.
- `ConfigContext.Toolchains()` no longer falls back to `["host"]` on manager
  errors.
- `EnsureConfig` without a selected preset is a build error (it used to run
  bare `make`, which builds the default target).
- Static/object targets with library dependencies now print an informational
  note at build time: the libraries provide ordering/includes and are not
  merged into the archive/object output (this was previously fully silent).
- `vmake` outside a project directory (no `.vmake/` or `build.go` upward) is a
  hard error instead of silently using the current directory.
- Version scripts and linker scripts (`SetVersionScript`, `-T`) now trigger
  relink when edited.
- `build/compile_commands.json` records every source of every default target
  (fully cached builds used to produce an empty file) and is written to the
  project root rather than the invocation directory.
