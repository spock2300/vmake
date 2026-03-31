# VMake - AGENTS.md

AI coding agents working in the VMake codebase should follow these guidelines.

## Build / Lint / Test Commands

```bash
go build -o vmake ./cmd/vmake    # Build
go vet ./... && gofmt -w .       # Lint
```

No Go unit tests. Test via integration projects in `test_data/`:

```bash
# Single test (pick any numbered directory)
cd test_data/01_simple_c && ../../vmake build && ./hello
cd test_data/08_with_package && ../../vmake build

# Run all tests (01-14)
for d in test_data/0[1-9]_*/ test_data/1[0-9]_*/; do (cd "$d" && ../../vmake build) || break; done
```

Known pre-existing failures (ignore): `09_with_curl` (mbedtls submodule), `10_local_repo` (package not found).

## Core Concepts

VMake 是一个面向 AI 时代的 C/C++ 项目管理和编译工具，三个核心概念贯穿始终：

- **源 (Source)** — 包的仓库。两种类型：**Registry**（包装第三方 C/C++ 库，build.go 在仓库内作为 wrapper）和 **Native**（vmake 原生包，独立 git 仓库，build.go 在仓库根目录，版本来自 git tag）。
- **包 (Package)** — 由 `build.go` 描述的编译单元。每个包注册回调：`OnRequire`（声明依赖）、`OnConfig`（配置选项）、`OnBuild`（定义目标）。本地包的 build.go 直接编译；远程包的 build.go 在 `pkg/resolver` 中被发现并加载。
- **目标 (Target)** — 最终编译产物：二进制（`TargetBinary`）、静态库（`TargetStatic`）、共享库（`TargetShared`）、对象文件（`TargetObject`）、第三方 wrapper（`TargetVoid`）。

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
- **Logging**: Always use alias `vlog "gitee.com/spock2300/vmake/pkg/log"` (methods: Debug, Info, Error, Fatal — no Warn)

### Fluent API
All public APIs use method chaining - return `*Target`, `*Package`, `*Option`:
```go
ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c").AddIncludes("include")
```

### Error Handling
- Library code never panics, always return error with context: `fmt.Errorf("git clone %s -> %s: %w", url, dir, err)`
- CLI code uses `vlog.Fatal()` or `os.Exit(1)` for user-facing errors
- `pkg/api` context helpers use `vlog.Fatal()` for user-facing errors

### Cross-Platform Paths
- Filesystem paths: `filepath.Join()`
- Logical identifiers: string concatenation (`repo/name`, `pkg:target`)

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
Use embedding for shared behavior, not inheritance.

### Function Type Patterns
Common callback types: `RequireFunc`, `ConfigFunc`, `BuildFunc`, `InstallFilterFunc`, `CopyFilter`, `MainFunc`.
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
Use `fatalErr(err)` pattern in CLI code:
```go
func fatalErr(err error) {
    if err != nil { vlog.Error("Error: %v", err); os.Exit(1) }
}
```

## Package Structure

| Package | Responsibility | Plugin Importable |
|---------|---------------|-------------------|
| `pkg/api` | Core API (Package, Target, Option, Contexts, Semver) | **Yes** |
| `pkg/plugin` | Extension/plugin system | **Yes** |
| `pkg/buildscript` | Build script scan, compile, load | No |
| `pkg/build` | Build execution, compile, link, scheduler, install | No |
| `pkg/toolchain` | Toolchain abstraction (GCC, Clang) | No |
| `pkg/repo` | Package management, Git, native repos | No |
| `pkg/resolver` | Dependency graph, deferred resolution | No |
| `pkg/config` | Project configuration management | No |
| `pkg/log` | Logging (Debug, Info, Error, Fatal) | No |
| `pkg/tui` | Terminal UI (interactive config) | No |
| `pkg/version` | Version information | No |
| `internal/*` | exec, fs, gitstore, glob, gocompile, jsonio | No |

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

## Runtime Execution Flow
```
Phase 1: OnRequire       -> Scan/Compile/Load buildscripts -> Resolve dependencies
Phase 2a: ResolveDeferred -> Resolve remote (deferred) packages -> Update topological order
Phase 2b: OnConfig       -> Execute callbacks -> Collect Options -> Merge global options
Phase 3: OnBuild         -> Execute callbacks -> Generate Targets -> Compile/Link
(Optional) Install       -> Install targets to prefix directory + generate manifest.json
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

## Known Gotchas
- `cmd/vmake/mainfest_cmd.go` — filename is misspelled ("mainfest" not "manifest"), do NOT rename
- `vlog` has `Debug`, `Info`, `Error`, `Fatal` — no `Warn` method
- `test_data/09_with_curl` and `test_data/10_local_repo` have pre-existing failures unrelated to code changes

## What Not To Do
- IDE integration plugins
- Remote builds / distributed compilation
- MSVC toolchain (not yet supported)
