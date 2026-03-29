# VMake - AGENTS.md

AI coding agents working in the VMake codebase should follow these guidelines.

## Build / Lint / Test Commands

```bash
go build -o vmake ./cmd/vmake    # Build
go vet ./... && gofmt -w .       # Lint
```

No Go unit tests. Test via integration projects in `test_data/`:

```bash
cd test_data/01_simple_c && ../../vmake build && ./hello          # Single test
cd test_data/08_with_package && ../../vmake build                 # Third-party packages
for d in test_data/0[1-9]_*/; do (cd "$d" && ../../vmake build) || break; done  # All tests
```

## Core Philosophy

**Less is More** - Before adding any feature or API, ask: Is this absolutely necessary?

## Code Style

### No Comments
Never add comments unless explicitly requested. Code should be self-explanatory.

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

Internal packages may use short aliases:
```go
import (
    exec "gitee.com/spock2300/vmake/internal/exec"
    vlog "gitee.com/spock2300/vmake/pkg/log"
)
```

### Naming Conventions
- **SetXxx**: Set a single value (SetKind, SetDefault)
- **AddXxx**: Append multiple values (AddFiles, AddIncludes)
- **RemoveXxx**: Remove values from slice (RemoveCFlags, RemoveDefines)
- **Type aliases**: Use for readability (`type TargetKind string`)
- **Logging**: Always use alias `vlog "gitee.com/spock2300/vmake/pkg/log"`

### Fluent API
All public APIs use method chaining - return `*Target`, `*Package`, `*Option`:
```go
ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c").AddIncludes("include")
```

### Error Handling
- Library code never panics, always return error
- Include context: `return fmt.Errorf("git clone %s -> %s: %w", url, dir, err)`
- CLI commands may call `os.Exit(1)` on fatal errors
- Use `vlog.Fatal()` only in CLI/builder code, never in library code

### Cross-Platform Paths
Use `filepath.Join()` for filesystem paths. Do NOT use for logical identifiers:
- Package identifiers: `repo/name` (string concatenation)
- Target identifiers: `pkg:target` (`:` as delimiter)

### Code Organization
- Struct fields are private; access via getter/setter methods (exceptions: `InstalledPackage`, `PackageMeta`, config types)
- Within a file: setters first, then getters, then remove methods, then private helpers
- Struct fields ordered by logical grouping, not alphabetically
- Embedded types at the top of the struct definition

## Package Structure

| Package | Responsibility | Plugin Importable |
|---------|---------------|-------------------|
| `pkg/api` | Core API (Package, Target, Option, Contexts, Semver) | **Yes** |
| `pkg/plugin` | Extension/plugin system for custom commands | **Yes** |
| `pkg/buildscript` | Build script scan, compile, load | No |
| `pkg/config` | Config storage | No |
| `pkg/build` | Build execution, compile, link, scheduler, install | No |
| `pkg/toolchain` | Toolchain abstraction (GCC, Clang) | No |
| `pkg/repo` | Package management, Git, source download, prefix repos | No |
| `pkg/resolver` | Dependency graph, deferred resolution | No |
| `pkg/log` | Logging (use alias `vlog`) | No |
| `pkg/tui` | TUI components (bubbletea) | No |
| `pkg/version` | Version info (GitCommit, BuildDate) | No |
| `internal/exec` | OS command execution | No |
| `internal/fs` | Filesystem utilities (EnsureDir, FileExists, etc.) | No |
| `internal/gitstore` | Generic git-backed store (Add/Remove/List/Path) | No |
| `internal/glob` | Glob pattern matching | No |
| `internal/gocompile` | Go plugin compilation | No |
| `internal/jsonio` | JSON read/write utilities | No |

**Principle**: `pkg/api` and `pkg/plugin` are public APIs that must remain stable. `internal/` packages cannot be imported by `pkg/`.

**Dependency DAG**: `internal/*` -> `pkg/toolchain` -> `pkg/api` -> `pkg/buildscript, pkg/repo` -> `pkg/resolver, pkg/plugin, pkg/build` -> `cmd/vmake`

## CLI Architecture
- `github.com/spf13/cobra`, package-level vars, `init()` registration
- Command factories (`newAddCmd`, `newRemoveCmd`, `newUpdateCmd`) in `cmd/vmake/helpers.go`

## Runtime Execution Flow
```
Phase 1: OnRequire       -> Scan/Compile/Load buildscripts -> Resolve dependencies
Phase 2a: ResolveDeferred -> Resolve remote (deferred) packages -> Update topological order
Phase 2b: OnConfig       -> Execute callbacks -> Collect Options -> Merge global options
Phase 3: OnBuild         -> Execute callbacks -> Generate Targets -> Compile/Link
(Optional) Install       -> Install targets to prefix directory
```

Pipeline: `cmd/vmake/root.go:runPipeline()`

## Conditional Expressions
```go
ctx.Target("app").
    AddDefines(ctx.If("debug", "DEBUG=1")...).
    AddCFlags(ctx.Select("optimization", map[string]string{"O0": "-O0", "O2": "-O2"}))
```

## Key Types
```go
type TargetKind string
const (TargetBinary, TargetStatic, TargetShared, TargetObject, TargetVoid)

type OptionType int
const (OptionBool, OptionString, OptionInt, OptionChoice)

type SourceOrigin int
const (SourceLocal, SourceRemote)
```

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

## Target Dependencies
- Same-package: `AddDeps("utils")`
- Cross-package: `AddDeps("lib:utils")` using `package:target` format
- Third-party: `AddDeps("official/zlib")` (declared via `OnRequire` + `AddRequires`)

## Package Repositories

Two ecosystem types coexist:

| | Index Repo | Prefix Repo |
|--|--|--|
| **Purpose** | Wrap third-party C/C++ libs | VMake-native packages, cross-project sharing |
| **build.go role** | Wrapper (calls CMake etc.) | True build descriptor (identical to local) |
| **Source location** | build.go in repo, source elsewhere | build.go IS in the package git repo root |
| **Version source** | `AddVersion()` manual mapping | git tags (auto-filtered for semver) |
| **Version selection** | Phase 3 (after build.go compiled) | Phase 1 (before build.go — clones first) |
| **Add command** | `vmake repo add name url` | `vmake repo add --prefix name "https://..../{name}.git"` |
| **Update** | `vmake repo update name` | `vmake pkg update repo/name` |

Index repos are checked first; prefix repos are fallback. Prefix build.go must NOT use `SetGit`/`AddVersion` — system handles automatically. Auto-fetch picks up new remote tags on cached repos.

## Extension System

Extensions are Go plugins that add CLI commands. Directory: `~/.vmake/extensions/<repo-name>/<plugin-name>/`

```go
package main
import "gitee.com/spock2300/vmake/pkg/plugin"
func Main(ctx *plugin.Context) {
    ctx.AddSubCommand(&cobra.Command{Use: "mycommand", Run: runMyCommand})
}
```

**Plugin Context** (`pkg/plugin/api.go`): `VMakeDir`, `PluginDir`, `CommandName`, `AddSubCommand`, `RegisterToolchain`, `GetToolchains`, `SetOnMissing`, `AddGlobalFlags`, `DownloadFile`, `ExtractArchive`, `RunGitLFS`

## What Not To Do
- IDE integration plugins
- Remote builds / distributed compilation
- MSVC toolchain (not yet supported)
