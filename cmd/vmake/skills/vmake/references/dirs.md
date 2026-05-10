# Directory Reference

## Quick Reference Table

| Property | What it returns | When to use |
|----------|-----------------|-------------|
| `SourceDir()` | Package root (where build.go lives) | Package metadata files, overlay dirs |
| `SrcDir()` | Source code dir (`SourceDir()/src/` when `SetGit` downloads source, falls back to `SourceDir()`) | Source files for firmware/third-party builds |
| `SrcDirRaw()` | Raw srcCodeDir without fallback (empty if `SetSrcDir` not called) | Detecting whether source dir was explicitly set |
| `BuildDir()` | Scratch dir for intermediate artifacts | Build outputs, stamps |
| `InstallDir()` | Installation prefix | Headers/libs installed by third-party packages |
| `ScriptDir()` | Same as `SourceDir()` | Legacy alias |

## BuildKey — Build Directory Naming

The `build/` directory is named by a **SHA-256 hex hash** of `(toolchain_path, build_mode, options)` — not by a readable name like `<toolchain>-<mode>`. This hash is called the **BuildKey**, and it exists to allow multiple build variants (different toolchains, modes, option sets) to coexist under `build/` without clobbering each other:

```
build/a1b2c3d4e5f6789012345678abcdef0123456789abcdef0123456789abcdef0123/
```

BuildDir path by package origin:
- **Local packages**: `<SourceDir>/build/<buildKey>/`
- **Remote packages**: `vmake_deps/<repo>/<pkg>/out/<buildKey>/build/`

The BuildKey is deterministic — same toolchain + mode + options always produces the same hash. You can find the current BuildKey in `config.json` entries. The `compile_commands.json` file lives at `build/compile_commands.json` (relative to project root). `AddBinHeader` output goes to `build/<buildKey>/generated/`.

## SourceDir vs SrcDir

For any package using `SetGit` (registry or local), `SourceDir()` is where `build.go` lives, but the actual downloaded source is at `SrcDir()` (= `SourceDir()/src/`). For a local package without `SetGit`, `SourceDir()` == `SrcDir()` (the fallback). Use `SrcDirRaw()` to check whether `SetSrcDir` was explicitly called (returns empty string if not).

Within `SetBuildFunc`, built-in helpers (`CMakeConfigure`, `CMakeBuild`, `CMakeInstall`) automatically use the correct directories. If you need to read or patch source files manually, use `SrcDir()` to locate the downloaded source tree.

All `AddFiles` paths are `SourceDir()`-relative (even with `SetGit`, use `"src/tasks.c"` not `"tasks.c"`). `AddPublicIncludes` propagated to dependents resolves from `SourceDir()` (via `filepath.Join` with the dep's `PkgDirs.SourceDir`).

## Path Resolution for Packages Using SetGit

When a package uses `SetGit`, `SourceDir()` and `SrcDir()` differ: `SourceDir()` is where `build.go` lives, `SrcDir()` = `SourceDir()/src/` is the downloaded source. This causes non-obvious path resolution behavior in `OnBuild`:

- **`AddFiles`** paths resolve from `SourceDir()`. Use `"src/tasks.c"`, NOT `"tasks.c"`. Single filenames without glob characters (`*`, `?`) may silently match nothing — always use the `src/` prefix.
- **`AddIncludes`** paths resolve from `SourceDir()`. Use `"src/include"` to reach downloaded headers.
- **`AddPublicIncludes`** paths propagate to dependents resolved from `SourceDir()` (via `filepath.Join(depPkg.SourceDir, pubInc)`). Use `"src/include"` to reach headers downloaded by `SetGit` (resolves to `SourceDir()/src/include/`). The target itself gets `AddPublicIncludes` paths as raw `-I` entries, resolved relative to CWD (which the scheduler sets to `SourceDir()` before compilation).
- **Relative parent paths** (`"../include"`) in `AddPublicIncludes` produce incorrect doubled paths (e.g., `/path/src/src/include`). Avoid them — place config headers inside `SrcDir()/include/` instead.
- **Absolute paths** in `AddFiles`/`AddPublicIncludes` get `SrcDir()` prepended, creating wrong paths like `/src/home/user/project/src/file`. Always use relative paths.

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
