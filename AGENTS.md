# VMake - AGENTS.md

VMake: 面向 AI 时代的 C/C++ 项目管理和编译工具。AI coding agents working in this codebase should follow these guidelines.

## Build / Lint / Test

```bash
go build -o vmake ./cmd/vmake    # Build
go vet ./... && gofmt -w .       # Lint
```

No Go unit tests. Test via integration projects in `test_data/` (each must run from its own directory):

```bash
# Single test
cd test_data/01_simple_c && ../../vmake build && ./hello

# Run all tests (01-17)
for d in test_data/0[1-9]_*/ test_data/1[0-9]_*/; do (cd "$d" && ../../vmake build) || break; done

# Firmware test (17_firmware) — tests stamp skip, KConfig presets, EnsureConfig
cd test_data/17_firmware && ../../vmake build
```

Known pre-existing failures (ignore): `07_subbuild_codegen`, `08_with_package`, `09_with_curl` (mbedtls submodule), `10_local_repo` (package not found).

## Development Mode

When iterating on vmake itself, set `VMAKE_DIR` so plugins compile against local source instead of installed version:

```bash
source start_dev.sh   # exports VMAKE_DIR=$(pwd)
```

Or manually: `export VMAKE_DIR=/path/to/vmake`. Without this, `go build -o vmake ./cmd/vmake` still works but plugin compilation may mismatch.

## Core Concepts

- **源 (Source)** — 包的仓库。**Registry**（包装第三方 C/C++ 库，build.go 在仓库内作为 wrapper）和 **Native**（vmake 原生包，独立 git 仓库，build.go 在仓库根目录，版本来自 git tag）。
- **包 (Package)** — 由 `build.go` 描述的编译单元。注册回调：`OnRequire`（声明依赖）、`OnConfig`（配置选项）、`OnBuild`（定义目标）。
- **目标 (Target)** — 最终编译产物：`TargetBinary`、`TargetStatic`、`TargetShared`、`TargetObject`、`TargetVoid`。

## Key Design Decisions

### 一切皆包，一切皆依赖
uboot, kernel, busybox, app, partitions, firmware are ALL packages with the same lifecycle. No special-casing.

### No Fallbacks — Fix the Root Cause
When a function receives wrong input, fix the caller. Never add fallback chains or `if x == "" { x = y }` guards to hide bugs.

### Local vs Remote Unification
The build pipeline treats local and remote packages identically. `IsLocal()` is only used for directory resolution (`makeLocalPkgDirs` vs `makeRemotePkgDirs`), never for behavioral branching.

### SrcDir vs SourceDir vs BuildDir
- `SourceDir`: package root (where build.go lives)
- `SrcDir()`: source code directory (may differ if `SetGit()` downloads source to `<SourceDir>/src/`)
- `BuildDir`: always under `<SourceDir>/build/<key>/` (separate from source)
- `pkg.Make()` uses BuildDir. When Makefile is in SourceDir, use `pkg.RunIn(srcDir, "make", ...)`

### KConfig Preset = Make Target Name
Preset files under `configs/` are partial configs (defconfig format), NOT complete `.config`. The preset name is passed to `make <preset>` to generate `.config`. Lifecycle: TUI select → save name to config.json → on build: check `.config` → if missing, `make <preset>` → build.

### Stamp-Based Skip for Void Targets
Local packages without InstallDir use `.vmake_stamp` in BuildDir. Stale when config files (`SetConfigFiles()`) are newer, deleted, or `.config` size becomes 0.

### EnsureConfig + PatchKConfig Abstraction
`pkg.EnsureConfig(srcDir) bool` checks `.config` existence + size > 0, runs `make <preset>` if needed. `PatchKConfig(map[string]string)` applies post-defconfig patches in EnsureConfig, restoreKConfigFiles, and TUI ensureConfigCmd.

### Abstraction Boundaries
- `restoreKConfigFiles` only restores from config.json — does NOT call `make <preset>`
- `make <preset>` stays in `EnsureConfig` (build) and `ensureConfigCmd` (TUI)
- Only abstract `.config` check (`EnsureConfig`), NOT a full `BuildKConfigMake` wrapper

### Auto-Wire Require → Build Deps
`OnRequire`/`AddRequires` alone does NOT create build graph edges. `Target.AddDeps("pkg:target")` is required for topology. However, `autoWireRequireDeps()` in `build_cmd.go` auto-wires them when a package has `AddRequires` calls but its targets lack explicit `AddDeps` — it links all of the dependency package's targets as deps.

### TUI Save Iterates Union of All Sources
When saving TUI results, iterate the union of `Values ∪ AllKConfigs` in a single loop. Don't add parallel loops per source.

### restoreKConfigFiles Skip Rules
- No config.json entry for package → skip entirely (don't delete `.config`)
- config.json has entry but kconfig empty (preset switch) → delete `.config`
- config.json has kconfig content → write only if content changed (avoid mtime update invalidating stamp)
- Empty kconfig content → don't write anything

## Runtime Execution Flow
```
Phase 1: OnRequire       -> Scan/Compile/Load buildscripts -> Resolve dependencies
Phase 2a: ResolveDeferred -> Resolve remote (deferred) packages -> Update topological order
Phase 2b: OnConfig       -> Execute callbacks -> Collect Options -> Merge global options
Phase 3: OnBuild         -> Execute callbacks -> Generate Targets -> Compile/Link
(Optional) Install       -> Install targets to prefix directory + generate manifest.json
```

Entry point: `cmd/vmake/main.go` → `loadPlugins()` → `Execute()` (cobra). Pipeline in `cmd/vmake/root.go` (`runPipeline`). Build logic in `cmd/vmake/build_cmd.go`.

## Code Style

### No Comments
Never add comments unless explicitly requested.

### Imports Ordering
Three groups separated by blank lines: stdlib -> external -> local:
```go
import (
    "context"
    "fmt"

    "github.com/spf13/cobra"

    "gitee.com/spock2300/vmake/pkg/api"
)
```
Internal packages may use short aliases: `vlog "gitee.com/spock2300/vmake/pkg/log"`, `exec "gitee.com/spock2300/vmake/internal/exec"`

### Naming Conventions
- **SetXxx**: Set a single value (SetKind, SetDefault)
- **AddXxx**: Append multiple values (AddFiles, AddIncludes)
- **RemoveXxx**: Remove from slices (RemoveCFlags, RemoveDefines)
- **Type aliases**: Use for readability (`type TargetKind string`)
- **Logging**: Always use alias `vlog "gitee.com/spock2300/vmake/pkg/log"` — methods: Debug, Info, Error, Fatal — **no Warn**

### Fluent API
All public APIs use method chaining - return `*Target`, `*Package`, `*Option`:
```go
ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c").AddIncludes("include")
```

### Error Handling
- Library code never panics, always return error with context: `fmt.Errorf("git clone %s -> %s: %w", url, dir, err)`
- CLI code uses `vlog.Fatal()` or `os.Exit(1)` for user-facing errors
- `pkg/api` context helpers use `vlog.Fatal()` for user-facing errors
- Cycle errors must be `vlog.Fatal`, NOT `vlog.Error` (BUG-1 lesson)

### Cross-Platform Paths
- Filesystem paths: `filepath.Join()`
- Logical identifiers: string concatenation (`repo/name`, `pkg:target`)
- Go 1.26 `filepath.Join("/a/b", "/a/b/c")` returns `/a/b/a/b/c` — NOT `/a/b/c`

### Code Organization
- Struct fields are private; access via getter/setter methods
- Exceptions: `InstalledPackage`, `PackageMeta`, `Toolchain`, `Tools`, `PackageNode`, `Graph` have public fields
- Within a file: setters first, then getters, then remove methods, then private helpers
- No interfaces — use function type aliases (`type ConfigFunc func(...)`) and struct-embedded function fields

### Struct Embedding (Composition)
Context types embed shared accessors to inherit behavior:
```go
type BuildContext struct {
    ConfigAccessor    // embedded: provides Bool(), String(), Option()
    *TargetRegistry   // embedded: provides Target(), GetTargets()
    pkgName string
}
```

### Function Type Patterns
Common callback types defined in `pkg/api/`: `RequireFunc`, `ConfigFunc`, `BuildFunc`, `InstallFunc`, `PackageFunc`, `CopyFilter`, `InstallFilterFunc`.
Define as `type XxxFunc func(...)` and store as struct fields.

### Generics
Use Go generics for type-safe helpers: `func getTypedValue[T any](...)`, `func execFuncs[T any](...)`.

### Context Usage
`context.Context` used in `internal/exec` for timeout/cancellation:
```go
type RunOptions struct { Context context.Context; Timeout time.Duration }
```
Auto-creates timeout context if `Timeout > 0` and `Context` is nil.

### CLI Error Helper
```go
func fatalErr(err error) {
    if err != nil { vlog.Error("Error: %v", err); os.Exit(1) }
}
```

### API Execution Methods
Methods on `*Package` (used in build.go scripts):
- `p.Run(name, args...)` — run command in BuildDir (uses exec.RunFatal, exits on failure)
- `p.RunIn(dir, name, args...)` — run command in specified directory
- `p.RunEnv(env, name, args...)` — run with custom environment in BuildDir
- `p.Make(args...)` — always uses BuildDir with `p.Env()`, passes `-C BuildDir` automatically
- `p.CMakeConfigure()`, `p.CMakeBuild()`, `p.CMakeInstall()` — CMake convenience methods
- `p.Configure(args...)` — autotools configure

Methods on `BuildContext`:
- `ctx.Exec(name, args...)` — build-phase command execution (vlog.Fatal on error)
- `ctx.BuildSubGraph(pkgName)` — build a sub-package as independent sub-graph
- `ctx.DepOutput(depRef)` — get dependency target output file path
- `ctx.DepBuildDir(depRef)` — get dependency build directory

## Package Structure

| Package | Responsibility | Plugin Importable |
|---------|---------------|-------------------|
| `pkg/api` | Core API (Package, Target, Option, Contexts, Semver, KConfig, Copy) | **Yes** |
| `pkg/plugin` | Extension/plugin system | **Yes** |
| `pkg/buildscript` | Build script scan, compile, load | No |
| `pkg/build` | Build execution, compile, link, scheduler, install, subgraph | No |
| `pkg/toolchain` | Toolchain abstraction (GCC, Clang) | No |
| `pkg/repo` | Package management, Git, native repos | No |
| `pkg/resolver` | Dependency graph, deferred resolution | No |
| `pkg/config` | Project configuration management | No |
| `pkg/log` | Logging (Debug, Info, Error, Fatal) | No |
| `pkg/tui` | Terminal UI (interactive config) | No |
| `pkg/version` | Version information | No |
| `internal/*` | exec, fs, gitstore, glob, gocompile, jsonio, toposort | No |

**Dependency DAG**: `internal/*` -> `pkg/toolchain` -> `pkg/api` -> `pkg/buildscript, pkg/repo` -> `pkg/resolver, pkg/plugin, pkg/build` -> `cmd/vmake`

## CLI Architecture
- `github.com/spf13/cobra`, package-level vars, `init()` registration
- Command factories (`newRemoveCmd`, `newUpdateCmd`) in `cmd/vmake/helpers.go`

### Build Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--force` | `-f` | Force buildscript recompilation |
| `--toolchain` | | Override toolchain |
| `--mode` | | Override build mode (debug/release) |
| `--install` | `-i` | Install after build |
| `--prefix` | `-p` | Installation prefix (default: `./install/`) |
| `--install-type` | | `runtime` (default) or `sdk` |
| `--manifest` | | Pin versions from manifest file |
| `--tests` | | Include test targets in build |

## Build Script System
Each `build.go` is compiled to a Go plugin (`.so`):
```go
package main
import "gitee.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) { ... })
    p.OnBuild(func(ctx *api.BuildContext) { ... })
}
```

## Package Repositories
Registry checked first; native is fallback. Add commands:
```bash
vmake repo add name url                  # Registry
vmake repo add --native name "https://..../{name}.git"  # Native
```

## Key Types
```go
type TargetKind string  (TargetBinary, TargetStatic, TargetShared, TargetObject, TargetVoid)
type OptionType int     (OptionBool, OptionString, OptionInt, OptionChoice)
type SourceOrigin int   (SourceLocal, SourceRemote)
type PkgDirs struct { SourceDir, BuildDir, InstallDir string }
```

## Test Targets (`SetTest` / `vmake test`)

- `SetTest(true)` marks a target as a test; auto-sets `isDefault=false`
- `IsTest()` getter on `Target`
- `vmake build` skips test targets; `vmake build --tests` includes them
- `vmake test` builds all test targets, then executes `TargetBinary` tests, reports pass/fail with timing, `os.Exit(1)` on failure
- `publishTarget` and `installTarget` skip `IsTest()` targets (test binaries are never installed)
- Test targets can depend on other test targets (e.g., `TargetStatic` test lib used by multiple test binaries); only `TargetBinary` tests are executed
- Subgraph builds inside `executeAllOnBuild` activate tests via `includeTests` parameter

## Known Gotchas
- `cmd/vmake/mainfest_cmd.go` — filename is misspelled ("mainfest" not "manifest"), do NOT rename
- `vlog` has `Debug`, `Info`, `Error`, `Fatal` — no `Warn` method
- `test_data/09_with_curl` and `test_data/10_local_repo` have pre-existing failures unrelated to code changes
- `exec.Command` doesn't expand shell features — `$(nproc)` won't work, must use `runtime.NumCPU()`
- `collectNeeded` must use BFS from `IsLocal()` roots — NOT `node.Pkg != nil` and NOT full graph mark-all
- `populateDepsFromGraph` only fills `pkg.Deps()` for packages with non-empty `InstallDir`
- `tea.ExecProcess` (takes `*exec.Cmd`) vs `tea.Exec` (takes `tea.ExecCommand` interface) — use `ExecProcess` for external interactive commands
- `scheduler.SetPackage` stores first assignment — `packages[name]` lookup finds original, not re-created one
- `busybox` kconfig has no `olddefconfig` — only `oldconfig`, `defconfig`, `allnoconfig`

## What Not To Do
- IDE integration plugins
- Remote builds / distributed compilation
- MSVC toolchain (not yet supported)
- Don't add fallback chains — fix root cause
- Don't use `pkg.Make()` when Makefile is in SourceDir — use `pkg.RunIn(srcDir, "make", ...)`
- Don't run tests from `test_data/` parent — each sub-project must be built from its own directory
