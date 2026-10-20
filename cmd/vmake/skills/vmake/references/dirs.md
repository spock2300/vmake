# Directory Reference

## Quick Reference Table

| Property | What it returns | When to use |
|----------|-----------------|-------------|
| `SourceDir()` | Package root — the `build.go` dir for local packages; for remote packages the member directory inside the package’s single working tree (`.vmake_deps/<repo>/<owner>/src[/<member>]`) | Package metadata files, overlay dirs |
| `SrcDir()` | Source code dir (`SourceDir()/src/` for local `SetGit` packages; a native member carrying its own repository is set to its nested tree; otherwise falls back to `SourceDir()`) | Source files for firmware/third-party builds |
| `SrcDirRaw()` | Raw source dir without the SourceDir fallback; the framework sets it for `SetGit` workspaces | Inspecting the configured source dir |
| `BuildDir()` | Scratch dir for intermediate artifacts | Build outputs and action success records |
| `InstallDir()` | Remote package installation prefix; empty for local packages | Remote package publication |
| `CMakeBuildDir()` | `BuildDir()/cmake`, unless explicitly set | CMake cache and intermediate artifacts |
| `CMakeInstallDir()` | Remote `InstallDir()` or local `BuildDir()/staging`, unless explicitly set | Headers/libs installed by CMake |
| `ScriptDir()` | Directory containing `build.go` (set automatically by the script loader via `SetScriptDir`) | Patch resolution; differs from `SourceDir()` for registry packages |

## BuildKey — Build Directory Naming

The `build/` directory is named by a **SHA-256 hex hash**, not a readable name — so multiple build variants coexist under `build/` without clobbering each other:

```
build/a1b2c3d4e5f6789012345678abcdef0123456789abcdef0123456789abcdef01/
```

BuildDir path by package origin:
- **Local packages**: `<SourceDir>/build/<buildKey>/`
- **Remote packages**: `.vmake_deps/<repo>/<owner>/out/<sha256(member)>/<buildKey>/build/` — the sibling `install/` holds published headers/libs. The package's single working tree lives at `.vmake_deps/<repo>/<owner>/src`; native subpackages use `src/<member>`, and switching configurations only changes the `out/<sha256(member)>/<buildKey>` directory

The BuildKey hashes the format version and `(toolchain identity, build_mode, options)` plus extra material: the **global-flags hash** for every package, extended with a `default-visibility=hidden` marker when `SetDefaultVisibilityHidden()` is active; local `SetGit` packages additionally contribute the resolved **source commit**; remote packages contribute **source version, commit and patch-set hash**. Buildscript text is action-level: commands, includes/defines and file lists are captured by action signatures, so a comment-only build.go edit does not rotate `BuildDir`. `AddBinHeader` rules additionally record the declared input in the generated header's first line so switching the input regenerates the header even without a key rotation. Artifacts written into `BuildDir()` by the script outside declared actions are not tracked. Version switches, global flag changes and remote patch edits produce a fresh key instead of silently reusing stale artifacts. A local `SetGit` patch-only change is restored into the existing tree but does not rotate its `BuildDir`. The BuildKey is deterministic — same inputs always produce the same hash. `compile_commands.json` is rebuilt for the current session and merged across schedulers by `(source file, object output)` at `<project root>/build/compile_commands.json`. `AddBinHeader` output goes to `BuildDir()/generated/`.

## SourceDir vs SrcDir

For local `SetGit`, `SourceDir()` stays at the package root. `SourceDir()/src` is a managed symlink to the package's single working tree at `.vmake_deps/local/<pkg>/src`; the link target no longer changes with the build key. Use `SrcDir()` for this actual source tree.

Remote registry and native packages build directly in their working tree at `.vmake_deps/<repo>/<owner>/src`; native subpackages use their relative path inside it (`src/<member>`), and a member carrying its own repository gets a nested tree at the member directory. `SourceDir()` points there before OnBuild runs; patches apply to `SrcDir()` in place (a changed patch set resets the tree to its pinned commit first). `ScriptDir()` identifies the loaded buildscript's directory and may differ. Build outputs live in `out/<sha256(member)>/<buildKey>/{build,install}` beside `src`; do not treat the tree as immutable — it is the shared writable source for every configuration of the project.

For local packages without SetGit, `SrcDir()` defaults to `SourceDir()` unless `SetSrcDir` overrides it. `SrcDirRaw()` reports the raw source-dir setting without the fallback. Once OnBuild exposes paths or synchronous subgraphs build a package, the session retains those paths through installation.

Path getters are bound after the package's source preparation; do not rely on `SourceDir()`/`BuildDir()`/`SrcDir()` before the build phase (they may still be empty in `OnPackage`/`OnConfig` and are populated for `OnBuild`).

Inspection and dry-run commands (`vmake inspect`, `vmake query`, `query targets`, `check-symbols`) declare targets in dry-run, and `inspect`/`query` block interpreted-script writes. The declaration step does not materialize sources, but native remote resolution during `Require` may materialize a tree. Needed packages with a present source tree still carry their computed `BuildDir()` during `inspect`; a local `SetGit` package or remote package whose expected tree is absent omits `buildDir`. `OnBuild` must guard real file I/O with `p.DryRun()` rather than relying on `p.BuildDir() == ""`, and must not assume the `src` link resolves.

## Action → Directory

| Action | Working directory | Notes |
|--------|-------------------|-------|
| `p.Make(...)` | `BuildDir()` | For a source-tree Makefile use `p.Make("-C", filepath.ToSlash(absSrcDir), ...)` with an absolute `SrcDir()` |
| `p.Run(...)` / `p.RunIn(dir, ...)` | `BuildDir()` by default | `RunIn` selects the directory explicitly |
| `p.Configure(...)` | configures `SrcDir()` out-of-source into `BuildDir()` | install prefix from `InstallDir()`; empty for local packages |
| `CMakeConfigure` / `CMakeBuild` / `CMakeInstall` | `BuildDir()` (configure with `--preset` uses `SrcDir()`) | `CMakeBuildDir()`/`CMakeInstallDir()` are passed as `-B`/`--build`/`--install`/`--prefix` arguments; see CMake Directories |
| `SetPrebuilt(path)` source | existing file at `path` (absolute or `SourceDir()`-relative) | output published at `BuildDir()/<TargetFilename>` (symlink, or copy with post-link steps) |
| Install / publish | remote `InstallDir()`; local `--prefix` (default `./install/`) | `InstallDir()` is empty for local packages |

Within `SetBuildFunc`, built-in helpers (`CMakeConfigure`, `CMakeBuild`, `CMakeInstall`) automatically use the correct directories. If you need to read or patch source files manually, use `SrcDir()` to locate the downloaded source tree.

## CMake Directories

Prefer the CMake helpers for configuring, building, and installing CMake projects.
All three use `CMakeBuildDir()`; configure reads `SrcDir()` and install writes to
`CMakeInstallDir()`. Use the getters when referring to generated files, installed
headers, or `SetPrebuilt` artifacts.

`SetCMakeBuildDir(dir)` and `SetCMakeInstallDir(dir)` return `*Package`. Relative
paths resolve against `BuildDir()`; absolute paths remain absolute. These settings
do not change `PkgDirs` or the once-per-session execution of void targets.
If a remote wrapper chooses a custom CMake install directory, it must publish its
results into the package's `InstallDir()` or declare them explicitly.

`CMakeConfigure` records its fingerprint in `<CMakeBuildDir>/.vmake_configure.json`
and skips configure while the fingerprint is unchanged **and** the recorded
configure outputs still exist (`CMakeCache.txt`, `CMakeFiles/`, and the known
generator's top-level build file). It logs `cmake configure (cached):
<CMakeBuildDir>`. Changing the configure arguments, the resolved cmake
executable, `CMAKE_*`/`PATH`, or any `CMakeLists.txt`/`*.cmake` input under
`SrcDir()` invalidates the cache and re-runs configure; deleting the outputs
also forces a re-run.

For a local static library named `foo`, CMake may build
`<BuildDir>/cmake/libfoo.a`, install `<BuildDir>/staging/lib/libfoo.a`, and VMake can
publish `<BuildDir>/libfoo.a` with `SetPrebuilt`. Keep these paths distinct so the
CMake archive does not occupy VMake's symlink destination.

Migration: replace old `BuildDir()` references to CMake artifacts with
`CMakeBuildDir()`, and use `CMakeInstallDir()` for CMake-installed artifacts. Replace
raw `-B`/installation-prefix overrides with the setters, and use `CMakeBuild()` /
`CMakeInstall()` instead of running make after configure. Clean the old build tree
before the first build after migration.

## Target Source Paths

Relative `AddFiles` paths are `SourceDir()`-relative (even with `SetGit`, use `"src/tasks.c"` not `"tasks.c"`); absolute paths are used as-is. Relative `AddPublicIncludes` propagated to dependents resolves from `SourceDir()` through `PkgInfo.ResolvePath`; absolute paths are preserved.

## Path Resolution for Packages Using SetGit

When a **local** package uses `SetGit`, `SourceDir()` and `SrcDir()` differ: `SourceDir()` is where `build.go` lives, `SrcDir()` = `SourceDir()/src/` is the downloaded source. This causes non-obvious path resolution behavior in `OnBuild` (registry packages instead resolve paths directly from the downloaded checkout root — `SourceDir()` — with no `src/` nesting):

- **`AddFiles`** relative paths resolve from `SourceDir()`. Use `"src/tasks.c"`, NOT `"tasks.c"`; a bare filename only matches at the package root. Absolute paths are used as-is.
- **`AddIncludes`** paths resolve from `SourceDir()`. Use `"src/include"` to reach downloaded headers.
- **`AddPublicIncludes`** relative paths propagate to dependents resolved from `SourceDir()` through `PkgInfo.ResolvePath`; absolute paths are preserved. Use `"src/include"` to reach headers downloaded by `SetGit` (resolves to `SourceDir()/src/include/`). The target itself gets `AddPublicIncludes` paths as raw `-I` entries, resolved relative to CWD (which the scheduler sets to `SourceDir()` before compilation).
- **Relative parent paths** (`"../include"`) in `AddPublicIncludes` resolve to the parent of `SourceDir()` (`filepath.Join(SourceDir, "../include")`), i.e. outside the package. Conversely, on registry packages a `"src/..."` prefix produces doubled paths (e.g., `/path/src/src/include`) because `SourceDir()` is already the downloaded checkout root. Avoid both — place headers inside `SrcDir()/include/` (local `SetGit` packages) or at the checkout root (registry packages).
- **Absolute paths** in `AddFiles` (and in `AddPublicIncludes` when propagated to dependents) are used as-is; they do not get `SourceDir()` prepended. Prefer relative paths for portability.

```go
// Correct pattern for a local package wrapping a SetGit download:
p.OnPackage(func(p *api.Package) {
    p.SetGit("https://github.com/FreeRTOS/FreeRTOS-Kernel.git")
    p.AddVersion("11.3.0", "V11.3.0")
})
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("freertos").SetKind(api.TargetStatic).
        AddFiles(
            "src/tasks.c",          // SourceDir()-relative
            "src/portable/GCC/ARM_CM4F/port.c",
        ).
        AddPublicIncludes(
            "src/include",                      // SourceDir()-relative (self + propagation)
            "src/portable/GCC/ARM_CM4F",
        )
})
```
