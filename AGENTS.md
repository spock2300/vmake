# VMake - AGENTS.md

AI coding agents working in the VMake codebase should follow these guidelines.

## Build / Lint / Test Commands

```bash
go build -o vmake ./cmd/vmake

gofmt -w .

cd test_data/01_simple_c && ../../vmake build && ./hello
cd test_data/02_with_config && ../../vmake build
cd test_data/03_multi_target && ../../vmake build
cd test_data/04_multi_module && ../../vmake build
cd test_data/08_with_package && ../../vmake build

./vmake clean && ./vmake build
```

For plugin development, set `VMAKE_DIR` to match plugin versions:

```bash
export VMAKE_DIR=/home/spock/git/vmake
cd test_data/01_simple_c && ../../vmake build
```

## Core Philosophy

**Less is More** - Before adding any new feature or API, ask:
- Is this absolutely necessary?
- Can 80% of use cases be achieved with 20% of the functionality?
- Is there a simpler way to achieve the goal?

## Code Style

### No Comments

**Never add comments** unless explicitly requested by the user. Code should be self-explanatory.

### Imports Ordering

Group imports in three sections, separated by blank lines:

```go
import (
    "context"
    "fmt"
    "os/exec"
    "path/filepath"

    "github.com/spf13/cobra"

    "gitee.com/spock2300/vmake/pkg/api"
    "gitee.com/spock2300/vmake/pkg/plugin"
)
```

Order: standard library → external packages → local packages

### Naming Conventions

- **SetXxx**: Set a single value (SetKind, SetDefault, SetLanguages)
- **AddXxx**: Add multiple values (AddFiles, AddIncludes, AddDefines)
- **Type aliases**: Use type aliases for readability
  ```go
  type TargetKind string
  type OptionType int
  type ConfigFunc func(ctx *ConfigContext)
  ```

### Fluent API

All APIs use method chaining:

```go
ctx.Target("app").
    SetKind(api.TargetBinary).
    AddFiles("src/*.c").
    AddIncludes("include")
```

### Error Handling

- Library code never panics, always return error
- Include context in error messages:
  ```go
  return fmt.Errorf("git clone %s -> %s: %w", url, dir, err)
  return fmt.Errorf("failed to find package %s: %w", name, err)
  return fmt.Errorf("compile %s failed: %w", name, cr.Error)
  ```
- Use `%w` for error wrapping to support `errors.Is` / `errors.As`

### Cross-Platform Paths

Use `filepath.Join()` for all file system paths:

```go
buildDir := filepath.Join(pkg.Dir, "build")
pluginPath := filepath.Join(buildDir, "plugin.so")
```

Do NOT use `filepath.Join()` for logical identifiers:
- Package identifiers: `repo/name` (use string concatenation)
- Target identifiers: `pkg:target` (use `:` as delimiter)

## Package Structure

| Package | Responsibility | Plugin Importable |
|---------|---------------|-------------------|
| `pkg/api` | Core API (Builder, Target, Option, Package) | **Yes** |
| `pkg/plugin` | Plugin scan, compile, load | No |
| `pkg/config` | Config storage | No |
| `pkg/tui` | Terminal UI | No |
| `pkg/build` | Build execution, compile, link | No |
| `pkg/toolchain` | Toolchain abstraction (GCC, Clang) | No |
| `pkg/repo` | Package management, Git, dependency resolution | No |
| `pkg/log` | Logging system | No |
| `internal/*` | Internal implementation details | No |

**Principle**: `pkg/api` is the only public API that must remain stable.

## Runtime Execution Flow

Three-phase architecture:

```
Phase 1: OnRequire
    Scan build.go → Compile plugins → Load plugins → Collect dependencies
    ↓
Phase 2: OnConfig  
    Execute OnConfig callbacks → Collect Option definitions → Load saved config
    ↓
Phase 3: OnBuild (or vmake config for TUI)
    Execute OnBuild callbacks → Generate Targets → Compile/Link
```

### Dependency Resolution

```
ResolveWithLocal → resolveLocal → Compile plugin → Load plugin → 
    GetRequires → resolveRecursive → FindPackageGo → Load package definition
```

### Config Storage

- Local package options → `config.Packages[pkgName].options`
- Third-party packages (name contains `/`) → `config.Requires[pkgName]`
- Global options → `config.Global.Options`

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

## Official Repository

`official_repo/` is a **separate git repository** for indexing third-party packages.
- Path format: `{repo_name}/{package_name}` (e.g., `official/zlib`)
- Package definitions: `official_repo/packages/{first_letter}/{package_name}/package.go`

## Target Dependencies

- Same-package: `AddDeps("utils")`
- Cross-package: `AddDeps("lib:utils")` using `package:target` format

## Key Types

```go
type TargetKind string
const (
    TargetBinary TargetKind = "binary"
    TargetStatic TargetKind = "static"
    TargetShared TargetKind = "shared"
    TargetObject TargetKind = "object"
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

## What Not To Do

- IDE integration plugins
- Remote builds
- Distributed compilation
- MSVC toolchain (not yet supported)
- Add new data structures without necessity

## References

- API Design: `docs/API_DESIGN.md`
- Test projects: `test_data/01_simple_c` through `test_data/10_local_repo`
