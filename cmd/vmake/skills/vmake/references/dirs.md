# Directory Reference

## Quick Reference Table

| Property | What it returns | When to use |
|----------|-----------------|-------------|
| `SourceDir()` | Package root — the `build.go` dir for local packages; the package member’s writable build workspace for remote packages | Package metadata files, overlay dirs |
| `SrcDir()` | Source code dir (`SourceDir()/src/` for local `SetGit` packages, otherwise falls back to `SourceDir()`) | Source files for firmware/third-party builds |
| `SrcDirRaw()` | Raw srcCodeDir without fallback (empty if `SetSrcDir` not called) | Detecting whether source dir was explicitly set |
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
- **Remote packages**: `vmake_deps/<repo>/<pkg>/out/<sha256(member)>/<buildKey>/build/` — `out` points into `~/.vmake/cache/v2/<repo>/<pkg>/<version>/out`. Each member/build key also owns `install/` and `work/repo/`; matching native actions may reuse successful outputs across projects

The BuildKey hashes the format version and `(toolchain identity, build_mode, options)` plus extra material: the **global-flags hash** and **buildscript hash** for every package, and additionally the **source version, commit and patch-set hash** for remote packages — so version switches, global flag changes, patch edits and build.go edits each produce a fresh key instead of silently reusing stale artifacts. The BuildKey is deterministic — same inputs always produce the same hash. `compile_commands.json` is rebuilt for the current session and merged across schedulers by `(source file, object output)` at `<project root>/build/compile_commands.json`. `AddBinHeader` output goes to `build/<buildKey>/generated/`.

## SourceDir vs SrcDir

For local `SetGit`, `SourceDir()` stays at the package root. `SourceDir()/src` is a managed symlink to the writable `BuildDir()/work/src` copied from a commit-specific cache seed. Use `SrcDir()` for this actual source tree.

Remote registry and native packages build in the member/build-key workspace at `out/<sha256(member)>/<buildKey>/work/repo`; native subpackages use their relative path within their own workspace. `SourceDir()` points there before OnBuild runs, and patches apply there. `ScriptDir()` identifies the loaded buildscript's directory and may differ. The project's ordinary `vmake_deps/<repo>/<pkg>/src` link still points at the immutable seed; do not hard-code that link as a writable build tree. Native child source links at `vmake_deps/<repo>/<pkg>/_members/<sha256(member)>/src` point to the child's actual source tree. The member is its repository-relative path with forward slashes; hashing keeps names such as `src` and `out` separate from the parent's links.

For local packages without SetGit, `SrcDir()` defaults to `SourceDir()` unless `SetSrcDir` overrides it. `SrcDirRaw()` reports only the explicit source-directory setting. Once OnBuild exposes paths or synchronous subgraphs build a package, the session retains those paths through installation.

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

All `AddFiles` paths are `SourceDir()`-relative (even with `SetGit`, use `"src/tasks.c"` not `"tasks.c"`). `AddPublicIncludes` propagated to dependents resolves from `SourceDir()` (via `filepath.Join` with the dep's `PkgDirs.SourceDir`).

## Path Resolution for Packages Using SetGit

When a **local** package uses `SetGit`, `SourceDir()` and `SrcDir()` differ: `SourceDir()` is where `build.go` lives, `SrcDir()` = `SourceDir()/src/` is the downloaded source. This causes non-obvious path resolution behavior in `OnBuild` (registry packages instead resolve paths directly from the downloaded checkout root — `SourceDir()` — with no `src/` nesting):

- **`AddFiles`** paths resolve from `SourceDir()`. Use `"src/tasks.c"`, NOT `"tasks.c"`. Single filenames without glob characters (`*`, `?`) may silently match nothing — always use the `src/` prefix.
- **`AddIncludes`** paths resolve from `SourceDir()`. Use `"src/include"` to reach downloaded headers.
- **`AddPublicIncludes`** paths propagate to dependents resolved from `SourceDir()` (via `filepath.Join(depPkg.SourceDir, pubInc)`). Use `"src/include"` to reach headers downloaded by `SetGit` (resolves to `SourceDir()/src/include/`). The target itself gets `AddPublicIncludes` paths as raw `-I` entries, resolved relative to CWD (which the scheduler sets to `SourceDir()` before compilation).
- **Relative parent paths** (`"../include"`) in `AddPublicIncludes` resolve to the parent of `SourceDir()` (`filepath.Join(SourceDir, "../include")`), i.e. outside the package. Conversely, on registry packages a `"src/..."` prefix produces doubled paths (e.g., `/path/src/src/include`) because `SourceDir()` is already the downloaded checkout root. Avoid both — place headers inside `SrcDir()/include/` (local `SetGit` packages) or at the checkout root (registry packages).
- **Absolute paths** in `AddFiles` (and in `AddPublicIncludes` when propagated to dependents) get `SourceDir()` prepended, creating wrong paths like `/src/home/user/project/src/file`. Always use relative paths.

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
