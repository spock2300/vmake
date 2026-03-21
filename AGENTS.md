# VMake - AGENTS.md

AI coding agents working in the VMake codebase should follow these guidelines.

## Build / Lint / Test Commands

```bash
# Build
go build -o vmake ./cmd/vmake

# Lint
go vet ./...
gofmt -w .
```

**Testing**: This project has no Go unit tests. Test using integration test projects in `test_data/`:

```bash
# Single integration test (run from repo root)
./vmake build                          # build current project
cd test_data/01_simple_c && ../../vmake build && ./hello
cd test_data/08_with_package && ../../vmake build
cd test_data/09_with_curl && ../../vmake build

# All integration tests
cd test_data/01_simple_c && ../../vmake build
cd test_data/02_with_config && ../../vmake build
cd test_data/03_multi_target && ../../vmake build
cd test_data/04_multi_module && ../../vmake build
cd test_data/08_with_package && ../../vmake build

# Debug third-party plugin development (set to your repo path)
export VMAKE_DIR=/path/to/vmake
cd test_data/01_simple_c && ../../vmake build
```

## Core Philosophy

**Less is More** - Before adding any new feature or API, ask:
- Is this absolutely necessary?
- Can 80% of use cases be achieved with 20% of the functionality?

## Code Style

### No Comments
Never add comments unless explicitly requested. Code should be self-explanatory.

### Imports Ordering
Group imports in three sections separated by blank lines: standard library -> external packages -> local packages:
```go
import (
    "context"
    "fmt"
    "path/filepath"

    "github.com/spf13/cobra"

    "gitee.com/spock2300/vmake/pkg/api"
)
```

### Naming Conventions
- **SetXxx**: Set a single value (SetKind, SetDefault)
- **AddXxx**: Append multiple values (AddFiles, AddIncludes)
- **Type aliases**: Use type aliases for readability (e.g., `type TargetKind string`)
- **Logging**: Always use alias `vlog "gitee.com/spock2300/vmake/pkg/log"`

### Fluent API
All public APIs use method chaining - return `*Target`, `*Package`, `*Option` from setter methods:
```go
ctx.Target("app").
    SetKind(api.TargetBinary).
    AddFiles("src/*.c").
    AddIncludes("include")
```

### Error Handling
- Library code never panics, always return error
- Include context in error messages: `return fmt.Errorf("git clone %s -> %s: %w", url, dir, err)`
- Use `%w` for error wrapping
- CLI commands may call `os.Exit(1)` on fatal errors

### Cross-Platform Paths
Use `filepath.Join()` for file system paths. Do NOT use for logical identifiers:
- Package identifiers: `repo/name` (string concatenation)
- Target identifiers: `pkg:target` (use `:` as delimiter)

## Package Structure

| Package | Responsibility | Plugin Importable |
|---------|---------------|-------------------|
| `pkg/api` | Core API (Builder, Target, Option, Package) | **Yes** |
| `pkg/plugin` | Plugin scan, compile, load | No |
| `pkg/config` | Config storage | No |
| `pkg/build` | Build execution, compile, link | No |
| `pkg/toolchain` | Toolchain abstraction (GCC, Clang) | No |
| `pkg/repo` | Package management, Git, dependency resolution | No |
| `pkg/log` | Logging (use `vlog "gitee.com/spock2300/vmake/pkg/log"`) | No |
| `pkg/tui` | TUI components (bubbletea) | No |
| `pkg/version` | Version info | No |
| `internal/exec` | OS command execution | No |
| `internal/glob` | Glob pattern matching | No |

**Principle**: `pkg/api` is the only public API that must remain stable. Other packages are internal and may change.

## CLI Architecture
- CLI uses `github.com/spf13/cobra`
- Commands defined in `cmd/vmake/` as package-level vars (e.g., `var buildCmd = &cobra.Command{...}`)
- Register commands in `init()` via `RootCmd.AddCommand()`
- Flags bound with `Flags().BoolVarP/StringVarP` etc.
- CLI may call `os.Exit(1)` for fatal errors

## Runtime Execution Flow
```
Phase 1: OnRequire
    Scan build.go -> Compile plugins -> Load plugins -> Collect dependencies
    |
Phase 2: OnConfig
    Execute OnConfig callbacks -> Collect Option definitions -> Load saved config
    |
Phase 3: OnBuild
    Execute OnBuild callbacks -> Generate Targets -> Compile/Link
```

## Conditional Expressions
Use functional conditionals instead of if statements:
```go
ctx.Target("app").
    AddDefines(ctx.If("debug", "DEBUG=1")).
    AddLinks(ctx.If("ssl", "ssl", "crypto")).
    AddCFlags(ctx.Select("optimization", map[string]string{
        "O0": "-O0",
        "O2": "-O2",
    }))
```

## Key Types
```go
type TargetKind string
const (
    TargetBinary TargetKind = "binary"
    TargetStatic TargetKind = "static"
    TargetShared TargetKind = "shared"
    TargetObject TargetKind = "object"
    TargetVoid   TargetKind = "void"
)

type OptionType int
const (
    OptionBool OptionType = iota
    OptionString
    OptionInt
    OptionChoice
)
```

## Plugin System
Each `build.go` is compiled to a Go plugin (`.so`):
```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(b *api.Builder) {
    b.OnConfig(func(ctx *api.ConfigContext) { ... })
    b.OnBuild(func(ctx *api.BuildContext) { ... })
}
```
Plugin naming: Package name = directory name of `build.go` location.

## Target Dependencies
- Same-package: `AddDeps("utils")`
- Cross-package: `AddDeps("lib:utils")` using `package:target` format

## What Not To Do
- IDE integration plugins
- Remote builds
- Distributed compilation
- MSVC toolchain (not yet supported)
- Add new data structures without necessity

## References
- Storage Structure: `docs/VMAKE_HOME.md`
- Architecture: `docs/ARCHITECTURE.md`
- Plugin API: `docs/PLUGIN_API.md`
- Test projects: `test_data/01_simple_c` through `test_data/10_local_repo`