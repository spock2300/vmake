# OnClean Lifecycle

Demonstrates custom clean logic using `OnClean`. Needed when your package has build artifacts that `vmake clean` doesn't know about (e.g., Makefile-generated files in the source tree, code-generated outputs, temporary test data).

## Prerequisites

- A project that resolves and, for `ctx.SrcDir()`, an existing source tree: either a plain local package or a local `SetGit` package whose managed `src/` link has been materialized by a previous build.
- The hook command (`make` here) available in the configured toolchain environment.

## build.go

```go
package main

import (
	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
	})

	p.OnClean(func(ctx *api.CleanContext) {
		ctx.RunIn(ctx.SrcDir(), "make", "clean")
	})
}
```

## What This Demonstrates

- **`p.OnClean(func(ctx *api.CleanContext))`** — Custom clean hook (runs during `vmake clean`, `vmake clean --all`, `vmake distclean`, and before every `vmake rebuild`)
- **`ctx.Run(name, args...)`** — Run command in BuildDir; a failure raises a `BuildScriptError`
- **`ctx.RunIn(dir, name, args...)`** — Run command in specified directory
- **`ctx.SrcDir()`** — Actual source tree; use it for downloaded sources (`SetGit`). For plain local packages it equals `SourceDir()`

## Key Points

- `OnClean` is a **separate pipeline** from build — it doesn't run during `vmake build`
- `vmake clean` executes `OnClean` hooks first, then removes the current configuration's build outputs; `vmake clean --all` removes every configuration's output directories. Source trees under `.vmake_deps/` are kept in both cases.
- `vmake distclean` also executes `OnClean` hooks, then removes all outputs, `install/`, `build/compile_commands.json`, and the build report. Downloaded working trees under `.vmake_deps/` are kept and reused (no re-download on the next build); pass `--purge-sources` to remove them too
- `vmake rebuild` executes the hooks (local packages only) before its clean + build, so hooks must be safe to run on a tree that is about to be rebuilt
- Failure semantics: a failing command raises a `BuildScriptError`. Plain `vmake clean` aborts with that error; `vmake clean --all`, `vmake distclean` and `vmake rebuild` log `Skipping OnClean: ...` and continue
- The hook executes with its working directory set to the package `SourceDir()`. Wrapped `os.*` relative paths resolve script-relative (against the loaded `build.go` directory); `os.Chdir` is rejected because it would break parallel builds — use `RunIn` instead
- `SourceDir()` vs `SrcDir()`: for local `SetGit` packages `SourceDir()` is the package root (where `build.go` lives) while `SrcDir()` is the downloaded source tree (`SourceDir()/src`, a managed symlink). Remote packages build in their own workspace, where `SourceDir()` is the checkout and `SrcDir()` falls back to it. See `references/dirs.md`
- `ctx.Make(args...)` runs `make -C <BuildDir> ...` — BuildDir, not the source tree. For an in-source Makefile use `ctx.RunIn(ctx.SrcDir(), "make", ...)`
- `ctx.Run`/`ctx.RunIn` raise a `BuildScriptError`; `RunEnv` and `Make` return errors instead

## Scope: clean vs distclean

| Command | OnClean hooks | Local `build/` dirs | `build/compile_commands.json` | `install/` | `.vmake_deps/` |
|---|---|---|---|---|---|
| `vmake clean` | run (current configuration) | current configuration only | kept | kept | kept |
| `vmake clean --all` | run, failures skipped | all configurations | kept | kept | kept |
| `vmake distclean` | run, failures skipped | all configurations | removed | removed | kept (removed by `--purge-sources`) |

**Do not delete `.vmake_deps/` or the project's `.vmake/_locks/` directory from a hook.** Deleting project working trees through a hook destroys source modifications and forces a re-download. Only remove artifacts your own build produced, using paths under `ctx.SrcDir()` / `ctx.BuildDir()`.

## Running / Verifying

```bash
vmake build                     # create artifacts your hook knows about
vmake clean                     # expected: Executing OnClean... then Clean completed!
vmake clean --all               # every configuration
vmake distclean                 # + install/; keeps .vmake_deps/ (use --purge-sources to remove)
```

## When to Use OnClean

- Your `SetBuildFunc` runs `make` in `SrcDir()` and produces build artifacts there (U-Boot, Linux kernel, busybox)
- Your build generates derived files (code generation, template expansion, asset processing)
- You have a custom build system that needs `make clean` or equivalent cleanup

## See Also

- references/dirs.md — `SourceDir` vs `SrcDir`, `BuildDir`, BuildKey
- SKILL.md - Common Mistakes
- examples/simple.md — Minimal build without clean
- examples/third-party-wrapper.md — TargetVoid with SetBuildFunc pattern
- examples/firmware.md — Real-world firmware with KConfig presets and external incremental builds
