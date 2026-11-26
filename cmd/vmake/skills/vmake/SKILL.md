---
name: vmake
description: VMake C/C++ build system, Go build.go scripts interpreted by yaegi. Load this skill before the first vmake command of a session (vmake, build, rebuild, config -s, distclean, clean, inspect, query, test) and before editing build.go or interpreting any build output, compiler flag, toolchain choice or install/ artifact. It is authoritative over guesses — compilers are resolved internally (never export PATH/CC/CFLAGS), mode flags (release -O2 -DNDEBUG, debug -O0 -g) vs AddGlobalCFlags merge order, BuildKey invalidation and when distclean is useful, debug symbols (-g/DWARF) and what a .map file cannot do, CMake/Make/Autotools wrapping, cross-compilation, packages and repositories, and diagnosing vmake failures.
---

# VMake Build System

VMake builds C/C++ projects using Go scripts (`build.go`) interpreted at runtime by [yaegi](https://github.com/traefik/yaegi). There is no DSL — build scripts are standard Go files using `github.com/spock2300/vmake/pkg/api`.

## 0. Before You Run vmake

Four rules prevent the expensive misunderstandings; each links to its section.

1. **Compilers are resolved, never inherited.** Do not `export PATH`, `CC`,
   `CXX`, `AR` or `LD` to make a build pass. vmake invokes a resolved toolchain
   by absolute path, including for CMake stages such as picolibc; the built-in
   `host` still resolves its tools from PATH. A missing compiler means
   `vmake toolchain list`, a `toolchain.json`, or an extension — see
   *Toolchains & Compiler Discovery*. Hand-run tools (`nm`, `objcopy`,
   `addr2line`) and some host test scripts do need the toolchain on PATH.
2. **Private target, mode, global, then public flags.** `release` = `-O2 -DNDEBUG`,
   `debug` = `-O0 -g`; a global `-O3` overrides the mode, while `AddPublicCFlags`
   and propagated dependency public flags can still override the global flag. A
   global `AddGlobalCFlags("-g3")` keeps release optimization *and* adds debug
   info. Global flags are buffered per package and applied only to packages that
   survive `FilterDeps` — see *Global Flags & Mode Flags*.
3. **Only some changes rotate the BuildKey.** Global flags (including the
   `SetDefaultVisibilityHidden` marker), mode, toolchain, package options, the
   local `SetGit` commit, and remote version/commit/patch rotate it. A local
   patch-only change does not rotate; remote patch content does. Target flags,
   defines, and file lists are action signatures: they rebuild in the same
   `BuildDir`. A normal build.go edit does not require `vmake distclean`; use it
   to remove old build keys after a key rotation. `install/` keeps the old
   artifact until `vmake build -i`.
4. **`-g` is host-side data.** DWARF grows the ELF (megabytes) but not the
   flashed image; a source-level debugger needs an ELF built with `-g`, and a
   `.map` file carries addresses only — no lines or variables. Rebuilt images
   still differ whenever embedded `__FILE__` build paths change, so reflash or
   re-verify before debugging against a fresh build.

## Quick Commands

```bash
vmake                                  # Build all targets (default command)
vmake build -j 8                       # Parallel compilation within each target (-j0 = CPU count)
vmake rebuild                          # Clean + build in one step
vmake test                             # Build & run native test targets (CTest-style)
vmake config --set //:opt=val          # Non-interactive config override (format: [pkg:]opt=val)
vmake config --unset //:opt            # Remove explicit override (revert to default)
vmake config list                      # List tracked configurations under configs/
vmake config use <profile>             # Set local active configuration cursor (.vmake/local.json)
vmake config check --all               # Validate all tracked configurations under configs/
vmake build -c <profile>               # Build a specific configuration statelessly
vmake inspect                          # Dry-run JSON project model (may initialize .vmake/ and .gitignore)
vmake inspect --no-targets             # Package/dependency JSON without running OnBuild
vmake pkg search tls --details         # Search names, descriptions and licenses in trusted repositories
vmake pkg show official/zlib           # Package metadata, versions and AddRequires snippet
vmake pkg link <repo>/<pkg> ../<pkg>   # Compile from a local git tree; removes stale .vmake_deps src
vmake pkg unlink <repo>/<pkg>          # Next build uses vmake.lock; run deps update for newer versions
vmake deps status                      # Show dependency versions, pins, working trees and cache state
vmake deps graph                       # Unified dependency graph (--targets, --format dot, --format mermaid, --json)
vmake deps why <pkg>                   # Dependency chain from root to <pkg> or <pkg>:<target> (--all, --json)
vmake deps check                       # Audit DAG cycles, internal/ visibility, and header boundaries
vmake deps sync [--prune]              # Reconcile working trees and vmake.lock without floating locked packages
vmake deps update [<pkg>[@<ver|commit>]] [--dry-run] [--refresh-repos] [--prune] # Re-resolve all or selected dependencies in vmake.lock
vmake deps prune [--dry-run] [--json]  # Remove lock entries not needed by the active configuration
vmake deps pin <pkg>@<ver|commit>      # Pin a dependency version/commit in .vmake/project.json and vmake.lock
vmake deps unpin <pkg>                 # Remove a dependency pin from .vmake/project.json
vmake deps fetch [pkg...] [--all] [--offline] # Materialize locked dependency sources without building
vmake query                            # Dependency tree
vmake query targets                    # Targets without building (local + remote)
vmake check-symbols                    # Verify ELF symbols against policy
vmake clean                            # Clean build outputs
vmake distclean                        # Reset project (keeps sources unless --purge-sources)
vmake doctor [--toolchain <name>]      # Check environment and toolchain readiness
vmake toolchain list                   # List toolchains
vmake toolchain show <name>            # Toolchain details and resolved tool paths
```

- **Local Source Development**: `vmake pkg link <target> <path>` (run in the consuming project) builds that container or `SetGit` source from a local git worktree and removes any existing `.vmake_deps/.../src` and `state.json` for it; per-build outputs and own-Git `third_party/` sources stay. Supported targets are native containers (`<repo>/<module>`), registry packages (`<repo>/<pkg>`), native `SetGit` members (`<repo>/<module>/<path>`), and local `SetGit` packages (`//<path>`). Check `vmake pkg list` (appears under `Source links:`), `vmake deps status`, and optionally `vmake inspect --no-targets` (`linked: true`, version `0.0.0-local`). A native container link supplies `vmake.json`, `build.go` members, `OnConfig` options, and flags/defines; a registry or `SetGit` source link keeps the wrapper `build.go` and replaces only upstream source. No commit, tag, version bump, or `vmake deps update` is required to test; uncommitted edits are compiled.
- **Switching Back from a Link**: `vmake pkg unlink <target>` removes the link; the next build uses the existing `vmake.lock` entry when present (otherwise it resolves and writes one), and untagged linked HEADs are not selected. Run `vmake deps update <target>` only if you want to re-resolve newer versions before building (project pins and `vmake.json` constraints still apply).
- **Build & Test Reports**: `build`/`rebuild` write `build/vmake-build.json` and `test` writes `build/vmake-test.json` (schemaVersion 2: status, duration, concise error, parsed compiler/linker diagnostics, target timings, test results, SHA-256 installed artifacts). `clean`/`distclean`/`pkg clean` remove both. Full schema: [references/build-result.md](references/build-result.md).
- **Package Discovery**: `pkg search` scans repository directories and matches repository/package names by default; `--details` reads `build.go` only for trusted repositories and also matches description, license and latest version. `--for-project` adds cached/locked/installed status; installed status uses the default `<project>/install/manifest.json`, so custom `--prefix` installs are not detected. `pkg show` prints metadata and an `AddRequires` snippet. Native repositories only expose packages already cached in the current project; native sub-packages are not listed separately and are read through their parent package. Neither command writes an index or cache.
- **Inspection & Dry-Run Discipline**: `inspect`, `query`, `query targets`, `deps graph`, `deps why`, `deps check`, and `check-symbols` evaluate `OnBuild` in dry-run mode with read-only `scriptfs` (`p.DryRun() == true`); `inspect --no-targets` skips `OnBuild` entirely, and `query config <pkg>` prints effective option values and generated defines without evaluating `OnBuild`. `check-symbols` keeps the read-only guard enabled for the whole open/inspect/declare sequence and reports rejected writes as `blocked-write` error findings; a rejected write that propagates as a script error aborts the command before the audit. Prefer declarative `p.WriteBuildFile`, `p.GenerateFile`, `p.Generate`, and `p.BuildPath` in `OnBuild` (they defer writes to scheduler execution and emit `$builddir/...` when `BuildDir()` is empty); guard any procedural `os.WriteFile` on `!p.DryRun()`. Inspecting a missing source tree does not fetch it, but native remote resolution may still materialize one. Full schema: [references/inspect.md](references/inspect.md).
- **Full CLI Reference**: See [references/cli.md](references/cli.md).
- **Stale Skill**: If a command here fails with an unknown flag or subcommand, the installed skill may be older than the `vmake` binary. Run `vmake version` and reinstall with `vmake skill install`.

## Toolchains & Compiler Discovery

vmake resolves compilers itself. Do not `export PATH`, `CC`, `CXX`, `AR`, or `LD`, and do not edit shell profiles to make a build pass. The built-in `host` toolchain still resolves its tools from PATH, and `git`/`make`/`cmake` are separate dependencies; see *Before You Run vmake* for hand-run tools and host test scripts.

Resolution order:

1. A package config entry `entries.<pkg>.options.toolchain` for that package.
2. `--toolchain <name>`.
3. The project `toolchain` option in the active configuration.
4. The `PreferredToolchain` of the active chip profile after `FilterDeps`.
5. The built-in `host` toolchain.

An embedded project declares the chip profile in the chip package's `OnConfig`:

```go
ctx.SetChipProfile(api.ChipProfile{
    Name:               "py32f539",
    PreferredToolchain: "arm-none-eabi",
    TargetOS:           "none",
    TargetTriple:       "arm-none-eabi",
})
```

The root package declares the global `chip` choice; vmake appends profile
names to it but does not create it. Every resolved package (local or remote)
may declare chip profiles; only packages in the filtered build participate in
selection, and two needed packages with different profiles are a configuration
error (an explicit `--toolchain` or `global.toolchain` bypasses that check).
Duplicate declarations of
the same chip name must match exactly or configuration fails. The TUI's `chip` choice
switches the toolchain and target preview, and derived values are not pinned
into the active `configs/*.json` file. Explicit configuration still wins.

`FilterDeps` can read the configured values of global options that the package
declares, so the root's `OnRequire` can use `ctx.When(api.ChipOptionName, ...)`
or `ctx.Select(api.ChipOptionName, ...)` to select boards and chip-specific
packages.

How the selected toolchain resolves its tools:

- Built-in `host`: `gcc`/`g++`/`ar`/`ld` plus `strip`/`ranlib`/`objcopy`/`size`/`objdump`/`nm` from PATH on Linux; macOS uses `cc`/`c++` and leaves `objcopy`/`objdump` unset. A Linux host with only clang is not the built-in `host`.
- Extension `toolchain.json`: when `InstallPath` is set, relative tool names resolve only under `<InstallPath>/bin` with no PATH fallback; absolute tool names are used as-is. A manifest with no `installations` at all has no `InstallPath` and relative names resolve from `PATH`. A manifest with `installations` but no current-host entry is rejected; an uninstalled host entry installs on a build with `--toolchain` (while `toolchain show`/`doctor` may still resolve relative names from PATH). Installed toolchains live in `~/.vmake/toolchains/<os>/<arch>/<name>/<version>/`.

A package-level `entries.<pkg>.options.toolchain` value therefore overrides `--toolchain` for that package; `--toolchain` overrides the project default, not a package entry.

Inspect instead of guessing:

```bash
vmake toolchain list
vmake toolchain show <name>
vmake doctor --toolchain <name>
```

Inside `OnBuild`, `p.Env()["CC"]` is the resolved absolute path. `p.Env()` also includes `CFLAGS` and the other tool variables, plus `CROSS_COMPILE` when the toolchain sets a prefix; it includes `PATH` when the toolchain contributes directories (an installed toolchain or absolute compilers). `p.CC()` returns the configured name, which may be relative. Use `p.CMakeConfigure()` and `p.Make()` rather than reconstructing compiler paths.

If the compiler is missing, run `vmake toolchain list` first. A declared but uninstalled toolchain installs on the first build that selects it; to prefetch the current host's archives explicitly, run `vmake ext provision <repo>` after the extension is trusted. A compiler outside PATH needs a `toolchain.json` entry or an extension repository, not a PATH export. See [references/toolchains.md](references/toolchains.md).

## Platform Prerequisites

- **Linux**: the built-in `host` toolchain uses `gcc`/`g++` from `PATH`; a clang-only host must declare a toolchain that names its clang executables.
- **macOS**: the built-in `host` toolchain uses `cc`/`c++` and leaves `objcopy`/`objdump` unset; Kconfig/kernel/U-Boot builds need GNU Make 3.82+ and a toolchain whose `make` is `gmake` (`vmake doctor` warns below 3.82).
- **Windows**: vmake storage needs real symlinks (Developer Mode or an elevated shell; no junctions); the full Git for Windows userland is required (MinGit is insufficient) and supplies neither a compiler nor make; use MinGW-w64 for native Windows and the ARM GNU Toolchain for bare metal (MSVC is out of scope); CMake helpers default to Ninja.
- `vmake doctor [--toolchain <name>]` checks symlinks, the Git userland on Windows, make, the selected toolchain, and whether the installed skill matches this binary (a stale copy is an error; reinstall with `vmake skill install`), but not CMake/Ninja; full host requirements: [references/platforms.md](references/platforms.md).
- **Cross targets**: `target_os`/`target_triple` control artifacts and the compiler target; `vmake test` refuses cross targets and points to `vmake build --tests`.

## Build Script Structure

Every `build.go` uses `package main`, imports `"github.com/spock2300/vmake/pkg/api"`, and exports `func Main(p *api.Package)`:

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.AddRequires("//utils", "official/zlib") // Pure package IDs (constraints live in vmake.json)
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("enable_tls").SetDefault(false).SetDescription("Enable TLS")
    })
    p.OnRequire(func(ctx *api.RequireContext) {
        if ctx.When("enable_tls", true) {
            ctx.AddRequires("official/mbedtls")
        }
    })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.GenerateConfigDefines() // Emit CONFIG_<OPT> defines for package options
        ctx.Target("myapp").SetKind(api.TargetBinary).
            AddFiles("src/*.c").AddIncludes("include").
            UseRequires() // Automatically wire all active package requires as deps
    })
}
```

## Directory Structure

```text
myproject/
├── vmake.json                # Module container manifest: roots, modules, sources (optional for single root build.go)
├── build.go                  # Optional root package (canonical ID "//")
├── configs/                  # Tracked configurations (configs/default.json, SKU profiles)
├── .vmake/                   # project.json (default_config & pins), local.json (local cursor & links), vmake.lock (v2)
├── .vmake_deps/              # Materialized external containers & SetGit trees
├── build/                    # compile_commands.json, vmake-build/test.json; root-package outputs at build/<keyDir>/
│                             # nested local packages use <pkgDir>/build/<keyDir>/, external packages use .vmake_deps/.../out/
└── install/                  # Staging: bin/, lib/, include/, manifest.json
```

See [references/dirs.md](references/dirs.md) for path accessors (`SourceDir()`, `SrcDir()`, `BuildDir()`, `InstallDir()`), build-key hashing, shared git cache (`~/.vmake/cache/git`), and lockfile rules.

## Lifecycle & Build Order

The high-level phases are listed below. During script loading, each package runs `Main`, then its `OnPackage`, then its `OnRequire` discovery before its dependencies are loaded; `OnPackage` callbacks therefore interleave with dependency loading rather than all running before every `OnRequire`.

1. **`OnPackage`** — git URL (`SetGit`), patches (`AddPatches`), Kconfig metadata.
2. **`OnRequire` (Discovery pass)** — eager dependency discovery before config values exist (`When`/`WhenNot`/`WhenAny` return `true`).
3. **`OnConfig` + `OnConfigureDeps` + `SetOnApply`** — declare options, load `configs/<active>.json`, propagate top-down defaults along the DAG (`OnConfigureDeps`), and run `SetOnApply` callbacks for resolved packages (option names sorted within each package). Buffered global C/CXX/LD flags and links from packages later pruned by `FilterDeps` are filtered out, and global-option macros are recomputed over the filtered set.
4. **`FilterDeps` (`OnRequire` re-run)** — prune unreachable conditional dependencies using resolved entry option values and the configured values of global options the package declares.
5. **Source Preparation** — fetch/checkout git sources, apply git patches, write `.vmake/vmake.lock` (or verify it under `--locked`), restore Kconfig `.config` files.
6. **`OnBuild`** — declare targets; scheduler executes targets serially in topological dependency order with parallel C/C++ compilation within each target.
7. **`OnInstall`** — only with `--install`: register extra install entries/filters/prefix (declaration only; no copying). After the callbacks, the installer copies target outputs and registered items into `install/` and writes `install/manifest.json`, reusing `BuildContext`s without replaying `OnBuild`.

## Module Containers & Package Types

VMake 2.0 separates **module containers** (distribution, versioning, and git checkouts declared in `vmake.json`) from **build packages** (any directory with a `build.go`, identified strictly by its container + relative path, loaded lazily on demand — no directory scanning).

| Type | Canonical ID / Manifest | Characteristics |
| --- | --- | --- |
| **Root Manifest** | `vmake.json` | Declares `roots` (entry packages, default `["//"]` if root `build.go` exists), `dev_roots` (local-only entry packages when `roots` is omitted; ignored externally), `exports` (public package whitelist across container boundaries), `repos` (self-contained native repo URL templates), `modules` (container version constraints or local `{"path": "./..."}`), and `sources` (constraints for `SetGit` packages) |
| **Local Package** | `"//"` (root) or `"//<rel/path>"` | Identified strictly by path relative to the workspace root (e.g. `"//libs/math"`, `"//apps/cli"`). Loaded lazily from `roots` and `AddRequires` |
| **Native Module Container** | `<repo>/<module>` (members `<repo>/<module>[/<rel/path>]`) | Git repo registered via `vmake repo add --native`; no root `build.go` is required. Members load lazily by path; intra-container references use `"//<rel/path>"` which resolves inside the same container |
| **Registry Package** | `<repo>/<pkg>` (container `<repo>`) | Wrapper `build.go` under `~/.vmake/repos/<repo>/packages/<first-char>/<pkg>/`; declare `"<repo>": "*"` in `vmake.json` `modules`, and optional source constraint in `sources` |
| **Independent Upstream Source (`SetGit`)** | Any local or native package calling `p.SetGit(...)` | Orthogonal to container identity: local `SetGit` clones to `.vmake_deps/local/<memberDir>/src` (symlinked at `<pkgDir>/src`); native `SetGit` members clone to `.vmake_deps/<repo>/<module>/third_party/<memberDir>/src`; versions/commits lock in `vmake.lock` `sources` |
| **Local Source Link** | `.vmake/local.json` | `vmake pkg link <target> <git-tree>`; supports native containers, registry packages, native `SetGit` members, and local `SetGit` packages (`//...`). Ignores version/lock/constraints while linked; `pkg unlink` restores lock resolution |

## Target API at a Glance

```go
t := ctx.Target("mylib").
    SetKind(api.TargetStatic).                  // TargetBinary, TargetStatic, TargetShared, TargetObject, TargetVoid
    AddFiles("src/*.c", "src/**/*.cpp").        // Globs relative to SourceDir()
    RemoveFiles("src/excluded.c").              // Exclude specific files/globs
    AddIncludes("src/internal").                // Private include dirs (-I)
    AddPublicIncludes("include").               // Propagated to dependents along AddDeps
    AddDefines("VERSION=2", "DEBUG").           // Private defines (-D)
    AddPublicDefines("MYLIB_API_V2=1").         // Propagated to dependents along AddDeps
    AddCFlags("-Wall", "-O2").                  // Private C flags (also AddPublicCFlags)
    AddCxxFlags("-std=c++17").                  // Private C++ flags (also AddPublicCxxFlags)
    AddLdFlags("-Wl,--gc-sections").            // Private linker flags (also AddPublicLdFlags)
    AddLinks("m", "pthread").                   // Private -l libs (also AddPublicLinks)
    AddDeps("//math", "official/zlib")          // Target & package dependencies
```

- **Target-level Public Propagation (`AddPublic*`)**: `AddPublicIncludes`, `AddPublicDefines`, `AddPublicCFlags`, `AddPublicCxxFlags`, `AddPublicLdFlags`, and `AddPublicLinks` (plus `RemovePublic*` / `Public*`) propagate transitively along `AddDeps` edges to downstream targets. Prefer `AddPublic*` over `AddGlobal*` whenever flags/defines belong to a library's consumers rather than every package in the workspace (global flag changes rotate every package's `BuildDir`).
- **Non-linkable Objects/Archives (`SetLinkable(false)`)**: `t.SetLinkable(false)` compiles a `TargetObject` or `TargetStatic` and exposes its output via `ctx.DepOutput(...)` without automatically linking its `.o`/`.a` into downstream `TargetBinary`/`TargetShared` targets that depend on it.
- **Opt-in Incremental `TargetVoid` (`AddInputs` / `AddOutputs`)**: By default, a reachable `TargetVoid` runs its `SetBuildFunc` once per invocation. Declaring **both** `t.AddInputs(...)` and `t.AddOutputs(...)` enables VMake-managed incremental stamp checking — the callback is skipped only when the action record matches, declared input contents and output size/mtime are unchanged, and no declared post-link command changed. Missing or modified outputs rerun it; the opaque callback body is not hashed, so declare its real inputs/outputs.
- **Full API Signatures**: See [references/api.md](references/api.md).

## Prebuilt Libraries

Wrap pre-compiled `.a`/`.so`/`.dll` artifacts (or CMake/Make install outputs) with `SetPrebuilt(path)`:

```go
ctx.Target("z").SetKind(api.TargetStatic).
    SetPrebuilt(filepath.Join(p.CMakeInstallDir(), "lib/libz.a")).
    AddPublicIncludes(filepath.Join(p.CMakeInstallDir(), "include")).
    AddPublicLinks("m") // Propagate transitive system libs to consumers
```

## Symbol Management

Control ELF symbol visibility, localization, stripping, and symbol verification policies:

```go
ctx.SetDefaultVisibilityHidden()                // Package-local -fvisibility=hidden (in OnConfig / SetOnApply)
t.SetKeepGlobalSymbols("include/public.sym")    // Post-link objcopy --keep-global-symbols=<file> + auto AddPostLinkDeps
t.SetSymbolPrefix("vendor_")                    // Prefix all symbols via objcopy --prefix-symbols
t.SetVersionScript("exports.map")               // -Wl,--version-script= on ELF Shared/Binary
t.AddExcludeLibs("libinternal")                 // -Wl,--exclude-libs=libinternal on ELF Shared/Binary
t.SetSymbolBinding("static-functions")          // -Wl,-Bsymbolic-functions on ELF Shared/Binary
```

## Test Targets

Mark native test binaries with `SetTest(true)`:

```go
ctx.Target("math_test").SetKind(api.TargetBinary).SetTest(true).
    AddFiles("tests/math_test.c").AddDeps("math")
```

- Test targets are **excluded from normal `vmake build` and `install/`**.
- `vmake test` builds and executes native test targets (`-j N`, `-k`, `--mode`, `-s`, `--toolchain`, `--manifest`, `--explain`, `--locked`, `--offline`), writing `build/vmake-test.json`. There is no package/target filter flag.
- For cross/bare-metal targets (`target_os=none`), compile test binaries without running them via `vmake build --tests`.

## Dependencies

Declare package dependencies on `Package` (`p.AddRequires(...)`) or conditionally in `OnRequire` (`ctx.AddRequires(...)`), and wire target build edges in `OnBuild`. `AddRequires` takes **pure package IDs only** (no inline version constraints — put container constraints in `vmake.json` `modules` and `SetGit` constraints in `vmake.json` `sources`):

```go
p.AddRequires("//shell", "//wifi", "official/cjson") // Single-entry static package requires
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("firmware").SetKind(api.TargetBinary).
        AddFiles("src/*.c").
        UseRequires() // Equivalent to AddDeps("//shell:*", "//wifi:*", "official/cjson:*") for all active requires
})
```

- **Canonical Package IDs & Target References**: Every local package ID is `"//"` (root) or `"//<rel/path>"` (e.g. `"//libs/shell"`, `"//drivers/wifi"`). External packages are `"<repo>/<module>"` or `"<repo>/<module>/<rel/path>"` (native containers) and `"<repo>/<pkg>"` (registry). Inside a native container, `"//<rel/path>"` resolves to a sibling package inside that same container (`<repo>/<module>/<rel/path>`). In `AddDeps`, a bare name (`"helper"`) always means a same-package target; cross-package references always use a canonical package path (`"//shell"`, `"//shell:lib"`, `"official/cjson"`, `"official/cjson:*"`).
- **Enforced Go-Style Architecture Boundaries**: Builds (`build`/`rebuild`/`test`) and `vmake deps check` reject: (1) **package-level and target-level cycles**, including cross-package reverse target edges (`pkgA:t1 -> pkgB:t2` plus `pkgB:t3 -> pkgA:t4`), reporting minimal cycle chains; (2) **`internal/` visibility violations**, where any local package or native container member path containing an `internal` directory segment is imported outside the subtree rooted at `internal`'s parent; and (3) **cross-package path reaches and private/undeclared `#include`s**, ensuring packages do not reach into sibling/parent package directories via `../`, cross-package headers belong to declared dependencies' `AddPublicIncludes` or `InstallDir/include`, and direct source `#include`s come from direct `AddDeps`/`UseRequires` dependencies (verified via `.d` depfiles when built and static `#include` scanning on cold trees). Use `vmake deps graph`, `vmake deps why <pkg|pkg:target>`, and `vmake deps check` (supporting `--json`, `--locked`, `--offline`, `-s`) to inspect and audit the unified graph.

### Module Containers & vmake.json

Multi-package workspaces and projects with external dependencies declare a root `vmake.json`:

```json
{
  "roots": ["//apps/firmware"],
  "modules": {
    "official": "*",
    "myrepo/sdk": ">=1.2.0"
  },
  "sources": {
    "official/zlib": ">=1.2.11"
  }
}
```

- `roots`: entry package IDs (`"//"` or `"//<rel/path>"`). Optional when a root `build.go` exists (defaults to `["//"]`).
- `dev_roots`: local-only entry packages active when this container is opened as the root workspace and `roots` is omitted (for example SDK internal hardware self-tests); ignored when another project consumes the container externally.
- `exports`: optional container public surface whitelist (`{"publicName": "//internal/rel"}`, 1-to-1). When present, external consumers may only depend on or configure listed export names (`<container>/<publicName>`); unlisted members remain private to the container. When omitted, all non-`internal/` members remain directly importable.
- `repos`: optional self-contained native repository declarations (`{"myrepo": {"native": "https://.../myrepo/{name}.git"}}`), allowing projects to build without prior global `vmake repo add --native` (global `~/.vmake/repos` still overrides; trust is still required and bound to `(repo, url-template)` via `vmake repo trust <name> --url <template>`).
- `modules`: external containers required by this module and their version constraints (`"*"` for registry repositories, semver constraints for native containers, or object form `{"version": ">=1.2.0"}` / `{"path": "../sdk"}` for local path containers that do not lock in `vmake.lock`). A native container does **not** need a root `build.go`; consumers can require `"myrepo/sdk/hal"` directly as long as `"myrepo/sdk"` is declared in `modules`.
- `sources`: optional version constraints for independent `SetGit` upstream sources (registry packages, local `SetGit` packages, or native `SetGit` members).

### Version Constraints & `vmake.lock` (v2)

Constraints in `vmake.json` (`modules` and `sources`) support one operator plus one version (`>=`, `>`, `<=`, `<`, `=`, `~`; `"*"` means any stable version). `>=`/`>` are major-locked when the major is > 0; `<=`/`<` are not. Prereleases match only when the constraint pins a prerelease of the same `major.minor.patch`. A bare version is `>=`, and exact selection requires `=`. When multiple modules constrain the same container, VMake intersects all constraints across the diamond and selects the highest satisfying tag (or fails on conflict). Registry packages (and local/native `SetGit` packages) without declared versions track the default branch and lock its resolved `HEAD` commit with `version: ""`. `.vmake/vmake.lock` uses schema `version: 2` with separate `containers` (native/registry container versions & commits) and `sources` (independent `SetGit` packages). Multi-SKU projects share one `vmake.lock` incrementally: normal builds, `vmake deps sync`, and `vmake deps update` retain lock entries belonging to inactive configurations (and `--locked` tolerates them); run `vmake deps prune` (or pass `--prune` to `deps sync`/`deps update`) to remove unused entries. Project-wide pins are managed via `vmake deps pin <target>@<ver|commit>` (`pins` in `.vmake/project.json`). While linked via `pkg link`, the preserved lock entry is ignored and `vmake deps update` keeps it; after `pkg unlink`, run `vmake deps update` to re-resolve versions.

### OnRequire Two-Phase Execution

`OnRequire` runs **twice**:
1. **Discovery (Phase 1)**: Config values do not exist yet. Direct reads (`ctx.Bool`, `ctx.String`, `ctx.Int`) are **fatal**. Use discovery-safe helpers:
   - `ctx.When(opt, val)` — equals `val` (returns `true` during discovery)
   - `ctx.WhenNot(opt, val)` — does not equal `val` (returns `true` during discovery)
   - `ctx.WhenAny(opt, val1, val2, ...)` — equals any of `vals` (returns `true` during discovery)
   - `ctx.If(opt, then...)` — returns `then` during discovery, or when bool `opt` is true
   - `ctx.Select(opt, map)` — returns all mapped values during discovery, or the active choice's mapped value in `FilterDeps`/`OnBuild` (pass directly to `AddRequires` / `AddDeps` / `AddCFlags`)
2. **FilterDeps (Phase 2)**: Runs after `OnConfig`/`SetOnApply` with this package's resolved entry option values (and configured values of its declared global options) so unselected dependency branches are pruned before source materialization and build.

## Option & Conditional

Declare options in `OnConfig`. **`SetDefault` and `SetValues` automatically infer the option type** (`bool` → `OptionBool`, `int` → `OptionInt`, `string` → `OptionString`, `SetValues(...)` → `OptionChoice`), so `SetType(...)` is optional when a default or value list is given:

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Option("enable_dma").SetDefault(true).SetDescription("Enable DMA")           // Inferred OptionBool
    ctx.Option("rx_buf_size").SetDefault(1024).SetDescription("RX buffer bytes")     // Inferred OptionInt
    ctx.Option("log_level").SetDefault("info").SetValues("debug", "info", "error")   // Inferred OptionChoice
    ctx.Option("custom_ld").SetDefault("board.ld")                                   // Inferred OptionString
    ctx.Option("cache_size").SetDefault(16).SetAdvanced(true)                        // Advanced tuning knob (collapsed in TUI by default, press 'a'; always overridable via -s/configs)
    ctx.Option("internal_knob").SetDefault(false).SetInternal(true)                  // Container-internal only (hidden and blocked across container seams)
})
```

Advanced options (`SetAdvanced(true)`) are a display tier only: the TUI hides unmodified default-valued advanced options by default (toggle via the clickable header/title/in-list controls or press `a`; explicitly configured advanced options stay visible), while CLI `--set`, `configs/*.json`, and `OnConfigureDeps` always allow overriding them. Container-internal options (`SetInternal(true)`) may only be configured or read within the same container (e.g. via `OnConfigureDeps`); external consumers cannot set them in `configs/*.json`, CLI `--set`, `OnConfigureDeps`, or cross-package reads, and the TUI workbench hides them. `GlobalOption` cannot be marked internal.

### Multiple Project Configurations

Manage multiple tracked configurations (`configs/**/*.json`, default `configs/default.json`) sharing `.vmake/vmake.lock` and `.vmake/project.json` (`default_config` and `pins`):

```bash
vmake config copy release_o3
vmake config use release_o3        # Stores local cursor in .vmake/local.json
vmake build -c release_o3          # Or build statelessly without changing .vmake/local.json
vmake config check --all           # Validate all tracked configurations
```

### Generated Defines Always Use the CONFIG_ Prefix

- **Package options** become C macros only when `ctx.GenerateConfigDefines()` (`-D` flags) or `ctx.GenerateConfigHeader()` (`generated/autoconf.h`) is called in `OnBuild`. By default, option `"enable_dma"` emits `CONFIG_ENABLE_DMA=1` (bool `false` is omitted in `-D` and `/* #undef */` in headers).
- **`SetMacroName(name)`** overrides the emitted macro name on **both** package `Option` and `GlobalOption` (supports `%s`/`%v` value formatting such as `SetMacroName("PY32F539xx%s")`).
- **Cross-package config propagation**: `ctx.ImportConfig("//chip")` + `ctx.GenerateConfigDefines()` merges `"//chip"`'s options into the current package's generated `-D` defines; use `GenerateConfigHeader()` as well for `autoconf.h`. `ctx.SyncConfigDefines("//chip")` is the defines-only shorthand and does not set the header. No export declaration is needed in `"//chip"`.

### OptionChoice Generates Dual Macros

An `OptionChoice` named `"log_level"` with value `"info"` generates **both** `CONFIG_LOG_LEVEL="info"` (string value) and `CONFIG_LOG_LEVEL_INFO=1` (boolean selector for `#ifdef CONFIG_LOG_LEVEL_INFO`).

### Global Flags & Mode Flags

Use `SetOnApply` in `OnConfig` when an option must set workspace-wide compiler/linker flags (`AddGlobalCFlags`, `AddGlobalCxxFlags`, `AddGlobalLdFlags`, `AddGlobalLinks`, `AddGlobalDefines`):

```go
ctx.GlobalOption("board_rev").SetDefault("revB").SetValues("revA", "revB").
    SetOnApply(func(ctx *api.ConfigContext, val any) {
        ctx.AddGlobalDefines("BOARD_REV_" + strings.ToUpper(val.(string)))
    })
```

The built-in global `"mode"` option (`debug`, `release`, `size`) automatically injects `-O0 -g`, `-O2 -DNDEBUG`, or `-Os -DNDEBUG`. Flags are ordered private target → mode → global → public target/propagated public, so a global `-O` overrides the mode's level, while `AddPublicCFlags` can still override the global flag. `AddGlobal*` calls from `SetOnApply` are buffered per package and applied only to packages that survive `FilterDeps`.

### GlobalOption Cross-Package Consistency

`ctx.GlobalOption(name)` defines a workspace-wide default and exports `-DCONFIG_<UPPER_NAME>` defines by default (`SetExportDefine(false)` disables global `-D` export when an option only drives dependency selection or package-local build logic). A non-default `entries.<pkg>.options.<name>` value can override the effective value for that package, while the exported macro still uses the global value. `mode` and `toolchain` are built-in; `target_os`/`target_triple` are project-declared. All four names are skipped from global macro export unless `SetMacroName` supplies a name. Multiple declarations must agree on `Type` and `SetExportDefine`, and their `Default`s must match; `chip` alone may leave one default unset (the other fills it) and unions declared `Values`. `Values` must agree for the other non-builtin options. `MacroName` conflicts only when both declarations are non-empty and differ; a later non-empty name is adopted when the earlier one is empty.

### Cross-Package Option Reads

In `OnBuild`, `ctx.Bool/String/Int`, `ctx.If`, `ctx.Select`, and `ctx.When*` also accept a `<pkg>:<option>` reference (for example `"//:debug"`, `"//chip:baud"`, or `"official/zlib:shared"`). The package is the canonical package ID before `:`; the option is the name after `:`. The value is the owner's resolved configuration — entry override, then declared default, then global fallback — not a value derived in the owner's `SetOnApply`/`OnBuild`.

- Cross-package reads are available in `OnBuild` (including `Target.SetBuildFunc` closures that capture the `BuildContext`) and in `OnConfigureDeps`, but not in `OnConfig`, `OnRequire`, `OnInstall`, or `OnClean`.
- A package-qualified read of a global option (for example `"//firmware:chip"`) resolves the owner's effective value, including its `entries.<pkg>.options` override; it can therefore differ from the bare-name read in the reading package. `target_os`/`target_triple` resolve even when no package declares them.
- Unknown packages/options, type mismatches, and malformed references are build errors.
- A composite condition is not a single option. Read every participating option, or publish a derived value from the owner; do not assume `ctx.Bool("//owner:switch")` equals the owner's `-D` capability macro.
- Do not infer options from `p.GlobalCFlags()` or generated `-D` flags; those are compiler flags, not the option model.

### Default Build Flags

The built-in `host` toolchain applies `api.DefaultBuildFlags("host", target_os)` to targets:

- C: `-Wall -Wextra -Werror`, `-Wstrict-prototypes -Wmissing-prototypes -Wmissing-declarations -Wold-style-definition -Wundef -Werror-implicit-function-declaration`, `-Wformat=2 -Wshadow`, `-ffunction-sections -fdata-sections`, `-fstack-protector-strong`, `-D_FORTIFY_SOURCE=2` (not Windows/macOS), `-fno-strict-aliasing -fno-common -fPIC`.
- C++: the same base plus `-Wnon-virtual-dtor -Woverloaded-virtual -Wundef` instead of the C-only prototype warnings.
- LD: ELF `-pie -Wl,--as-needed -Wl,--gc-sections -Wl,-z,relro,-z,now`; Windows `-Wl,--gc-sections`; macOS `-Wl,-dead_strip`.

VMake also adds `-MMD -MP` for header dependency tracking; do not duplicate it. Use `SetDefaultFlags` or target `AddCFlags`/`AddLdFlags` to change the defaults.

## Cross-Compiling

Set `target_os` (host OS names, plus `host` and the bare-metal value `none`) and `target_triple` via toolchain manifests or project options. CMake also recognizes `freebsd`, `openbsd`, `netbsd`, and `android`:

```bash
vmake config --set toolchain=arm-gcc --set target_os=none --set target_triple=arm-none-eabi
vmake build
```

`target_os`/`target_triple` are project-declared global options, not built-in like `mode`/`toolchain`; `--set` fails unless the project declares them in `OnConfig` (a chip profile's target defaults and the toolchain defaults still fill unset values). `--toolchain <name>` on `build`/`test` overrides the selection for one invocation.

Inspect `p.TargetOS()` and `p.TargetTriple()` in `OnBuild` when platform-specific files or flags are needed.

## RTOS / Embedded

Embedded helpers on `Target` and `Package`:

```go
t.SetLinkerScript("stm32f4.ld").                // Or UseDependencyLinkerScript() + p.SetProvidedLinkerScript(...)
    AddPostLinkHex().                           // objcopy -O ihex {output} {output}.hex
    AddPostLinkBin().                           // objcopy -O binary {output} {output}.bin
    AddPostLinkSize().                          // size {output}
    AddPostLinkDeps("scripts/sign.ld").
    AddPostLinkOutputs("{output}.signed").
    AddPostLink("objcopy", "-O", "binary", "{output}", "{output}.signed")
```

- **Declarative Code Generation**: Use `p.WriteBuildFile(rel, content)`, `p.GenerateFile(rel, fn)`, or `p.Generate(fn)` inside `OnBuild` (or `t.AddBinHeader("fw.bin")`) to generate headers/sources into `BuildDir()` with dry-run safety.
- **Synchronous Subgraph**: Use `ctx.BuildSubGraph("//pkgA")` in `OnBuild` when an external image step needs specific VMake packages built and linked first, then query `ctx.DepOutput("//pkgA:target")` / `ctx.DepBuildDir("//pkgA:target")`.

### KConfig Preset Management (Firmware)

For Linux kernel, U-Boot, or BusyBox packages using Kconfig (see [examples/firmware.md](examples/firmware.md)):

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.KConfig("u-boot").AddPreset("rk3568_defconfig").SetDefaultPreset("rk3568_defconfig")
})
```

Call `pkg.EnsureConfig(pkg.SrcDir())` inside the `TargetVoid` `SetBuildFunc` before `pkg.Make(...)`. Firmware binaries also use a linker script via `t.SetLinkerScript(...)` or `t.UseDependencyLinkerScript()`, set `target_os`/`target_triple` (bare metal is `target_os=none` with a triple such as `arm-none-eabi`), and use a toolchain whose resolved make program (`make`/`gmake`) `pkg.Make()` runs.

## Install

`TargetBinary` installs to `install/bin/`; `TargetShared` installs to `install/lib/`. `TargetStatic` is installed only with `--install-type sdk` (the default `runtime` type skips it). Use `t.SetInstall(false)` to skip installing internal helper targets or `t.SetInstallDir("lib/plugins")` to override the subdirectory.

### OnInstall Lifecycle

Use `p.OnInstall(func(ctx *api.InstallContext) { ctx.AddInstalls("include", "include") })` (or `ctx.AddInstalls` in `OnBuild`) to stage headers, configs, and extra assets into `install/`. The install phase runs after builds and reuses the invocation's `BuildContext`s without replaying `OnBuild`.

`install/manifest.json` records the vmake version, toolchain, mode, generation time, and package versions/refs. Installed artifact paths/sizes/hashes are recorded in `build/vmake-build.json`, not in the manifest.

## Multi-File Build Scripts

If a package directory contains multiple `.go` files (`package main`), VMake merges them using the host's `GOOS`/`GOARCH` (`_test.go` and `import "C"` files are excluded). Helper functions and tables can live in sibling `.go` files alongside `build.go`.

## Wrapping Third-Party Libraries

Prefer the high-level CMake/Make helpers inside a `TargetVoid` or prebuilt wrapper (see [examples/third-party-wrapper.md](examples/third-party-wrapper.md)):

```go
p.CMakeConfigure("-DBUILD_SHARED_LIBS=OFF") // Fingerprinted in <CMakeBuildDir>/.vmake_configure.json
p.CMakeBuild()
p.CMakeInstall()
```

`CMakeConfigure` fingerprints its inputs in `<CMakeBuildDir>/.vmake_configure.json` and skips an unchanged configure. New files matched only by `file(GLOB)` do not invalidate that fingerprint unless `CONFIGURE_DEPENDS` is used or the build tree is cleaned.

For in-source Makefiles, pass an absolute path with forward slashes: `p.Make("-C", filepath.ToSlash(p.SrcDir()), "all")`.

## Build Scope

- `vmake build` builds all default targets in needed packages, local and remote. Roots come from the active configuration's `"root"`/`"roots"` override when set, otherwise `vmake.json` `"roots"`, otherwise `"//"` when a root `build.go` exists.
- There is **no package/target filter flag**. `-p`/`--prefix` is the installation prefix, not a package selector. To narrow a build, set `"root"`/`"roots"` in a `configs/*.json` profile, adjust `"roots"` in `vmake.json`, or use a separate project. Use `vmake -C <dir>` to select the project root.

## Clean

- `vmake clean` runs `OnClean`, removes the active configuration's build outputs and `build/compile_commands.json`, and preserves `.vmake_deps/` source trees, `install/`, and other configurations' `BuildDir`s.
- `vmake clean --all` removes all configurations' build outputs and `build/compile_commands.json`, but keeps `install/`; it also removes `pkg link` packages' `.vmake_deps` outputs. `vmake distclean` also removes `install/` (`--purge-sources` deletes `.vmake_deps/`) but keeps `configs/` profiles and `.vmake/` project, lock, and `local.json` files.
- `clean`, `distclean`, and `pkg clean` remove both `build/vmake-build.json` and `build/vmake-test.json`. With a missing toolchain, `clean --all`/`distclean` still remove outputs and skip toolchain-dependent `OnClean` with a warning; plain `clean` fails its toolchain check.

## Repository & Toolchain Commands

```bash
vmake repo add <name> <url>          # Register a registry package repository
vmake repo add --native <name> <url> # Register a native VMake repository
vmake repo update <name>             # Pull latest definitions for a registry repository
vmake repo trust <name> [--url <template>] # Trust a repository (or manifest-declared repo URL) and its future updates
vmake ext add <name> <url>           # Add an extension repository (plugins/toolchains)
vmake ext trust <name>               # Trust an extension and its future updates
vmake ext provision <name>           # Install toolchains declared for the current host
vmake pkg link <repo/name> <path>   # Build from a local worktree; removes stale .vmake_deps src
vmake pkg unlink <repo/name>        # Next build uses vmake.lock; run deps update for newer versions
```

Native repositories are trusted per repository; `repo trust` works without a resolved package or commit, and `repo update` still does not apply to them. Trust survives new commits and `deps update`. `pkg link` records the path in `.vmake/local.json` (machine-local, not lock/config) and removes the package's materialized `src`/`state.json`; an explicitly named linked package is rejected by `deps update` and `deps pin`. While a package is linked, edit its worktree and rerun `vmake build`; no tag, version bump, or `deps update` is needed. Bare `deps update` keeps linked packages' existing lock entries unchanged.

## Common Mistakes

1. **Spreading `[]string` into `...any` setters**: Pass slices directly (`t.AddCFlags(flags)`, `t.AddFiles(files)`). Spreading `flags...` fails in Go/yaegi because `[]string` is not `[]any`. `AddCFlags`, `AddDeps`, `AddProvidedLibs`, `AddExcludeLibs`, and all `Remove*` take `...any`; on Target, only `AddPostLinkDeps`, `AddPostLinkOutputs`, and `AddPostLink`'s argument list are `...string` (`SetKeepGlobalSymbols` takes one path). Package/context methods `AddPatches`, `SetPatches`, `SetGit`, `ImportConfig`, and `SyncConfigDefines` are also `...string`.
2. **Reading options directly in `OnRequire`**: `ctx.Bool`/`String`/`Int` fail during Phase 1 discovery. Always use `ctx.When`, `ctx.WhenNot`, `ctx.WhenAny`, `ctx.If`, or `ctx.Select`.
3. **Using `AddGlobal*` for library-specific flags**: `AddGlobalCFlags`/`AddGlobalDefines` change the global-flags hash and rotate every package's `BuildDir`. Use `t.AddPublicCFlags`/`t.AddPublicDefines`/`t.AddPublicLinks` on the library target instead.
4. **Declaring options outside `OnConfig`**: Calling `ctx.Option(...)` in `OnBuild`/`OnInstall` raises a `*BuildScriptError`.
5. **Procedural file writes in `OnBuild` without dry-run guards**: `vmake inspect` (without `--no-targets`) and `query` run `OnBuild` with read-only `scriptfs`. Use `p.WriteBuildFile`/`p.GenerateFile` or guard with `if !p.DryRun()`.
6. **Passing relative or backslash paths to `p.Make("-C", ...)`**: Always use `filepath.ToSlash(p.SrcDir())`.
7. **Tagging or version-bumping a `pkg link`ed package**: A linked package is built from the worktree as-is, including uncommitted edits. Edit files and rerun `vmake build`; no commit, tag, version bump, or `vmake deps update` is required to test. A commit is optional and moves the linked HEAD/build key; a tag points at HEAD and excludes uncommitted edits. Tag and update the lock only for a release after `vmake pkg unlink`.
8. **Putting `version` inside `configs/*.json` entries**: Per-config `entries.<pkg>.version` has been removed and is ignored. Use `vmake deps pin <pkg>@<ver|commit>` to record project-wide pins in `.vmake/project.json` or `vmake deps update <pkg>@<ver|commit>` to update `.vmake/vmake.lock`.

## Writing New Build Scripts — Checklist

1. Declare static dependencies with `p.AddRequires(...)` in `Main(p)` and conditional ones with `ctx.When`/`WhenNot`/`WhenAny` in `OnRequire`.
2. Declare options in `OnConfig` using `SetDefault`/`SetValues` (type is inferred automatically).
3. Wire targets in `OnBuild` using `UseRequires()` or `AddDeps(...)`, and expose library headers/defines/flags with `AddPublicIncludes`/`AddPublicDefines`/`AddPublicCFlags`/`AddPublicLinks`.
4. Verify with `vmake inspect` and `vmake build`.

## Detailed References

- **Complete API Signatures**: [references/api.md](references/api.md)
- **CLI Commands & Flags**: [references/cli.md](references/cli.md)
- **Toolchain Resolution & Compiler Paths**: [references/toolchains.md](references/toolchains.md)
- **Platform Prerequisites (Linux, macOS, Windows)**: [references/platforms.md](references/platforms.md)
- **Inspect & Query JSON Schema**: [references/inspect.md](references/inspect.md)
- **Build & Test Result JSON (`vmake-build.json`)**: [references/build-result.md](references/build-result.md)
- **Directories, Caching & Lockfiles**: [references/dirs.md](references/dirs.md)
- **Advanced Gotchas & Patching**: [references/gotchas.md](references/gotchas.md)
- **Examples**: [examples/complete.md](examples/complete.md), [examples/config-to-define.md](examples/config-to-define.md), [examples/config-propagate.md](examples/config-propagate.md), [examples/third-party-wrapper.md](examples/third-party-wrapper.md), [examples/firmware.md](examples/firmware.md)
