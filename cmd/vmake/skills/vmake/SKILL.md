---
name: vmake
description: >
  VMake C/C++ build system assistant for writing build.go files, configuring
  build options, integrating CMake projects through VMake's CMake API,
  managing third-party packages, using vmake CLI commands,
  embedded/RTOS firmware builds, cross-compilation, linker scripts, and code
  generation. Also use when the user is working on a C/C++ project that uses
  vmake, modifying existing build.go files, debugging vmake build errors,
  setting up package repositories, or asking about vmake concepts like targets,
  dependencies, options, or lifecycle phases.
---

# VMake Build Assistant

VMake is a Go-based C/C++ build system. Build scripts are Go files
(`build.go`) interpreted at runtime by [yaegi](https://github.com/traefik/yaegi) (Go interpreter)
and executed through a multi-phase lifecycle.
It's an alternative to CMake/Meson/Bazel, using Go as the configuration language.

## Mental Model: Build Phases

Every build.go follows the same lifecycle. You don't need all phases — only
include the ones your project needs:

| Phase | Hook / Step | When you need it |
|-------|-------------|-----------------|
| 1 | `OnRequire` | Declare deps (runs with nil config; all packages resolved eagerly) |
| 2 | `OnConfig` | Build options (debug/release, features, etc.) — includes OnApply callbacks |
| 3 | `FilterDeps` | Re-runs `OnRequire` with real config values; recomputes deps; BFS collects needed packages |
| 4 | `OnBuild` | Define targets |
| 5 | Compile & Link | Scheduler compiles sources and links targets |
| 6* | Install | Optional install (only with `--install` flag) |
| — | `OnInstall` | Extra install entries (runs when install starts, after builds succeed; items copied together with targets) |
| clean | `OnClean` | Custom clean logic (runs during `vmake clean`/`distclean`; separate from build pipeline) |

`OnPackage` runs for all packages right after `Main()` is called (before any lifecycle phases). Use it to describe the package (`SetDescription`, `SetLicense`, `SetHomepage`). `SetGit`/`AddVersion` inside `OnPackage` downloads remote source — for local packages it is linked at `SourceDir()/src/` (and `SrcDir()` points there) — works for both registry packages and local packages that need to wrap a downloaded library.

## Decision Guide

- **New project, no options, no deps** → Only `OnBuild`. Start from `examples/simple.md`.
- **Need configurable features** → Add `OnConfig`. See `examples/config.md`.
- **Conditional compilation** → Options + `ctx.If()`/`ctx.Select()`. See `examples/conditional.md`.
- **Config options → C compiler defines (-D flags)** → Three mechanisms. See `examples/config-to-define.md`.
- **Multiple targets (lib + binary + tests)** → See `examples/multi-target.md`.
- **Multi-module workspace (lib/ + app/ directories)** → See `examples/multi-module.md`.
- **Third-party packages** → `OnRequire` + `AddRequires` + `AddDeps`. See `examples/with-package.md`.
- **Build a CMake project** → Prefer `CMakeConfigure`, `CMakeBuild`, and `CMakeInstall` inside `TargetVoid` + `SetBuildFunc`. See `examples/third-party-wrapper.md`.
- **Wrap an Autotools library** → `TargetVoid` + `SetBuildFunc`. See `examples/third-party-wrapper.md`.
- **Pre-compiled libraries (.a/.so)** → `SetPrebuilt`. See `examples/prebuilt.md`.
- **Cross-package config propagation (GenerateConfigDefines, ExportConfig, ImportConfig)** → See `examples/config-propagate.md`.
- **Code generation / host tools** → `BuildSubGraph` + `DepOutput` + `Exec`. See `examples/subbuild.md`.
- **Embedded / RTOS firmware (linker script, hex/bin)** → See `examples/embedded-rtos.md`.
- **Embedded firmware (KConfig/partitions)** → `EnsureConfig` + `SetKConfigPatches` + `DepBuildDir`. See `examples/firmware.md`.
- **Symbol conflicts / leaked internals across dependencies** → `SetDefaultVisibilityHidden` + `SetVersionScript` + `vmake check-symbols`. See `examples/symbol-management.md`.

For CMake projects, let VMake's CMake API manage toolchain resolution, platform paths,
build directories, installation prefixes, build configuration, and parallelism.
Keep build.go focused on project options and project-specific steps. Invoke `cmake`
directly only for special operations the API cannot express; do not duplicate these
general rules in build.go. See `references/api.md` for settings and migration details.

## Build Script Template

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
    })

    p.OnClean(func(ctx *api.CleanContext) {
    })
}
```

## Platform Notes (Windows)

VMake runs natively on Windows. Preconditions: **Git for Windows** (full installer — it supplies
`sh`, coreutils, `sed`/`awk`/`grep`/`find`, `tar` and `curl`, which vmake discovers automatically),
a **target toolchain** (GNU ARM for bare-metal or MinGW-w64 for native Windows; Git for Windows ships no C compiler and no `make`), and
**Developer Mode** (the storage layout is symlink-based). `vmake doctor` reports all of these.

When writing build.go that must also work on Windows:

- `p.Make()` and `p.EnsureConfig()` run the *toolchain's* make program (`Toolchain.Tools.MAKE`),
  which defaults to `make`. Do not hardcode `make` — call `p.Make(...)` / `p.EnsureConfig(dir)`.
  Default make is resolved only when used; custom menuconfig with an existing config does not require it. An explicitly configured MAKE is still validated.
- `p.Configure(...)` runs `./configure` through `sh` automatically on Windows; never exec the
  script path directly.
- `p.CMakeConfigure(...)`, `p.CMakeBuild(...)`, and `p.CMakeInstall(...)` pass resolved tools
  and normalized paths. Windows defaults to Ninja unless a generator or configure preset is explicit;
  install CMake and the selected generator. Use these helpers for CMake projects.
- Artifact names are decided by the toolchain's **target OS**, not the host:
  `TargetBinary` → `.exe`, `TargetShared` → `.dll` (+ import library `lib<name>.dll.a`, which is
  what consumers link against on PE targets). Use `api.TargetFilename(kind, name, targetOS)` if you
  need to compute one.
- `SetVersionScript`, `AddExcludeLibs` and `SetSymbolBinding` are ELF-only; the build fails with a
  clear error on a Windows target instead of silently dropping them. There is no PE equivalent.
- `vmake check-symbols` inspects ELF dynamic symbols with the selected toolchain's `nm -D` on either host; PE files and ELF files without dynamic symbols report that the audit is not applicable.
- Build filesystem paths with `filepath.Join`. Write `/` only in logical identifiers
  (`repo/pkg`, `pkg:target`) and glob patterns (`src/**/*.c`) — the glob layer normalizes those, and
  object names are flattened so `src/foo.c` and `src\foo.c` map to the same object.
- Prefer `runtime.NumCPU()` over `$(nproc)`: commands are exec'd directly, with no shell expansion.

## Common Mistakes

### `pkg.Make()` runs in BuildDir, not SourceDir

`pkg.Make()` always runs `make` in `BuildDir`. For most third-party packages (U-Boot, Linux, Busybox, etc.), the Makefile is in the source tree, so you need `pkg.RunIn()`:

```go
SetBuildFunc(func(p *api.Package) error {
    srcDir := p.SrcDir()
    p.EnsureConfig(srcDir)
    p.RunIn(srcDir, "make", "-j"+strconv.Itoa(runtime.NumCPU()))
    return nil
})
```

Use `pkg.Make()` when the Makefile is in the scratch `BuildDir`. CMake projects use
`pkg.CMakeBuild()` and `pkg.CMakeInstall()` with their own build directory and generator.

### `$(nproc)` won't work — use `runtime.NumCPU()`

`exec.Command` doesn't expand shell features. `$(nproc)`, `$(pwd)`, and pipes won't work. Use Go APIs instead:

```go
"-j" + strconv.Itoa(runtime.NumCPU())
```

### `ctx.If()` returns `[]string` — pass it directly, do NOT spread with `...`

`Add*` methods take variadic `...any` and flatten `[]string` items. Under yaegi, spreading a `[]string` into `...any` fails (`reflect.CallSlice` cannot convert `[]string` to `[]interface{}`):

```go
AddCFlags(ctx.If("debug", "-g", "-O0"))    // correct — []string flattens automatically
AddCFlags(ctx.If("debug", "-g", "-O0")...) // runtime error in yaegi
```

Also: `flattenAny` silently drops empty strings — never rely on an empty flag reaching the compiler.

### `filepath.Join` with absolute paths

`filepath.Join("/a/b", "/a/b/c")` returns `/a/b/a/b/c`, NOT `/a/b/c`. The second absolute path wins and the first becomes a segment. Use string concatenation or trim leading `/` for logical path components.

### `SetGit`/`AddVersion` — works for local packages too

`SetGit`/`AddVersion` in `OnPackage` is the primary mechanism for registry packages, but it also works for local packages that need to download and compile a remote library (e.g., FreeRTOS, mbedtls). When a local package uses `SetGit`, source is downloaded to `SourceDir()/src/` and `SrcDir()` returns that path (registry packages differ: their `SourceDir()` already points at the downloaded checkout, so `SrcDir()` falls back to `SourceDir()`). This is the easiest way to wrap a third-party C library that doesn't need a full registry setup.

### Path resolution for packages using `SetGit`

When a **local** package uses `SetGit`, `SourceDir()` and `SrcDir()` differ — all `AddFiles` / `AddIncludes` / `AddPublicIncludes` paths resolve from `SourceDir()`, so you must prefix with `"src/"`. Registry/native packages resolve paths directly from the downloaded checkout: their `SourceDir()` already points at it, and a `"src/"` prefix would double-nest. See `references/dirs.md` for full rules, edge cases, and the correct code pattern.

### Static library deps with symbols not referenced by your code

vmake wraps `AddDeps` archives in `--start-group`/`--end-group`. If a static lib dep provides symbols only referenced by post-group libraries (e.g., libc from `-specs`), the linker won't pull the archive. Fix with `-nostdlib` + `AddGlobalLinks`. See `references/gotchas.md` for all three fix patterns with code.

### `pkg.Run` / `pkg.RunIn` / `CMake*` call `os.Exit` on failure — no error return

`p.Run()`, `p.RunIn()`, `p.CMakeConfigure()`, `p.CMakeBuild()`, `p.CMakeInstall()` (and their `CleanContext` wrappers) return **nothing** — they exit the process on failure. Only `p.RunEnv()`, `p.Make()`, and `p.Configure()` return a real `error` you should check. Never write `return pkg.Run(...)` — call it as a statement, then `return nil`.

### `vmake clean` vs `vmake distclean`

`vmake clean` runs `OnClean` hooks then removes build artifacts (objects, binaries); keeps `vmake_deps/`. `--all` removes every build-key directory.

`vmake distclean` also runs `OnClean` hooks, then removes all local build dirs, `install/`, `compile_commands.json`, and `vmake_deps/`. The shared global cache (`~/.vmake/cache`) survives by default — a rebuild re-links without recompiling. `--purge-cache` additionally deletes global cache entries for every remote package this project materialized (affects other projects too). Use distclean when modifying `build.go` and the build ignores your changes.

### Patching source before build in registry packages

To patch downloaded source inside `SetBuildFunc`, use Go's `os.ReadFile` + `os.WriteFile` (simple single-line changes) or `AddPatches("patches/fix.patch")` in `OnPackage` (multi-file git patches). Remote patches are applied to a content-addressed patched copy, never to the immutable cache checkout; local packages patch in place. See `references/gotchas.md` for code examples of both patterns.

### Strict config accessors — wrong reads are build errors, not zero values

Script-facing contexts (`OnConfig`/`OnBuild`/`OnInstall`/`OnClean`/`OnRequire`) fail loudly on: reading an **unknown option** (typo), using an **accessor that mismatches `SetType`** (e.g. `ctx.String` on an `OptionBool`), and **direct value reads (`ctx.Bool/String/Int`) during OnRequire discovery**. In `OnRequire`, use the discovery-aware helpers: `ctx.When("opt", true)`, `ctx.If(...)`, `ctx.Select(...)`.

Two related rules:
- **Both OnRequire passes must declare the same set of requires** — only guard values may differ. A dependency that appears only in the second pass (FilterDeps, with real config) is a build error.
- `When` compares numerics across `int`/`float64` (JSON round-trips decode numbers as `float64`); `Select` returns `""` and `When` returns `true` while config is still nil during discovery.

### Script-relative file IO

Inside build.go, relative paths passed to wrapped stdlib (`os.ReadFile/WriteFile/Stat/...`, `exec.Command` without `Dir`) resolve against the **build.go's own directory** in ALL phases — not the process cwd. `os.Getwd()` returns the script dir; `os.Chdir` returns an error. Long-tail unwrapped APIs (`text/template.ParseFiles`, `exec.CommandContext`, `io/ioutil`) still see the process cwd — build absolute paths from `p.SourceDir()`/`p.BuildDir()` for those.

## Directory Reference

| Property | What it returns | When to use |
|----------|-----------------|-------------|
| `SourceDir()` | Package root (where build.go lives) | Package metadata files, overlay dirs |
| `SrcDir()` | Source code dir (`SourceDir()/src/` for local `SetGit` packages, falls back to `SourceDir()`) | Source files for firmware/third-party builds |
| `BuildDir()` | Scratch dir for intermediate artifacts | Build outputs, stamps |
| `InstallDir()` | Remote package installation prefix; empty for local packages | Remote package publication |
| `CMakeBuildDir()` | `BuildDir()/cmake`, unless explicitly set | CMake cache and build artifacts |
| `CMakeInstallDir()` | Remote `InstallDir()` or local `BuildDir()/staging`, unless explicitly set | Headers/libs installed by CMake |

For `BuildKey` naming, `SourceDir` vs `SrcDir` distinction, and `SetGit` path resolution rules, see `references/dirs.md`.

## Storage Layout

Sources and build outputs for remote packages live in a **content-addressed global cache** (`~/.vmake/cache/`), shared across projects. Each project's `vmake_deps/` is a symlink farm into it — auto-added to `.gitignore` on first build. Buildscripts are interpreted by yaegi at runtime — no `.so` files are generated.

```
project/
├── build.go
├── .vmake/
│   ├── config.json               # Option values + selected presets (commit it)
│   └── vmake.lock                # Pinned remote versions+commits (commit it)
└── vmake_deps/                   # Auto-managed, gitignored symlink farm
    └── <repo>/<pkg>/
        ├── src → ~/.vmake/cache/<repo>/<pkg>/<version>/src        # immutable checkout
        └── out → ~/.vmake/cache/<repo>/<pkg>/<version>/out        # shared binary cache
                     └── <buildKey>/{build,install}/
```

- Each `<version>` has its own immutable checkout (cloned via temp-dir + atomic rename, never mutated in place). Projects needing different versions never thrash each other.
- `out/<buildKey>/` is a **shared binary cache**: identical toolchain+mode+options+version+commit+global-flags reuse compiled artifacts across projects (rebuild after `distclean` re-links without recompiling).
- Patches on remote packages never touch the immutable checkout — a patched copy is materialized at `<version>/patched/<patchHash>/src` and shared by identical patch sets.
- `.vmake/vmake.lock` pins remote versions + commits for reproducible builds. With a valid lock entry and cached checkout, resolution is fully offline. `vmake lock update` re-resolves; `vmake lock show` prints pins.
- Per-package `flock` files in `~/.vmake/cache/_locks/` serialize concurrent access across projects.

Global storage in `~/.vmake/`:
- `~/.vmake/cache/<repo>/<pkg>/<version>/{src,out}` — content-addressed source checkouts + shared build outputs
- `~/.vmake/cache/_localgit/<sha256(url)>/src` — shared clones for local `SetGit` packages (keyed by URL)
- `~/.vmake/repos/` — registry repo clones (buildscript metadata only)
- `~/.vmake/toolchains/` — toolchain manifests
- `~/.vmake/extensions/` — extension repos
- `~/.vmake/config.json` — `trustedRepos` (remote-script trust)

Environment overrides: `VMAKE_CACHE` (cache root), `VMAKE_FETCH_TIMEOUT` (git fetch seconds, default 120), `VMAKE_TRUST_ALL=1` (bypass trust gating, CI).

vmake locates the project root by walking upward from cwd to find `.vmake/`, `build.go`, or (at the starting directory only) `*/build.go` (via `findProjectDir()`). Running outside a project is a hard error.

## Remote Script Trust

`build.go` files from remote repositories execute with full system access. On first use of an untrusted repo, vmake prompts (TTY) or refuses (non-TTY). Manage with `vmake repo trust/untrust <name>`, auto-approve with `--yes/-y`, or bypass with `VMAKE_TRUST_ALL=1` (CI). `vmake repo update` removes trust so content drift requires explicit re-trust.

## Package Types

| Type | How identified | `OnPackage` metadata | Source code location |
|------|---------------|---------------------|---------------------|
| **Local** | build.go in project directory | `SetDescription`, `SetLicense`, or `SetGit`/`AddVersion` for remote source | `SourceDir()` (same as build.go), or `SrcDir()` = `SourceDir()/src/` if `SetGit` used |
| **Registry** | `vmake repo add name url` | `SetGit`, `AddVersion` required | `SourceDir()` is the downloaded checkout (`vmake_deps/<repo>/<pkg>/src`); `SrcDir()` falls back to `SourceDir()` |
| **Native** | `vmake repo add --native name url` | No `SetGit`/`AddVersion` — version from git tag | `SourceDir()` == `SrcDir()` (the downloaded checkout; `build.go` sits at its root) |

Registry packages wrap external C/C++ libraries. Native packages are independent vmake projects consumed as dependencies. The resolver checks registry first, then native.

## Target API at a Glance

```go
ctx.Target("app").
    SetKind(api.TargetBinary).
    AddFiles("src/*.c").
    AddPublicIncludes("include").
    AddDefines("DEBUG=1").
    AddCFlags("-Wall").
    AddCxxFlags("-stdlib=libc++").
    AddLdFlags("-lm").
    AddLinks("ssl", "crypto").
    AddDeps("lib:utils").
    SetDefault(false).
    SetBuildFunc(func(p *api.Package) error { ... }).
    SetPrebuilt("/path/to/libfoo.a")
```

`AddPublicIncludes` implies `AddIncludes` — directories set via `AddPublicIncludes` are automatically available to the target itself and propagated to all dependents. There is no need to duplicate them with `AddIncludes`. Use `@"pattern"` as the last argument to filter propagated files: `AddPublicIncludes(".", "@*.h")` only propagates headers matching `*.h`.

`AddFiles` accepts glob patterns and can be called with multiple globs to collect sources from different directories:

```go
AddFiles("src/common/*.c", "src/network/*.c", "src/stun/*.c")
```

Remove flags: `RemoveCFlags`, `RemoveDefines`, `RemoveIncludes`, etc. These perform **immediate exact-match deletion** from internal slices — calling `RemoveCFlags("-Wall")` after `AddCFlags("-Wall")` removes the flag instantly.

`RemoveFiles` works differently: it stores patterns and applies them at build time against **glob-expanded paths**, not against the raw `AddFiles` argument strings. This deferred matching means:
- `AddFiles("src/*.c").RemoveFiles("src/test_*.c")` — works (globs expand, patterns match expanded paths)
- `AddFiles("src/main.c", "src/test.c").RemoveFiles("src/test.c")` — works even though no glob was used (patterns match the final file paths, not the AddFiles strings)
- `RemoveFiles` does NOT remove entries from the `AddFiles` rule list — it adds exclusion patterns to a separate filter applied during compilation

This is the only Remover method that uses deferred matching; all others (`RemoveCFlags`, `RemoveDeps`, `RemoveLinks`, `RemoveProvidedLibs`, etc.) delete immediately.

Third-party packages with external build systems use `TargetVoid` with `SetBuildFunc`. The callback function `func(p *api.Package) error` returns a real error — unlike `pkg.Run()` (which calls `os.Exit`), `SetBuildFunc` errors are returned to the scheduler and fail the build gracefully. Use `return fmt.Errorf(...)` for controlled failure, `return nil` for success.

## Prebuilt Libraries

Use `SetPrebuilt(path)` on `TargetStatic`, `TargetShared`, or `TargetBinary` to export a pre-compiled artifact. The scheduler creates a symlink from the expected output path (no copy, zero disk overhead). Incremental: compares symlink target, recreates only if path changed. Multiple libraries: one target per `.a`/`.so`.

```go
ctx.Target("drv").SetKind(api.TargetStatic).
    SetPrebuilt(filepath.Join(p.SourceDir(), "lib", "libdrv.a")).
    AddPublicIncludes("include").AddProvidedLibs("drv")
```

See `examples/prebuilt.md` for full patterns, `AddProvidedLibs`, shared libraries, and when to use `SetPrebuilt` vs `TargetVoid`+`SetBuildFunc`.

## Symbol Management

Control which symbols a library exports to prevent conflicts and leaks in
complex dependency graphs. Five layers, applied in order:

| Layer | API | Purpose |
|-------|-----|---------|
| 1. Default hidden | `ctx.SetDefaultVisibilityHidden()` | `-fvisibility=hidden` in this package; annotate exports in source |
| 2. Version script | `target.SetVersionScript("foo.map")` | Declarative exports on `TargetShared`/`TargetBinary` |
| 3. Link policy | `target.AddExcludeLibs(...)`, `target.SetSymbolBinding("static")` | Strip static archive symbols; bind internal refs |
| 4. Audit | `vmake check-symbols` | Pure `nm -D` auto-detection: duplicates, mangled leaks, glibc leaks, version-script violations |
| 5. Prefix | `target.SetSymbolPrefix("v_")` | `objcopy --prefix-symbols=` for third-party C code |

Enable Layer 1 separately in each package whose exports you control. Dependencies
retain their own visibility policy; explicit `AddGlobalCFlags`/`AddGlobalCxxFlags`
still affect all packages. Native and CMake compilation use the same package
defaults, which are included in that package's build key.
`SetVersionScript` on `TargetObject` is a build error (partial link produces
no dynamic symbol table). See `examples/symbol-management.md` for full
patterns and version-script syntax.

## Test Targets

Mark targets with `SetTest(true)`. Test targets are excluded from `vmake build` by default; `vmake build --tests` includes them; `vmake test` builds and runs `TargetBinary` tests, reporting pass/fail with timing.

```go
ctx.Target("tests").SetKind(api.TargetBinary).SetTest(true).
    AddFiles("tests/*.c").AddDeps("mylib")
```

Always define test targets unconditionally in `OnBuild` — `SetTest(true)` controls scheduler visibility, not option guards. `SetTest(true)` does NOT clear `IsDefault` (ordering of `SetTest`/`SetDefault` is irrelevant — inclusion is decided by the scheduler). Test targets are never installed and can depend on other test targets (only `TargetBinary` tests are executed). See `examples/multi-target.md`.

## Dependencies

### Declaring and Using Dependencies

```go
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("official/zlib >=1.2")
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").AddDeps("official/zlib")
})
```

### Dependency Wiring (Always Explicit)

Every target must declare its build-graph edges explicitly via `AddDeps`. There is **no** automatic wiring — `AddRequires` in `OnRequire` only declares the *package-level dependency* for resolution, not the *build-graph edge*.

- `AddRequires("official/zlib >=1.2")` — declares the package as a dependency (for version resolution and source download)
- `AddDeps("official/zlib")` — creates the build-graph edge (link + propagate public includes)

Both are needed. `AddRequires` alone will not link the library or propagate headers.

Run `vmake doctor` to detect packages that are missing explicit `AddDeps`.

### Dependency Format

- `"utils"` — same-package target
- `"lib:utils"` — specific cross-package target (build order + link + PublicIncludes)
- `"lib:*"` or `"official/zlib:*"` — wildcard: all targets from that package + transitive deps
- `"official/zlib"` — third-party package (expanded to all targets from that package + transitive deps)

Invalid refs (empty, whitespace, stray `:`, empty segments) are fatal at declaration time; unknown targets/packages and dependency cycles fail at build-graph time. In `pkg:target`, a `pkg` part without `/` is first resolved as a sub-package name relative to the declaring package (`ResolveSubPackageName`); on failure the error lists the tried candidates.

### Sub-Packages

A nested `build.go` inside a **native** remote package's checkout becomes a sub-package: an independently loaded package named `parent/sub` with its own options/targets/build dirs, versioned by the parent (not locked separately). Key rules:

- Registry (wrapper) packages have **no** sub-packages — by design (`docs/DESIGN_DECISIONS.md` DD-1)
- Sub-packages are lazy: their build.go is interpreted only when depended on (DD-3)
- Reference from outside by full name: `ctx.AddRequires("subtest/mother/sub_a")`, `AddDeps("subtest/mother/sub_a:*")` — the parent must be listed before its sub-packages in the same `AddRequires`
- Inside a sub-package, siblings can be referenced by short name: `AddRequires("sub_b")`, `AddDeps("sub_b:utils_b")`
- A parent cannot reference its own sub-packages in `OnRequire` (discovery runs after dep resolution); local projects have no sub-package concept (nested build.go are top-level packages)
- Example: `test_data/25_subpackage`

### Version Constraints

AddRequires accepts semver constraints: `"official/zlib >=1.2"`, `"official/curl ~8.5"`, `"test_build/mathlib"` (no constraint = any version).

Operators: `>=` and `>` (major-locked when major > 0 — `>=1.2` never matches `2.0`), `<=` / `<` (no major lock), `=` (exact), `~` (major.minor lock). Highest satisfying version is selected; multi-package constraints must be mutually satisfiable. See `references/api.md` for the full operator table and major lock semantics.

Version pins in `.vmake/config.json` entries (set via TUI) take precedence over latest matching tags, and `.vmake/vmake.lock` pins survive until `vmake lock update`.

### OnRequire Two-Phase Execution

`OnRequire` callbacks execute **twice** — once for discovery, once with real configuration:

| Pass | Phase | Config values | Purpose |
|------|-------|--------------|---------|
| 1 | Phase 1 | `nil` | Discover initial dependency graph. All packages (registry and native) are resolved eagerly. `OnRequire` runs for the first time here with nil config. |
| 2 | Phase 3 (`FilterDeps`) | Real values from `config.json` | After `OnConfig` has resolved all option values, `FilterDeps` re-runs every package's `OnRequire` with actual config. The returned dependencies **replace** `node.Deps`, then topology is re-sorted and needed packages are collected via BFS. |

This is what enables **option-conditional dependencies**. During discovery, direct reads (`ctx.Bool/String/Int`) are build errors — use the discovery-aware helpers. On pass 1 (nil config) `ctx.When(...)` returns `true`, `ctx.If(...)` returns its `then` arguments (nil config counts as true), and `ctx.Select(...)` returns `""`; on pass 2 all see real values:

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Option("use_ssl").SetType(api.OptionBool).SetDefault(false)
})

p.OnRequire(func(ctx *api.RequireContext) {
    if ctx.When("use_ssl", true) {
        ctx.AddRequires("official/openssl")
    }
})
```

**Same-set rule:** both passes must declare the same set of requires — only guard values may differ. A dependency that first appears in the FilterDeps pass (e.g. the guard was false with defaults, then the user enabled the option) is a build error; hoist the unconditional `AddRequires` out of the guard.

**Mechanism:** `FilterDeps` re-runs `OnRequire` for every package with real config values, replacing `node.Deps`. Then topology is re-sorted and needed packages collected via BFS from local roots.

**Key implication:** `AddRequires` alone does not guarantee a package is built — it must be reachable from a local root via BFS.

## Option & Conditional

```go
ctx.Option("debug").SetType(api.OptionBool).SetDefault(false)

AddCFlags(ctx.If("debug", "-g", "-O0"))   // []string flattens into ...any — no "..." spread
if !ctx.When("debug", true) {
	target.AddCFlags("-O2")
}
AddCFlags(ctx.Select("opt", map[string]string{
    "O0": "-O0", "O2": "-O2",
}))

ctx.String("name")
ctx.Int("count")
ctx.Bool("debug")
ctx.When("x", "val")

ctx.Option("chip").SetType(api.OptionChoice).SetValues("stm32f4", "esp32").
    SetOnApply(func(ctx *api.ConfigContext, val any) {
        ctx.SetProvidedLinkerScript("linker/" + val.(string) + ".ld")
    })

ctx.Option("trace").SetType(api.OptionBool).SetDefault(false).
    SetOnApply(func(ctx *api.ConfigContext, val any) {
        if val.(bool) {
            ctx.AddGlobalCFlags("-DTRACE=1", "-finstrument-functions")
            ctx.AddGlobalLdFlags("-ltrace")
        }
    })
```

- `SetOnApply(fn)` — callback invoked once per build after all option values are resolved (in sorted option-name order), receives `*ConfigContext` and `val any` **normalized to the declared type** (`bool` for OptionBool, `int` for OptionInt, `string` for OptionString/OptionChoice — `api.NormalizeOptionValue` converts JSON `float64` to `int` before the call). The callback's context carries real option values, so reading other options inside it works. Used to react to options (set global flags, choose linker script based on chip).

### OptionChoice Generates Dual Macros

When `GenerateConfigDefines` or `GenerateConfigHeader` processes a `Choice` option, it produces **two** entries:

- `CONFIG_{NAME}="<value>"` — the selection itself
- `CONFIG_{NAME}_{VALUE}=1` — a boolean for the specific choice

For example, `OptionChoice("platform").SetValues("linux", "windows")` with value `"linux"` generates:
```
CONFIG_PLATFORM="linux"
CONFIG_PLATFORM_LINUX=1
```

This lets code use either `#if CONFIG_PLATFORM_LINUX` (specific check) or switch on `CONFIG_PLATFORM` (general check). Note: `-D` defines pass the macro text directly (e.g., `-DCONFIG_PLATFORM_LINUX=1`), while `autoconf.h` writes `#define CONFIG_PLATFORM_LINUX 1`.

### SetConfigValue: Programmatic Override

`ctx.SetConfigValue(name, val)` in `OnConfig` changes an option value programmatically. Unlike `SetOnApply` (which only reacts), `SetConfigValue` changes the value that other parts of `OnConfig` will see:

```go
if ctx.String("chip") == "stm32f4" {
    ctx.SetConfigValue("use_fpu", true)
}
```

### Global Flags & Mode Flags

`AddGlobalCFlags/CxxFlags/LdFlags/Links` are only available on `ConfigContext` (mainly inside `SetOnApply`). They apply to ALL targets in ALL packages and are deduplicated. Global flags set by a package that `FilterDeps` prunes from the build are dropped — flags from unneeded packages never leak. Global flag changes also change the BuildKey, so artifacts rebuild correctly when flags change.

The compile merge order is: per-target flags → mode flags → global flags → dedup (first occurrence wins). For GCC, the last occurrence of repeated flags (`-O`) wins — mode overrides per-target, globals override mode, unless dedup removes the later exact duplicate.

Mode auto-injected flags (injected by scheduler, not via `AddGlobalCFlags`):

| Mode | Flags injected |
|------|---------------|
| `release` | `-O2 -DNDEBUG` |
| `debug` | `-O0 -g` |

During linking, global LD flags are appended after per-target flags; global links go inside `--start-group`/`--end-group`.

### GlobalOption Cross-Package Consistency

If two packages define the same global option via `GlobalOption()`, their `Type` and `Default` must be **identical** — otherwise the build fails with a fatal error. This constraint ensures all packages agree on the option's meaning. For example, if `chip/build.go` defines `GlobalOption("mcu").SetType(api.OptionString).SetDefault("stm32f405")` and `bsp/build.go` defines `GlobalOption("mcu").SetType(api.OptionChoice)`, the build will fail with a type mismatch error.

There is no merging of definitions: only `Type` and `Default` are validated for consistency; which definition supplies the merged view's `SetValues`/`SetDescription` is unspecified — prefer a single declaring package. Each declaring package's own `SetOnApply` callback still runs during that package's config pass.

### Default Build Flags

The builtin `host` toolchain contributes default C/C++/linker flags (hardening, warnings, `-ffunction-sections`), selected by the project's `target_os`. They are injected as the **base** of every new target — when you call `ctx.Target("app")`, the target's initial CFlags/CxxFlags/LdFlags are set from those defaults. Calling `AddCFlags(...)` appends to this base.

Toolchains declared by extensions contribute no default flags: `toolchain.json` describes which programs to run, not which CPU to target. A cross-compiling project supplies its own `-mcpu`/`-mthumb`/`--specs=` through `ctx.AddGlobalCFlags` / `AddGlobalLdFlags` in `OnConfig`, or per target with `AddCFlags`/`AddLdFlags`.

## RTOS / Embedded

### Simple chip package (no compilation, linker script only)

```go
// chip/build.go — provides linker script, no compiled output
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.SetProvidedLinkerScript("linker/sim.ld")
})
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("chip").SetKind(api.TargetVoid)
})
```

### Full HAL package with global flags (realistic firmware)

For real embedded projects, the chip/HAL package compiles startup code as a static library and sets global compiler/linker flags via `AddGlobalCFlags`/`AddGlobalLdFlags` in `SetOnApply`. See `examples/embedded-rtos.md` for the complete two-package pattern (chip + firmware with `UseDependencyLinkerScript`, post-link steps, `AddBinHeader`).

Key embedded rules: (1) Target-specific flags must appear in both CFLAGS and LDFLAGS to avoid ABI mismatches. (2) Use `-nostdlib` + `AddGlobalLinks("c_nano", "gcc")` in `SetOnApply` — this places libc inside `--start-group`/`--end-group` so arc dep symbols resolve. (3) `-specs=nano.specs` links libc after the group; if a dep provides symbols only libc references, use `EXTERN` in the linker script. See `references/gotchas.md` for the full explanation.

- `SetProvidedLinkerScript(path)` — chip/bsp declares linker script for consumers (fatal on double-set)
- `UseDependencyLinkerScript()` — firmware target auto-inherits `-T` from first dependency that provides one
- `SetLinkerScript(path)` — direct linker script on target (fatal on double-set)
- `AddPostLink(tool, args...)` — generic post-link, shorthands: `AddPostLinkHex/Bin/Size/Strip`
- `AddPostLinkOutputs(paths...)` — declare extra output files explicitly, with `{output}` templates or SourceDir-relative/absolute paths. Missing outputs trigger relink and all post-link steps; only declared outputs are automatically installed. Hex/Bin/Strip declare their outputs automatically. AddPostLink arguments never imply outputs, including positional inputs and `--add-gnu-debuglink={output}.debug`
- `AddPostLinkDeps(files...)` — declare extra post-link input files (SourceDir-relative, like `AddFiles`); any dep newer/missing → relink + re-run ALL post-link steps. Without it, editing a file consumed by a post-link step (e.g. an `objcopy --keep-global-symbols` list) is silently skipped
- `AddBinHeader(inputs...)` — binary files → `.h` headers
- RTOS tool accessors: `Package.ObjCopy()`, `Size()`, `ObjDump()`, `NM()`

Editing a linker script or version script triggers relink automatically.

### KConfig Preset Management (Firmware)

Use `ctx.KConfig("u-boot").AddPreset("rk3568_defconfig").SetDefaultPreset(...)` in `OnConfig`; select via `vmake config` TUI; call `pkg.EnsureConfig(srcDir)` in `SetBuildFunc`; use `SetKConfigPatches(map[string]string{...})` for post-defconfig overrides; register config files with `p.SetConfigFiles(".config")`. See `examples/firmware.md` for the full multi-package firmware pattern.

## Sub-Graph Build (Code Generation)

```go
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.BuildSubGraph("codegen")
    ctx.Exec(ctx.DepOutput("codegen:codegen"), "output/generated.h")
    ctx.DepBuildDir("codegen:codegen")

    ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
})
```

Use `ctx.ToolchainOption()` to allow per-package toolchain switching for sub-graph builds.

## Stamp-Based Skip (Void Targets)

Local void targets use `.vmake_stamp` in `BuildDir` for incremental builds. Stale when the **content hash** (SHA-256) of files registered via `p.SetConfigFiles(".config")` changes, the git HEAD revision changes, the stamp file is missing/corrupt, or a **dependency artifact is newer than the stamp**. Target source file mtimes are NOT checked — only SHA-256 content hash and git commit hash (dependency artifacts are compared by mtime).

Use `SetConfigFiles` on `*Package` (in `OnPackage`) to declare which files invalidate the stamp.

**InstallDir changes the skip mechanism entirely.** When a void target has `InstallDir` set (remote packages — their `InstallDir` is `<version>/out/<buildKey>/install` in the global cache), the scheduler checks whether `InstallDir` exists and contains files — if it does, the target is skipped. `.vmake_stamp` is **not** consulted. Force rebuild by deleting the install directory (e.g. `vmake pkg clean <repo/name>`; `vmake clean` only touches local packages' build dirs).

## Install

| Flag | Description |
|------|-------------|
| `--install` / `-i` | Install after build |
| `--prefix` / `-p` | Prefix (default: `./install/`) |
| `--install-type` | `runtime` (binaries+shared) or `sdk` (everything) |

Custom install entries: `ctx.AddInstalls("src/file.conf", "etc/file.conf")` (available in `OnBuild` and `OnInstall`).

### OnInstall Lifecycle

`OnInstall` runs during `--install`, right after all builds succeed. Use `ctx.SetPrefix()` for per-package prefix overrides and `ctx.AddInstalls()` for extra file copies (docs, configs, licenses) — these are installed together with target outputs. Test targets are never installed; without `--install-type sdk`, static libraries are skipped at install. See `examples/on-install.md`.

## Build Scope

vmake builds packages by BFS from local (directory-based) packages. Remote packages are only built if reachable from a local package's transitive dependency chain. If you `AddRequires("pkg")` but no local package depends on it, the package won't be built.

## Reproducible Builds (vmake.lock + --manifest)

Remote dependency versions and commits are pinned in `.vmake/vmake.lock` after resolution — commit it alongside `.vmake/config.json`. Subsequent builds reuse locked versions; new upstream tags never change what you build until you run `vmake lock update`.

For CI/CD, pin from an install manifest instead:

```bash
# First build: install and write install/manifest.json (versions + revisions)
vmake build --install

# Later build: import manifest pins into vmake.lock BEFORE resolution, then build
vmake build --manifest install/manifest.json
```

`--manifest` imports the recorded git URLs, refs, and revisions into `vmake.lock` before dependency resolution, so the graph is built from the pinned versions (local `SetGit` packages are also checked out to recorded revisions). `vmake manifest show install.json` displays contents; `vmake manifest checkout install.json` restores sources without building. With a valid lock entry and cached checkout, resolution is fully offline.

## CLI Quick Reference

| Command | Description |
|---------|-------------|
| `vmake build` | Build (`-j N` parallel jobs: packages and per-target compiles, `-k` keep-going after failure) |
| `vmake build --tests` | Build including test targets |
| `vmake test` | Build + run test targets |
| `vmake rebuild` | Clean + build |
| `vmake config` | TUI for options (`--set opt=val` / `--set pkg/opt=val` non-interactive) |
| `vmake clean [--all]` | Execute OnClean hooks then remove build artifacts |
| `vmake distclean [--purge-cache]` | Deep clean: artifacts + install/ + vmake_deps/ (+ global cache entries) |
| `vmake query` | Dependency tree; `query targets`, `query config <pkg>` |
| `vmake lock update/show` | Re-resolve / print `.vmake/vmake.lock` pins |
| `vmake toolchain list/show` | Toolchain info |
| `vmake repo add/list/remove/update/trust/untrust` | Package repos + trust management |
| `vmake pkg list/search/clean/update` | Packages (`pkg update <repo/name>[@version] [--dry-run]`) |
| `vmake ext add/list/remove/update` | Extension repos |
| `vmake manifest show/checkout` | Install manifest |
| `vmake check-symbols [--strict]` | Audit exported symbols via nm -D (Linux only) |
| `vmake doctor` | Diagnose platform prerequisites (symlinks, Git userland, make, toolchain) and build.go issues |
| `vmake init-editor` | Generate go.mod so gopls supports build.go |
| `vmake git tag` | Version tagging |
| `vmake skill install/uninstall/path` | AI skill management (`install --project` also installs into ./.claude/skills/) |
| `vmake update [version]` | Update vmake |
| `vmake version` | Version info |

Build flags: `--mode`, `--toolchain`, `--install/-i`, `--prefix/-p`, `--install-type`, `--manifest`, `--tests`, `--jobs/-j`, `--keep-going/-k`
Global flags: `-v` verbose, `-V` very-verbose, `-q` quiet, `-y/--yes` assume yes (trust prompts)

## Reading Guide

- **Learning the basics** → Start with `examples/simple.md`, then `examples/config.md`
- **Writing a build.go** → Follow the Decision Guide above to pick the right example
- **Mapping config to defines** → `examples/config-to-define.md` (three mechanisms compared)
- **Multi-module workspace** → `examples/multi-module.md`
- **OnClean / OnInstall lifecycles** → `examples/on-clean.md`, `examples/on-install.md`
- **Looking up a specific API** → See `references/api.md` for complete method signatures
- **CLI usage** → See `references/cli.md` for full command tree
- **Directory / path resolution details** → `references/dirs.md` (BuildKey, SetGit paths, SourceDir vs SrcDir)
- **Advanced gotchas** → `references/gotchas.md` (static lib deps, OnApply/global flags, source patching)
- **Advanced patterns** → `examples/complete.md`, `examples/subbuild.md`, `examples/embedded-rtos.md`, `examples/firmware.md`, `examples/config-propagate.md`, `examples/prebuilt.md`, `examples/third-party-wrapper.md`

## Key Conventions

- Use `filepath.Join()` for filesystem paths
- Package IDs use `/`: `official/zlib`
- Target IDs use `:`: `lib:utils`
- `OnPackage` with `SetGit`/`AddVersion` works for both registry packages and local packages wrapping remote libraries
- `OnPackage` and `AddKConfig` are single-slot — a second registration is a build error (one kconfig entry per package)
- `SetLanguages()` exists but has no effect — language is auto-detected from file extension
- For **local** packages using `SetGit`, `AddFiles` paths resolve from `SourceDir()` — always prefix with `"src/"` (registry/native packages resolve from the checkout root directly)
- Relative file IO inside build.go resolves against the build.go's directory (see Script-relative file IO above)
- Pass `[]string` directly to `Add*` methods — never spread with `...` (yaegi limitation)
