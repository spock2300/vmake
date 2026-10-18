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
| 1 | `OnRequire` | Declare deps (runs with nil config) |
| 2 | `OnConfig` | Build options (debug/release/size, features, etc.) — includes OnApply callbacks |
| 3 | `FilterDeps` | Re-runs `OnRequire` with this package's entry option values; recomputes deps; BFS collects needed packages |
| 4 | `OnBuild` | Define targets |
| 5 | Compile & Link | Scheduler compiles sources and links targets |
| 6* | Install | Optional install (only with `--install` flag) |
| — | `OnInstall` | Extra install entries (runs when install starts, after builds succeed; items copied together with targets) |
| clean | `OnClean` | Custom clean logic (runs during `vmake clean`/`distclean`; separate from build pipeline) |

`OnPackage` runs for all packages right after `Main()` is called (before any lifecycle phases). Use it to describe the package (`SetDescription`, `SetLicense`, `SetHomepage`). `SetGit`/`AddVersion` inside `OnPackage` declares the remote source (resolution later materializes it; for local packages it is linked at `SourceDir()/src/`, and `SrcDir()` points there) — works for both registry packages and local packages that need to wrap a downloaded library.

## Decision Guide

- **New project, no options, no deps** → Only `OnBuild`. Run `vmake doctor` to check the host toolchain, then start from `examples/simple.md`.
- **Need configurable features** → Add `OnConfig`. See `examples/config.md`.
- **Multiple saved configurations (debug/release/board variants)** → Store files in `.vmake/` and switch with `vmake config list/use/copy/describe`. See `examples/config.md` and **Multiple Project Configurations** below.
- **Conditional compilation** → Options + `ctx.If()`/`ctx.Select()`. See `examples/conditional.md`.
- **Config options → C compiler defines (auto `-DCONFIG_*` or `autoconf.h`)** → Four mechanisms, including global option auto-export. See `examples/config-to-define.md`.
- **Multiple targets (lib + binary + tests)** → See `examples/multi-target.md`.
- **Run tests** → `SetTest(true)` targets + `vmake test` on host targets, or `vmake build --tests` for cross/bare-metal. See `examples/multi-target.md`.
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
- **Cross-compile or pick a toolchain** → project `target_os`/`target_triple` global options + `--toolchain`. See **Cross-Compiling** below.
- **Install layout and install hooks** → `--install`, `--prefix`, `--install-type`, `AddInstalls`. See `examples/on-install.md`.
- **Custom clean logic** → `OnClean` hooks for `vmake clean`/`distclean`. See `examples/on-clean.md`.
- **Broad tour of options, deps, tests, install, and post-link** → See `examples/complete.md`.
- **Pin dependency versions for reproducible or CI builds** → `.vmake/vmake.lock` + `vmake build --manifest install/manifest.json`. See **Reproducible Builds (vmake.lock + --manifest)** below.
- **Orient yourself in an unfamiliar project** → `vmake inspect` prints the resolution/declaration model (packages, targets, options, deps, toolchain) as one JSON document; it does not build. See **Machine-Readable Project Model** below.

For CMake projects, let VMake's CMake API manage toolchain resolution, platform paths,
build directories, installation prefixes, build configuration, and parallelism.
Keep build.go focused on project options and project-specific steps. Invoke `cmake`
directly only for special operations the API cannot express; do not duplicate these
general rules in build.go. See `references/api.md` for settings and migration details.

## Machine-Readable Project Model

`vmake inspect` prints one JSON document (schemaVersion 1) with the project
resolution/declaration model: `root`, `configFile`, `configSha256`, `mode`,
`toolchain`, `toolchainInfo`, `global` values, `roots`, `packages` (with
options, deps, `sourcePresent`, and targets), the package `graph`, the
`compileDatabase`/`buildReport` paths, and the expected
`installPrefix`/`installManifest` paths. It never builds or installs, so prefer
it over scraping `query` text when you need to decide which package, target, or
file to change.

- Envelope: `{schemaVersion, command, status: "ok"|"error", data?, diagnostics}`.
  `diagnostics` is always an array; warnings keep `status: "ok"` and exit 0,
  model failures use `status: "error"` and exit 1.
- Targets are listed only for needed packages; a package pruned by `FilterDeps`
  has none. `files` keeps the declared globs (inspect does not expand them).
  `installs` lists the package's `AddInstalls` entries declared in `OnBuild`
  (`{source, dest}`), and `installPath` predicts the prefix-relative artifact
  path (`bin/app`, `lib/libfoo.a`) or omits it for void/object/test/no-install
  targets. `OnInstall` does not run and the install prefix is not resolved, so
  these values are declarations, not evidence that a file exists.
- A needed package whose source tree is absent gets a
  `VMAKE_W_SOURCE_MISSING` warning and `sourcePresent: false`; inspect does not
  fetch, materialize, or link sources for this check. `vmake query` and
  `query targets` print the same warning on stderr without changing stdout.
- `--set [pkg/]option=value` (repeatable) evaluates a hypothetical
  configuration and echoes typed values in `data.overrides`; it never writes
  `.vmake/config.json`. Values are coerced by the declared type: bool accepts
  `true`/`false`, `on`/`off`, and `1`/`0`; int needs an integer; string and
  choice keep the raw text (so a string may be `"42"` or `"true"`). Each
  `--set` resolves twice to learn the types, roughly doubling resolution time
  and repeating `OnConfig`/`OnApply` side effects. Unknown packages/options and
  wrong types or choices fail with `VMAKE_E_OPTION`.
- Buildscript stdout is discarded while the model is resolved, so the JSON is
  never corrupted by `build.go` prints.
- `compileDatabase`/`buildReport` point at `build/compile_commands.json` and
  `build/vmake-build.json`; the latter holds the last build/rebuild/test result
  (see `references/build-result.md`). `installPrefix`/`installManifest` point at
  the default `install/` and `install/manifest.json`; `--prefix` overrides them.
  Full schema: `references/inspect.md`.

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
- Artifact names are decided by the resolved **target OS** (the `target_os` global option, falling back to a toolchain default and then the host OS), not by the host toolchain:
  `TargetBinary` → `.exe`, `TargetShared` → `.dll` (+ import library `lib<name>.dll.a`, which is
  what consumers link against on PE targets). Use `api.TargetFilename(kind, name, targetOS)` if you
  need to compute one.
- `SetVersionScript`, `AddExcludeLibs` and `SetSymbolBinding` are ELF-only; the build fails with a
  clear error on a Windows target instead of silently dropping them. There is no PE equivalent.
- `vmake check-symbols` inspects ELF dynamic symbols with the selected toolchain's `nm -D` on either host; PE files and ELF files without dynamic symbols report that the audit is not applicable.
- Build filesystem paths with `filepath.Join`. Write `/` only in logical identifiers
  (`repo/pkg`, `pkg:target`) and glob patterns (`src/**/*.c`) — the glob layer normalizes those, and
  object identities use target and normalized source-path hashes.
- Let `p.Make` and CMake helpers inherit the VMake jobs budget; do not add manual job counts. Commands execute without shell expansion.

## Platform Notes (macOS)

VMake runs natively on macOS with the Xcode Command Line Tools
(`xcode-select --install`), which provide Apple clang (`cc`/`c++`), `ar`, `ld`,
`strip`, `ranlib`, `nm` and `size`. GNU binutils are not required for ordinary
builds: the host toolchain leaves `objcopy`/`objdump` unset. The macOS target OS
name is `darwin`; `target_os` values are not aliased.

- Artifact names follow the resolved target OS: `TargetShared` → `.dylib`.
  Dependency archives are force-loaded (`-force_load`) instead of GNU
  `--start-group`/`--whole-archive`, and shared libraries link with
  `-dynamiclib` and an `@rpath/<name>` install name. Binary consumers of
  `.dylib` dependencies receive `-rpath` entries for the dependency
  directories plus `@loader_path`/`@loader_path/../lib`; shared-library
  consumers receive the dependency directories plus `@loader_path` only, so
  in-tree runs and the default `install/bin` + `install/lib` layout resolve.
- `SetVersionScript`, `AddExcludeLibs` and `SetSymbolBinding` are ELF-only and
  fail with a clear error on a macOS target. `vmake check-symbols` reports
  native Mach-O artifacts as not applicable; it audits ELF artifacts only.
- Post-link helpers that require `objcopy` (`AddPostLinkHex`, `AddPostLinkBin`,
  `SetSymbolPrefix`) need a selected toolchain that declares `objcopy`, such as
  the ARM GNU Toolchain (the vmake-tools extension ships its macOS arm64
  archive). The builtin macOS host declares none: Apple ships no objcopy and GNU
  objcopy cannot rewrite Mach-O, so `SetSymbolPrefix` is effectively ELF-only.
  `AddPostLinkSize` and `AddPostLinkStrip` use the system `size`/`strip`.
- Apple ships GNU Make 3.81. Kconfig/kernel/U-Boot projects need GNU Make 3.82
  or newer: `brew install make`, then point the selected toolchain's `make` at
  `gmake`. `vmake doctor` reports the resolved make path and warns when the
  detected Make is older than 3.82.

## Common Mistakes

### `p.Make()` runs in BuildDir, not SourceDir

`p.Make()` starts in `BuildDir`. For a source-tree Makefile, pass a second `-C` with the absolute source directory returned by `SrcDir()`. Import `path/filepath` and convert make path arguments with `filepath.ToSlash`. KBuild projects keep ownership of their compiler flags using the guarded reset below:

```go
SetBuildFunc(func(p *api.Package) error {
    srcDir := p.SrcDir()
    p.EnsureConfig(srcDir)
    const ownFlags = "ifndef VMAKE_FIRMWARE_FLAGS_RESET\nundefine CFLAGS\nundefine CXXFLAGS\nundefine LDFLAGS\nexport VMAKE_FIRMWARE_FLAGS_RESET := 1\nendif"
    if err := p.Make("-C", filepath.ToSlash(srcDir), "--eval", ownFlags); err != nil {
        return err
    }
    return nil
})
```

The `ownFlags` reset clears inherited C/CXX/linker flags only once and leaves recursive Make flags intact. Ordinary Makefiles that accept `pkg.Env()` compiler flags can omit it. Use `pkg.Make()` without `-C` when the Makefile is in `BuildDir`. CMake projects use `pkg.CMakeBuild()` and `pkg.CMakeInstall()` with their own build directory and generator.

### Use helper-managed jobs

`p.Make` and `p.CMakeBuild` inherit the normalized `vmake -j` budget. Do not append manual job counts or shell substitutions. `exec.Command` does not expand `$(nproc)`, `$(pwd)`, or pipes; construct ordinary paths and arguments with Go APIs.

### `ctx.If()` returns `[]string` — pass it directly to `...any` helpers, do NOT spread there

Methods whose parameter is variadic `...any` (for example `AddCFlags`, `AddDefines`, `AddFiles`, `AddLinks`) expand `[]string` items through `flattenAny`. Spreading the slice with `...` does not compile for those — Go will not convert `[]string` to `[]any` (yaegi reports the same call-site error through `reflect.CallSlice`). Methods whose parameter is `...string` (`AddDeps`, `AddProvidedLibs`, `AddExcludeLibs`, `AddPostLinkDeps`/`AddPostLinkOutputs`) accept `xs...` normally:

```go
AddCFlags(ctx.If("debug", "-g", "-O0"))    // correct — the []string flattens automatically
```

### `filepath.Join` with absolute paths

`filepath.Join("/a/b", "/a/b/c")` returns `/a/b/a/b/c`, NOT `/a/b/c`. The second absolute path wins and the first becomes a segment. Use string concatenation or trim leading `/` for logical path components.

### `SetGit`/`AddVersion` — works for local packages too

`SetGit`/`AddVersion` in `OnPackage` is the primary mechanism for registry packages, but it also works for local packages that need to download and compile a remote library (e.g., FreeRTOS, mbedtls). When a local package uses `SetGit`, source is downloaded to `SourceDir()/src/` and `SrcDir()` returns that path (registry packages differ: their `SourceDir()` already points at the downloaded checkout, so `SrcDir()` falls back to `SourceDir()`). This is the easiest way to wrap a third-party C library that doesn't need a full registry setup.

### Path resolution for packages using `SetGit`

When a **local** package uses `SetGit`, `SourceDir()` and `SrcDir()` differ — all `AddFiles` / `AddIncludes` / `AddPublicIncludes` paths resolve from `SourceDir()`, so you must prefix with `"src/"`. Registry/native packages resolve paths directly from the downloaded checkout: their `SourceDir()` already points at it, and a `"src/"` prefix would double-nest. See `references/dirs.md` for full rules, edge cases, and the correct code pattern.

### Static library deps with symbols not referenced by your code

On ELF and PE **binary** links, dependency archives from `AddDeps` land inside `--start-group`/`--end-group` and `.a`/`.so`/`.dylib` inputs are additionally wrapped in `-Wl,--whole-archive` (PE `.dll.a` import libraries are excluded), so every object of such an archive is pulled in; shared-library links pull only referenced members. On Mach-O targets there is no linker group or whole-archive: dependency archives are force-loaded with `-force_load`, which has the same all-members effect. A library supplied as a plain `-l` (e.g. via `AddLinks`/`AddGlobalLinks`) is inside the group but not whole-archived: if nothing in the group references it, it is skipped even when a post-group library (libc from `-specs`) needs its symbols. Fix with `-nostdlib` + `AddGlobalLinks(...)` in `SetOnApply`, per-target `AddLinks(...)`, or `EXTERN` in the linker script. See `references/gotchas.md` for all three fix patterns with code.

### `pkg.Run` / `pkg.RunIn` / `CMake*` raise script errors — no error return

`p.Run()`, `p.RunIn()`, `p.CMakeConfigure()`, `p.CMakeBuild()`, `p.CMakeInstall()` return **nothing** — they raise a script error on failure, caught by the execution boundary so resources can be released (the `CleanContext` offers only `Run`/`RunIn`/`RunEnv`/`Make`, with no CMake wrappers). Only `p.RunEnv()`, `p.Make()`, and `p.Configure()` return a real `error` you should check. Never write `return pkg.Run(...)` — call it as a statement, then `return nil`.

### `vmake clean` vs `vmake distclean`

`vmake clean` runs `OnClean` hooks then removes build outputs (objects, binaries) and `build/compile_commands.json` for the current configuration; keeps the source trees under `.vmake_deps/`. If build-key directories for other configurations remain, it prints one note with their count. `--all` removes every build-key output directory.

`vmake distclean` also runs `OnClean` hooks, then removes all emitted outputs, `install/`, `compile_commands.json`, and the build report. Downloaded working trees under `.vmake_deps/` are kept and reused, so the next build does not re-download; pass `--purge-sources` to delete them too. Buildscript edits normally invalidate the affected actions; a full distclean is only needed to reclaim stale outputs/orphan keys or when a buildscript writes an untracked file into `BuildDir()` that the build still consumes.

### Patching source before build in registry packages

To patch downloaded source inside `SetBuildFunc`, use Go's `os.ReadFile` + `os.WriteFile` (simple single-line changes) or `AddPatches("patches/fix.patch")` in `OnPackage` (multi-file git patches). Patches apply to the package's single working tree exposed as `SrcDir()` (`.vmake_deps/.../src` for remote packages, the `<pkgDir>/src` link for local SetGit packages). A changed patch set restores the tree to the pinned commit before patches are applied. See `references/gotchas.md` for code examples of both patterns.

### Strict config accessors — wrong reads are build errors, not zero values

Script-facing contexts (`OnConfig`/`OnBuild`/`OnInstall`/`OnClean`/`OnRequire`) fail loudly on: reading an **unknown option** (typo), using an **accessor that mismatches `SetType`** (e.g. `ctx.String` on an `OptionBool`), and **direct value reads (`ctx.Bool/String/Int`) during OnRequire discovery**. In `OnRequire`, use the discovery-aware helpers: `ctx.When("opt", true)`, `ctx.If(...)`, `ctx.Select(...)`.

Two related rules:
- **Pass 2's requires replace the edge list.** Dropping a dependency removes the edge, so it is excluded from the needed/build set unless another root reaches it; adding one is accepted only when that package is already in the resolved graph (for example because another package requires it). A dependency first added in pass 2 that is absent from the graph fails with `dependency ... not found in dependency graph` — hoist the unconditional `AddRequires` out of the guard.
- `When` compares numerics across `int`/`float64` (JSON round-trips decode numbers as `float64`); `Select` returns `""` and `When` returns `true` while config is still nil during discovery.

### Generated macros are CONFIG_-prefixed, not the raw option name

`GenerateConfigDefines` / `GenerateConfigHeader` always emit `CONFIG_<OPTION_NAME>` (uppercased, `-` → `_`): an option `debug` becomes `-DCONFIG_DEBUG=1`, so C code checks `#if CONFIG_DEBUG`, not `#if DEBUG`. A disabled bool emits no `-D` at all. When C code needs another name, read the option and call `AddDefines` manually. Global options are the exception: `ctx.GlobalOption(...)` declarations are auto-exported to every package as `CONFIG_<NAME>` (see **Global Option Auto-Export** below). See **Generated Defines Always Use the CONFIG_ Prefix** below.

### Script-relative file IO

Inside build.go, relative paths passed to wrapped stdlib (`os.ReadFile/WriteFile/Stat/...`, `exec.Command` without `Dir`) resolve against the package's **`SourceDir()`** once it is bound (before binding, the build.go's own directory) — not the process cwd. `os.Getwd()` returns that base and `os.Chdir` is an error. `OnBuild`/`OnInstall`/`OnClean` callbacks run with the process cwd set to the bound `SourceDir()`, so unwrapped APIs (`text/template.ParseFiles`, `exec.CommandContext`, `io/ioutil`) resolve relative paths there too. `OnConfig` runs before source binding; its cwd is the loaded build.go directory (the registry wrapper checkout for registry packages). Outside callbacks (for example a `TargetVoid` `SetBuildFunc` invoked later by the scheduler) unwrapped APIs use the process cwd. Prefer absolute paths built from `p.SourceDir()`/`p.BuildDir()`.

## Directory Reference

| Property | What it returns | When to use |
|----------|-----------------|-------------|
| `SourceDir()` | Package source root: local package directory, or downloaded working tree (`src[/<member>]`) for remote packages; a registry wrapper's build.go stays in `ScriptDir()` | Package metadata files, overlay dirs |
| `SrcDir()` | Source code dir (`SourceDir()/src/` for local `SetGit` packages, falls back to `SourceDir()`) | Source files for firmware/third-party builds |
| `BuildDir()` | Scratch dir for intermediate artifacts | Build outputs and action success records |
| `InstallDir()` | Remote package installation prefix; empty for local packages | Remote package publication |
| `CMakeBuildDir()` | `BuildDir()/cmake`, unless explicitly set | CMake cache and build artifacts |
| `CMakeInstallDir()` | Remote `InstallDir()` or local `BuildDir()/staging`, unless explicitly set | Headers/libs installed by CMake |

For `BuildKey` naming, `SourceDir` vs `SrcDir` distinction, and `SetGit` path resolution rules, see `references/dirs.md`.

## Storage Layout

Remote and local `SetGit` sources materialize as **one shallow working tree per package** inside the project, checked out depth-1 from a shared content-addressed object cache. Builds write configuration-specific outputs into per-build-key directories, never copies of the source. This is storage isolation, not a script sandbox.

```
~/.vmake/
├── trust.json                         # commit-bound trust records
├── _locks/                            # repo_<hash>.lock + ext_<hash>.lock (repo/extension updates)
└── cache/
    ├── git/<urlhash>.git/             # shared object cache: one bare repo per upstream URL
    │   └── objects/ + refs/vmake/<sha> + vmake-cache.json
    └── _locks/                        # lifecycle + per-owner + per-tree + per-cache-repo locks

project/.vmake_deps/
├── <repo>/<owner>/
│   ├── src/                           # single working tree (depth=1, selected version; native subpackages use src/<member>)
│   ├── state.json                     # urls/version/ref/commit/patchHash/submodules
│   └── out/<sha256(member)>/<buildKey>/{build,install}
└── local/<pkg>/src                    # local SetGit tree; <pkgDir>/src links to it
```

- The shared cache accumulates only the objects vmake actually fetched, anchored under `refs/vmake/<commit>`; `VMAKE_GIT_CACHE=off` disables it. A lock-pinned working tree at a cached commit is fetched depth-1 from the cache without touching upstream; ref resolution (`git ls-remote`), HEAD materialization, cache misses, and the full-clone fallback for servers that reject SHA fetches still contact upstream.
- Working trees are normally depth-1 fetches from the cache: tags land in `refs/tags/`, branches in `refs/remotes/origin/`, and only the selected version's objects are downloaded. HEAD/default-branch materialization clones upstream directly, and cache misses or SHA-refusing servers fall back to upstream/full clone. `git log` is truncated by the shallow boundary (use `git fetch --unshallow` when full history is needed).
- A commit change recreates the tree; a patch-set change on the same commit restores it in place (`git checkout --force --detach <commit>` + `clean -fdx`) without network access. Switching toolchain/mode/options only switches `out/<buildKey>` (remote) or the package's `build/<buildKey>` (local). Patches and Kconfig `.config` apply to the tree; ordinary external build output is not transactional, so a failed callback may leave partial files and the next session calls it again.
- `.vmake/vmake.lock` pins remote versions/commits, versioned local `SetGit` versions/commits, the source kind, and registry wrapper commits. When the locked version's tree already exists, or its commit is in the object cache, builds run fully offline; resolution otherwise needs one small `git ls-remote`. `vmake lock update` re-resolves.
- Commands that resolve dependencies take the exclusive `project/.vmake/_locks/project.lock`, so commands against one project serialize (resolution may materialize trees or links). Each tree materialization is serialized by `cache/_locks/tree_<hash>.lock`, each cache repository by `cache/_locks/cache_<urlhash[:16]>.lock`, and package/extension repository checks against updates by `~/.vmake/_locks/repo_<hash>.lock`/`ext_<hash>.lock`; builds and `lock update` additionally hold `_locks/owner_<sha256(name)>.lock` for every needed remote package (native sub-packages fold into their root parent); the cache lifecycle lock is shared for builds/reads and exclusive for cleanup.

Global storage in `~/.vmake/`:
- `~/.vmake/cache/git/<urlhash>.git/` — shared content-addressed object cache, one bare repo per upstream URL (initialized in a `.tmp-*` staging directory and atomically renamed; accumulates fetched objects, anchored under `refs/vmake/<commit>`; `VMAKE_GIT_CACHE=off` disables)
- `~/.vmake/cache/_locks/` — lifecycle, per-owner, per-tree and per-cache-repo locks (legacy `cache/mirrors` is removed by the layout upgrade)
- `~/.vmake/_locks/` — package and extension repository locks; trust decisions never take an exclusive lock while a shared one is held (`flock` is not reentrant)
- `~/.vmake/repos/` — registry repo clones (buildscript metadata only)
- `~/.vmake/toolchains/` — installed toolchains (`<host-os>/<host-arch>/<name>/<version>/`)
- `~/.vmake/extensions/` — extension repos
- `~/.vmake/trust.json` — commit-bound trust records for package repos and extensions

Environment overrides: `VMAKE_CACHE` (cache root), `VMAKE_GIT_CACHE=off` (disable the shared git object cache), `VMAKE_GIT_TIMEOUT` (seconds for network git clone/fetch/submodule/ls-remote, default 1800), `VMAKE_ENV_EXTRA` (comma-separated extra environment variables included in incremental fingerprints), `VMAKE_TRUST_ALL=1` (bypass trust gating, CI). Clone/fetch stream git's transfer progress to the terminal in real time, while `git ls-remote` has no progress output. Commands that resolve dependencies or materialize package sources (`build`, `rebuild`, `lock update`, ...) print a one-line status (package name/tree key, target ref/commit when known, source URL) before resolving a ref, listing native versions, or trying each source URL, so you can tell which step is running; the line can also appear on a cache hit without touching upstream. `-q` suppresses these lines together with git progress, and `query`/`check-symbols` are always quiet. They are not a heartbeat: `git ls-remote`, connection setup and server-side object enumeration can stay silent until the operation completes or hits `VMAKE_GIT_TIMEOUT`.

vmake locates the project root by walking upward from cwd: a start directory containing `.vmake/` is treated as the project root, an ancestor `.vmake/` without `build.go` is only a container and is ignored, and otherwise the nearest `build.go` is the root (including a start directory whose child directories have `build.go`). Running outside a project is a hard error.

## Remote Script Trust

`build.go` files from remote repositories and extension plugin code execute with full system access. Trust is bound to the exact resolved commit: records are keyed `kind:name@commit`, so separate commits of one repository are trusted independently. On first use of an untrusted package repo, vmake prompts (TTY) or refuses (non-TTY); native packages are trusted per resolved package commit. Extensions load only when their repo is trusted, clean, and at the recorded HEAD; `vmake ext update`/`repo update` clear trust. Manage with `vmake repo trust|untrust <name>` and `vmake ext trust|untrust <name>`, auto-approve with `--yes/-y`, or bypass with `VMAKE_TRUST_ALL=1` (CI).

## Package Types

| Type | How identified | `OnPackage` metadata | Source code location |
|------|---------------|---------------------|---------------------|
| **Local** | build.go in project directory | `SetDescription`, `SetLicense`, or `SetGit`/`AddVersion` for remote source | `SourceDir()` (same as build.go), or `SrcDir()` = `SourceDir()/src/` if `SetGit` used |
| **Registry** | `vmake repo add name url` | `SetGit`, `AddVersion` required | `SourceDir()` is the member directory inside the package’s working tree in `.vmake_deps/`; `SrcDir()` falls back to `SourceDir()` |
| **Native** | `vmake repo add --native name url` | No `SetGit`/`AddVersion` — version from git tag | `SourceDir()` == `SrcDir()` (the working tree in `.vmake_deps/`; subpackages use their own member directory) |

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
    SetBuildFunc(func(p *api.Package) error { return nil }).
    SetPrebuilt("/path/to/libfoo.a")
```

`AddPublicIncludes` implies `AddIncludes` — directories set via `AddPublicIncludes` are automatically available to the target itself and propagated to all dependents. There is no need to duplicate them with `AddIncludes`. The whole directory always becomes an `-I` entry; use `@"pattern"` as the last argument to filter which files are **installed** from it: `AddPublicIncludes(".", "@*.h")` installs only matching headers, while the directory stays on the include path.

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

Third-party packages with external build systems use `TargetVoid` with `SetBuildFunc`. The callback function `func(p *api.Package) error` returns a real error. Returned errors and script errors raised by void-return helpers both reach the scheduler after cleanup. Use `return fmt.Errorf(...)` for controlled failure, `return nil` for success.

## Prebuilt Libraries

Use `SetPrebuilt(path)` on `TargetStatic`, `TargetShared`, or `TargetBinary` to export a pre-compiled artifact. The expected output path in `BuildDir()` becomes a symlink to the source file; when the target has post-link steps (e.g. `SetSymbolPrefix`), the artifact is copied instead so post-link can rewrite it. Incrementality uses a link-state success record (signature plus input/output hashes), so a changed source file or a changed path in build.go triggers a rebuild. Multiple libraries: one target per `.a`/`.so`.

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

Always define test targets unconditionally in `OnBuild` — `SetTest(true)` controls scheduler visibility, not option guards. `SetTest(true)` does NOT clear `IsDefault`, but `vmake test` runs only targets that are both `IsTest()` and `IsDefault()`, and only `TargetBinary` targets — a test target with `SetDefault(false)` is not run. If no runnable test target exists, `vmake test` exits non-zero with `no test targets found` and publishes a failed report; use `vmake build --tests` to build tests without running them. `vmake build` excludes tests unless `--tests` is passed; cross/bare-metal targets can only be built (`vmake test` refuses to run them). Test targets are never installed and can depend on other test targets. Test runs publish `build/vmake-build.json` with `command:"test"`, per-test `tests[]` entries and test counts in `summary`. `vmake build|rebuild|test --json` prints one result envelope on stdout instead of human summaries, with progress and diagnostics moving to stderr (see `references/build-result.md`). See `examples/multi-target.md`.

## Dependencies

### Declaring and Using Dependencies

Package repositories must be registered and trusted before resolution: `vmake repo add official <git-url>` (first use prompts for trust; non-TTY runs need `-y` or `VMAKE_TRUST_ALL=1`). The `official` repo is not built in.

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

Invalid refs (whitespace, multiple `:`, empty segments, and path-shape errors in refs without `:`) are fatal at declaration time; an exactly empty entry is silently ignored. A malformed path inside a `pkg:target` ref fails during graph construction. Unknown targets/packages and dependency cycles fail at build-graph time. In `pkg:target`, a `pkg` part without `/` is first resolved as a sub-package name relative to the declaring package (`ResolveSubPackageName`); on failure the error lists the tried candidates.

### Sub-Packages

A nested `build.go` inside a **native** remote package's checkout becomes a sub-package: an independently loaded package named `parent/sub` with its own options/targets/build dirs, versioned by the parent (not locked separately). Key rules:

- Registry (wrapper) packages have **no** sub-packages — by design (DD-1; repository design docs are not shipped with this skill)
- Sub-packages are lazy: their build.go is interpreted only when depended on (DD-3)
- Reference from outside by full name: `ctx.AddRequires("subtest/mother/sub_a")`, `AddDeps("subtest/mother/sub_a:*")` — the parent must be listed before its sub-packages in the same `AddRequires`
- Inside a sub-package, siblings can be referenced by short name: `AddRequires("sub_b")`, `AddDeps("sub_b:utils_b")`; `DepOutput`/`DepBuildDir`, `BuildSubGraph` and `ImportConfig` accept the same short names
- A needed sub-package requires its native parent to be needed too; otherwise configuration fails with a hint to list the parent in `AddRequires`
- A parent cannot reference its own sub-packages in `OnRequire` (discovery runs after dep resolution); local projects have no sub-package concept (nested build.go are top-level packages)
- Example: `test_data/25_subpackage`

### Version Constraints

AddRequires accepts semver constraints: `"official/zlib >=1.2"`, `"official/curl ~8.5"`, `"test_build/mathlib"` (no constraint = any version).

Operators: `>=` and `>` (major-locked when major > 0 — `>=1.2` never matches `2.0`), `<=` / `<` (no major lock), `=` (exact), `~` (major.minor lock). Highest satisfying version is selected; multi-package constraints must be mutually satisfiable. See `references/api.md` for the full operator table and major lock semantics.

Version pins in entries of the active configuration (`.vmake/config.json` by default, or the file selected in `.vmake/project.json`) take precedence over latest matching tags; set a pin by editing the `version` field of that package's entry in the file. `.vmake/vmake.lock` pins survive until `vmake lock update`.

### OnRequire Two-Phase Execution

`OnRequire` callbacks execute **twice** — once for discovery, once with this package's entry option values:

| Pass | Phase | Config values | Purpose |
|------|-------|--------------|---------|
| 1 | Phase 1 | `nil` | Discover initial dependency graph. Top-level packages (registry and native) are resolved eagerly; native sub-packages load lazily. `OnRequire` runs for the first time here with nil config. |
| 2 | Phase 3 (`FilterDeps`) | This package's `entries.<pkg>.options` values; global config values are not merged | After `OnConfig` has resolved all option values, `FilterDeps` re-runs every package's `OnRequire` with the package's entry values. Unset reads fall back to declared defaults. The returned dependencies **replace** `node.Deps`, then topology is re-sorted and needed packages are collected via BFS. |

This is what enables **option-conditional dependencies**. During discovery, direct reads (`ctx.Bool/String/Int`) are build errors — use the discovery-aware helpers. On pass 1 (nil config) `ctx.When(...)` returns `true`, `ctx.If(...)` returns its `then` arguments (nil config counts as true), and `ctx.Select(...)` returns `""`; on pass 2 they see this package's entry option values (global config values are not merged):

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

**Graph-membership rule:** pass 2's returned list replaces the package's dependency edges. Dropping an edge is fine; adding one is accepted only when the package is already in the resolved graph (for example because another package requires it). A dependency that first appears in the FilterDeps pass and is absent from the graph otherwise fails with `dependency ... not found in dependency graph`; hoist the unconditional `AddRequires` out of the guard.

**Mechanism:** `FilterDeps` re-runs `OnRequire` for every package with that package's entry option values, replacing `node.Deps`. Then topology is re-sorted and needed packages collected via BFS from local roots.

**Key implication:** `AddRequires` alone does not guarantee a package is built — it must be reachable from a local root via BFS.

## Option & Conditional

### Multiple Project Configurations

Store multiple JSON configuration files directly in `.vmake/`. The optional
`.vmake/project.json` selects one with `{"config":"config-debug.json"}`.
Without it, the project uses `.vmake/config.json` without creating a selection
file. An explicit missing or invalid selection is an error, never a fallback.
Filenames must end in `.json`; paths and `project.json` are rejected. Each file
may carry a top-level `description` (≤200 characters; line breaks are allowed)
explaining what the configuration is for.

```bash
vmake config list
vmake config copy config-debug.json
vmake config use config-debug.json
vmake config describe "Board A debug build"
vmake config describe
vmake config
vmake build
```

`list` marks the current file with `*`, reports unsaved/invalid entries and
appends each readable file's description. `describe` prints the active
description, or sets it (`describe ""` clears); it does not execute build
scripts. `copy` preserves the current file bytes, does not switch, and refuses
to overwrite; an unsaved legacy default copies as an empty configuration. `use`
validates an existing file and updates the selection, including when the old
target is missing; its completion candidates carry the description. These
management commands do not execute build.go or resolve dependencies. Repair
malformed project.json before using it.

The TUI shows the description as a fixed `Description` row at the top of the
options panel: press `D` or click the row to edit it, and `Ctrl+S` saves it
with the rest of the configuration. Descriptions are metadata only — they do
not affect dependency resolution or the BuildKey. The TUI also supports mouse
interaction; its header language selector switches between English and 中文
(initial language follows the system locale).

TUI and `--set` edit only the selected file. Build, test, query, clean, doctor and
lock update use that same selection relative to the detected project root; script
scanning scope is unchanged. All configurations share `.vmake/vmake.lock`.
Switching does not rewrite the lock. Configuration filenames do not enter the
BuildKey, so equivalent configurations reuse artifacts. Installation still defaults
to `install/`; use distinct `--prefix` values to retain multiple installations.
`clean --all` and `distclean` preserve configurations and project.json.

### Declaring Options

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

- `SetOnApply(fn)` — callback invoked once per build after all option values are resolved (in sorted option-name order), receives `*ConfigContext` and `val any`. Only `OptionInt` is normalized (`api.NormalizeOptionValue` converts JSON `float64`/`int64` to `int`); other types are passed through unchanged. The callback's context carries real option values, so reading other options inside it works. Used to react to options (set global flags, choose linker script based on chip).

### Generated Defines Always Use the CONFIG_ Prefix

`GenerateConfigDefines()` and `GenerateConfigHeader()`, called in `OnBuild`, derive every macro name from the option name: the emitted name is always `CONFIG_<OPTION_NAME>` — the option name uppercased, with `-` replaced by `_` (option `debug` → `CONFIG_DEBUG`, option `tick-hz` → `CONFIG_TICK_HZ`). The bare option name is never emitted, so C code must check `#if CONFIG_DEBUG` / `#ifdef CONFIG_DEBUG`. Call position within `OnBuild` does not matter: the macros are applied to every target of this package after the callbacks have run.

- Bool true → `-DCONFIG_X=1`; bool false → no `-D` flag at all (in `autoconf.h`: `/* #undef CONFIG_X */`)
- Int → `-DCONFIG_X=42`; String/Choice → `-DCONFIG_X="value"` (the quotes are part of the argv element)
- An option with no configured value and no `SetDefault` is skipped entirely
- Global options (`mode`, `toolchain`, and `GlobalOption(...)` declarations) are excluded from this package-local list; custom global options are exported to every package instead, see **Global Option Auto-Export** below
- Options merged in via `ImportConfig()`/`SyncConfigDefines()` use the same prefix

If C code expects a different name, read the option in `OnBuild` and emit the define manually, e.g. `target.AddDefines("DEBUG=1")` guarded by `ctx.Bool("debug")`. See `examples/config-to-define.md` for the full value table and the three config→define mechanisms.

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

`AddGlobalCFlags/CxxFlags/LdFlags/Links` are available on `ConfigContext`; declare them in `OnConfig` or `SetOnApply`. They return nothing (not chainable), apply to ALL targets in ALL packages, and flags are appended as-is — duplicates are preserved, not deduplicated. Global flags set by a package that `FilterDeps` prunes from the build are dropped — flags from unneeded packages never leak. Global flag changes also change the BuildKey, so artifacts rebuild correctly when flags change.

The compile merge order is: per-target flags → mode flags → global flags. For GCC, the last occurrence of a repeated option (`-O`) wins — mode flags override per-target `-O`, and global flags override mode.

Mode auto-injected flags (injected by scheduler, not via `AddGlobalCFlags`):

| Mode | Flags injected |
|------|---------------|
| `release` | `-O2 -DNDEBUG` |
| `debug` | `-O0 -g` |
| `size` | `-Os -DNDEBUG` |

During linking, global LD flags are appended after per-target flags; global links go inside `--start-group`/`--end-group` (on Mach-O targets dependency archives are force-loaded instead).

### GlobalOption Cross-Package Consistency

If two packages define the same global option via `GlobalOption()`, their `Type` and `Default` must be **identical**, and their `SetValues` must also match (the built-in `mode`/`toolchain` options skip the `SetValues` comparison) — otherwise the build fails with a fatal error. `SetMacroName` declarations are merged instead: a non-empty macro name must be identical wherever it appears, and declarations without one express no preference. This constraint ensures all packages agree on the option's meaning. For example, if `chip/build.go` defines `GlobalOption("mcu").SetType(api.OptionString).SetDefault("stm32f405")` and `bsp/build.go` defines `GlobalOption("mcu").SetType(api.OptionChoice)`, the build will fail with a type mismatch error.

Declarations are merged deterministically (package-name order); the first definition wins for presentation fields such as `SetDescription`/`SetGroup`. Prefer a single declaring package. Each declaring package's own `SetOnApply` callback still runs during that package's config pass.

### Global Option Auto-Export

Every global option declared with `ctx.GlobalOption(...)` is automatically exported to **all packages** as a `-DCONFIG_<NAME>` macro. The value is the resolved configuration value (saved config → default), so changing it in `vmake config` or with `--set` changes the macros and rebuilds every package — no `SetOnApply` + `AddGlobalCFlags` boilerplate, and no hardcoded values in build.go.

Formatting matches `GenerateConfigDefines`: bool true → `-DCONFIG_X=1` (false emits nothing), int → `-DCONFIG_X=42`, string → `-DCONFIG_X="value"`, choice → `-DCONFIG_X="value"` plus `-DCONFIG_X_VALUE=1`.

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.GlobalOption("cpu_clock_hz").SetType(api.OptionInt).SetDefault(416000000)
    ctx.GlobalOption("mcu").SetType(api.OptionChoice).
        SetDefault("py32f539").SetValues("py32f539")
})
```

This emits `-DCONFIG_CPU_CLOCK_HZ=416000000 -DCONFIG_MCU="py32f539" -DCONFIG_MCU_PY32F539=1` for every package. Use it for chip/platform contracts and feature switches that all packages must see.

- `SetMacroName("NAME")` replaces the default name entirely; a `%s`/`%v` in the name renders the value into the macro (`SetMacroName("PY32F539xx%s")` with value `L` → `-DPY32F539xxL=1`) and emits only that macro. `SetMacroName` is valid on global options only.
- `mode`, `toolchain`, `target_os`, and `target_triple` are not exported by default; redeclare the option with a `SetMacroName` to opt it in (`mode`/`toolchain` redeclarations must match the built-in `Type`/`Default`; a conflicting macro name between packages is a fatal error).
- Two global options producing the same macro with different values abort the configuration phase; identical definitions are deduplicated.
- A package-level value for a global option left in `entries.<pkg>.options.<name>` still wins inside that package, but the exported macro always uses the global value; delete stale entry values when migrating an option from package scope to global scope.
- Exported macros participate in the BuildKey through global flags, so config changes rebuild all packages.

### Default Build Flags

The builtin `host` toolchain contributes default C/C++/linker flags (hardening, warnings, `-ffunction-sections`), selected by the project's `target_os`. They are injected as the **base** of every new target — when you call `ctx.Target("app")`, the target's initial CFlags/CxxFlags/LdFlags are set from those defaults. Calling `AddCFlags(...)` appends to this base.

Toolchains declared by extensions contribute no default CPU flags: `toolchain.json` describes which programs to run, not which CPU to target. They may declare `target_os`/`target_triple` defaults, which apply only while the project leaves those options unset. A cross-compiling project supplies its own `-mcpu`/`-mthumb`/`--specs=` through `ctx.AddGlobalCFlags` / `AddGlobalLdFlags` in `OnConfig`, or per target with `AddCFlags`/`AddLdFlags`.

## Cross-Compiling

Declare the target platform as project global options in `OnConfig`, then select the toolchain with `--toolchain`. Values are resolved as: package entry value → package-local non-global option default → project global config/default → toolchain default → empty, so a toolchain that declares `target_os`/`target_triple` lets the declaration stay value-free:

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString)   // toolchain default, e.g. "none"
    ctx.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString) // toolchain default
    ctx.AddGlobalCFlags("-mcpu=cortex-m4", "-mthumb", "-ffunction-sections", "-fdata-sections", "--specs=nosys.specs")
    ctx.AddGlobalLdFlags("-mcpu=cortex-m4", "-mthumb", "--specs=nosys.specs", "-Wl,--gc-sections")
})
```

- `target_os="none"` means bare metal; empty values count as unset and fall back to the project default and then the toolchain default. Use an explicit `"host"` for the host system, and select `toolchain=host` (or a toolchain without target defaults) to clear a toolchain-provided triple. `TargetOS()`/`TargetTriple()` read the resolved values, and artifact names follow the target OS via `TargetFilename(kind, name, target_os)` (or `ExtFor`/`PrefixFor`).
- Register toolchain repositories with `vmake ext add <name> <git-url>`; their `toolchain.json` files are scanned and installed for the host automatically (the extension authoring guide is in the vmake repository and is not shipped with this skill). `vmake toolchain list` shows what is available.
- List toolchains with `vmake toolchain list` / `vmake toolchain show <name>`; select per invocation with `--toolchain <name>`, or per package via `ctx.ToolchainOption()`. Toolchains contributed by extensions may declare `target_os`/`target_triple` defaults, but never CPU/ABI flags — supply `-mcpu`/`-mthumb`/`--specs` yourself. The `vmake config` TUI shows toolchain-provided defaults for declared but unset options.
- Validate a cross setup with `vmake doctor --toolchain <name>`.
- Cross/bare-metal tests cannot execute: `vmake test` refuses non-host targets; use `vmake build --tests`.
- For bare-metal linking (libc, crt0, `-nostdlib`, `EXTERN`), see `examples/embedded-rtos.md` and `references/gotchas.md`.

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

Key embedded rules: (1) Target-specific flags must appear in both CFLAGS and LDFLAGS to avoid ABI mismatches. (2) Use `-nostdlib` + `AddGlobalLinks("c_nano", "gcc")` in `SetOnApply` — this places libc inside `--start-group`/`--end-group` so archive dependency symbols resolve. (3) `-specs=nano.specs` links libc after the group; if a library supplied as a plain `-l` provides symbols only libc references, use `EXTERN` in the linker script or global links. See `references/gotchas.md` for the full explanation.

- `SetProvidedLinkerScript(path)` — chip/bsp declares linker script for consumers (fatal on double-set)
- `UseDependencyLinkerScript()` — firmware target auto-inherits `-T` from first dependency that provides one
- `SetLinkerScript(path)` — direct linker script on target (fatal on double-set)
- `AddPostLink(tool, args...)` — post-link step; `tool` must be one of the toolchain's built-in tools (`objcopy`, `size`, `objdump`, `nm`, `strip`; any other name fails with `post-link tool not found`). Shorthands: `AddPostLinkHex/Bin/Size/Strip`
- `AddPostLinkOutputs(paths...)` — declare extra output files explicitly, with `{output}` templates or SourceDir-relative/absolute paths. Missing outputs trigger relink and all post-link steps; only declared outputs are automatically installed. Hex/Bin/Strip declare their outputs automatically. AddPostLink arguments never imply outputs, including positional inputs and `--add-gnu-debuglink={output}.debug`
- `AddPostLinkDeps(files...)` — declare extra post-link input files (SourceDir-relative, like `AddFiles`); a dep whose content (or permissions) changed or that is missing triggers relink + re-run of ALL post-link steps (a bare `touch` does not count). Without it, editing a file consumed by a post-link step (e.g. an `objcopy --keep-global-symbols` list) is silently skipped
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

## Serial Targets and Incremental Builds

All VMake targets execute serially in target dependency order. `-j N` bounds compilation within the current target and the `Make`/`CMakeBuild` helpers. `-j0` uses CPU count; negative values are errors. Explicit helper arguments, backend arguments and parallel environment settings must be positive and no greater than the session budget; smaller values reduce parallelism. Bare `-j` and inherited Make jobserver tokens are rejected. CMakeInstall is serial unless parallelism is requested. Arbitrary `Run`/`RunIn` commands remain caller-controlled.

Native compilation and linking reuse a versioned action success record only when ordered commands, tool identity, effective environment, and input/output content still match. Objects are isolated by target and source path. Missing outputs and failed post-link steps force a retry; prebuilt artifacts with post-link steps are copied before modification.

Local and remote void callbacks run once per build session, with Make/CMake responsible for incremental checks. Neither `.vmake_stamp` nor a nonempty `InstallDir` skips them. `SetConfigFiles` retains package metadata and does not declare target inputs or control skipping.

`BuildSubGraph` runs synchronously during OnBuild and shares completed targets with subsequent subgraphs and the main graph. Reentering it from an executing target callback is rejected. Package configuration, toolchain, and output paths are fixed before its first OnBuild; later conflicting requests fail. Installation reuses the targets declared in this session.

## Install

| Flag | Description |
|------|-------------|
| `--install` / `-i` | Install after build |
| `--prefix` / `-p` | Prefix (default: `./install/`) |
| `--install-type` | `runtime` (binaries+shared; static libraries and public headers skipped) or `sdk` (everything). Only the exact value `sdk` enables SDK mode; other values behave as `runtime` |

Custom install entries: `ctx.AddInstalls("src/file.conf", "etc/file.conf")` (available in `OnBuild` and `OnInstall`).

### OnInstall Lifecycle

`OnInstall` runs during `--install`, after all builds succeed but before any files are copied: it registers extra install entries and filters, it cannot run shell commands. Use `ctx.AddInstalls("src/file.conf", "etc/file.conf")` for extra copies (docs, configs, licenses) — these are installed together with target outputs. The install root comes from `--prefix` (default `./install/`). Test targets are never installed; without `--install-type sdk`, static libraries are skipped at install (sdk also installs public includes). See `examples/on-install.md`.

## Build Scope

vmake builds packages by BFS from local (directory-based) roots. Remote packages are only built if reachable from a root's transitive dependency chain. If you `AddRequires("pkg")` but no local package depends on it, the package won't be built.

Without an explicit root, the resolver falls back to a heuristic: when no local package declares `OnRequire`, every local package is a root; once any local package declares requires, a local package that declares none and is depended on by another local package is not treated as a root. A hint is printed when no package calls `SetRoot(true)`. In a larger workspace, call `p.SetRoot(true)` once on the entry package (`Main` or `OnPackage`); a top-level package that only declares options must not be the sole root unless it also `AddRequires` the packages it should build.

Run vmake from the project root or any directory inside it. A stray `.vmake/` in a package directory makes that package look like a project when you run vmake there, so package resolution and the lockfile point at the wrong root. Remove stale package-local `.vmake/` directories; `vmake doctor` reports them as `staleProjectMarker` warnings without changing or deleting anything. An ancestor `.vmake/` without `build.go` is not a project root and is ignored.

Bare `vmake` builds with default options (equivalent to `vmake build` without
flags); build flags such as `--tests` and `--mode` require the `build` subcommand.

## Reproducible Builds (vmake.lock + --manifest)

Remote dependency versions and commits — and those of local packages that declare a versioned `SetGit` source — are pinned in `.vmake/vmake.lock` after resolution; commit it alongside the active configuration (`.vmake/config.json` by default). Subsequent builds reuse locked versions; new upstream tags never change what you build until you run `vmake lock update`.

For CI/CD, pin from an install manifest instead:

```bash
# First build: install and write install/manifest.json (versions + revisions)
vmake build --install

# Later build: import manifest pins into vmake.lock BEFORE resolution, then build
vmake build --manifest install/manifest.json
```

`--manifest` resolves the recorded git URLs, refs, and revisions and writes the resulting version/commit pins into `vmake.lock` before dependency resolution, so the graph is built from the pinned versions. During `vmake build --manifest`, local packages with a managed `SetGit` source are skipped by manifest checkout — the build materializes the recorded source version on its own; the separate `vmake manifest checkout` command performs raw checkouts of local entries in their package directories. `vmake manifest show install.json` displays contents; `vmake manifest checkout install.json` restores sources without building. With a valid lock entry and cached checkout, resolution is fully offline.

## CLI Quick Reference

| Command | Description |
|---------|-------------|
| `vmake build` | Build (serial targets; `-j N` bounds per-target compilation and Make/CMake helpers; `-k` keeps going after failure) |
| `vmake build --tests` | Build including test targets |
| `vmake test` | Build + run test targets |
| `vmake rebuild` | Clean + build |
| `vmake config` | TUI for the selected configuration (`--set opt=val` / `--set pkg/opt=val` non-interactive) |
| `vmake config list/use/copy/describe` | List files, select an existing file, copy the active configuration in `.vmake/`, or print/set its description |
| `vmake clean [--all]` | Execute OnClean hooks then remove build outputs (keeps source trees) |
| `vmake distclean [--purge-sources]` | Deep clean: outputs + install/ (keeps .vmake_deps/ working trees unless `--purge-sources`) |
| `vmake query` | Dependency tree; `query targets`, `query config <pkg>` |
| `vmake lock update/show` | Re-resolve / print `.vmake/vmake.lock` pins |
| `vmake toolchain list/show` | Toolchain info |
| `vmake repo add/list/remove/update/trust/untrust` | Package repos + commit-bound trust |
| `vmake ext add/list/remove/update/trust/untrust` | Extension repos + commit-bound trust |
| `vmake cache list` / `vmake cache remove <url>` | Inspect shared git object caches or delete one namespace (`-y` required without a TTY) |
| `vmake pkg list/search/clean/update` | Packages (`pkg update <repo/name>[@version] [--dry-run]`) |
| `vmake manifest show/checkout` | Install manifest |
| `vmake check-symbols [--mode <value> --strict --toolchain <value>]` | Audit dynamic symbols of local packages' default Shared/Binary targets via nm -D (ELF; PE and no-dynsym artifacts report not-applicable) |
| `vmake doctor` | Diagnose platform prerequisites (symlinks, Git userland, make, toolchain) and build.go issues |
| `vmake init-editor [--local <path>] [--remove]` | Generate a scratch go.mod so gopls supports build.go |
| `vmake completion <shell>` | Print completion for bash/zsh/fish/powershell |
| `vmake completion install [--shell <shell>]` | Auto-install completion (bash/zsh/fish; no PowerShell installer) |
| `vmake git tag` | Version tagging |
| `vmake skill install/uninstall/path` | AI skill management (`install --project <dir>` also installs into `<dir>/.claude/skills/`) |
| `vmake update [version]` | Update vmake |
| `vmake version` | Version info (`vmake --version` also prints it) |

Build flags: `--mode`, `--toolchain`, `--install/-i`, `--prefix/-p`, `--install-type`, `--manifest`, `--tests`, `--jobs/-j`, `--keep-going/-k`, `--explain`, `--json`
Global flags: `-v` verbose, `-V` very-verbose, `-q` quiet, `-y/--yes` assume yes (trust prompts), `-h/--help` help
`--version` is a root flag; `vmake test` always builds and runs test targets, so `--tests` is accepted for compatibility but has no effect there.

## Reading Guide

- **Learning the basics** → Start with `examples/simple.md`, then `examples/config.md`
- **Writing a build.go** → Follow the Decision Guide above to pick the right example
- **Mapping config to defines** → `examples/config-to-define.md` (three mechanisms compared)
- **Multi-module workspace** → `examples/multi-module.md`
- **OnClean / OnInstall lifecycles** → `examples/on-clean.md`, `examples/on-install.md`
- **Looking up a specific API** → See `references/api.md` for complete method signatures
- **Build results and `--json`** → `references/build-result.md` (schemaVersion-2 report fields, summary counts, target status, diagnostics, test report, clean interactions)
- **CLI usage** → See `references/cli.md` for full command tree
- **Directory / path resolution details** → `references/dirs.md` (BuildKey, SetGit paths, SourceDir vs SrcDir)
- **Advanced gotchas** → `references/gotchas.md` (static lib deps, OnApply/global flags, source patching)
- **Repository docs (not shipped)** → `docs/DESIGN_DECISIONS.md`, `docs/EXTENSION_PLUGIN.md`, and `docs/FIRMWARE_BUILD_DESIGN.md` exist only in the vmake source repository and are not installed with this skill. Use the shipped references and examples for operational rules.
- **Advanced patterns** → `examples/complete.md`, `examples/subbuild.md`, `examples/embedded-rtos.md`, `examples/firmware.md`, `examples/config-propagate.md`, `examples/prebuilt.md`, `examples/third-party-wrapper.md`
- **Cross-compiling / bare metal** → **Cross-Compiling** above, plus `examples/embedded-rtos.md` and `examples/firmware.md`

## Key Conventions

- Use `filepath.Join()` for filesystem paths
- Package IDs use `/`: `official/zlib`
- Target IDs use `:`: `lib:utils`
- `OnPackage` with `SetGit`/`AddVersion` works for both registry packages and local packages wrapping remote libraries
- `OnPackage` and `AddKConfig` are single-slot — a second registration is a build error (one kconfig entry per package)
- `SetLanguages()` exists but has no effect — language is auto-detected from file extension
- For **local** packages using `SetGit`, `AddFiles` paths resolve from `SourceDir()` — always prefix with `"src/"` (registry/native packages resolve from the checkout root directly)
- Relative file IO inside build.go resolves against the package's `SourceDir()` once bound, otherwise the build.go's directory (see Script-relative file IO above)
- Pass `[]string` directly to `...any` target helpers (`AddFiles`, `AddIncludes`, `AddDefines`, `AddCFlags`, `AddLinks`, ...); they flatten slices. For those, `xs...` does not compile because Go rejects `[]string` for `[]any` (yaegi reports the same call-site error via `reflect.CallSlice`). `...string` helpers such as `AddDeps`, `AddProvidedLibs`, `AddExcludeLibs`, and `AddPostLinkDeps`/`AddPostLinkOutputs` accept `xs...` normally.
