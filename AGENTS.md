# VMake - AGENTS.md

AI coding agents working in the VMake codebase should follow these guidelines.

## Build / Lint / Test Commands

```bash
go build -o vmake ./cmd/vmake    # Build
go vet ./... && gofmt -w .       # Lint
```

**Testing**: No Go unit tests. Test via integration projects in `test_data/`:

```bash
# Single integration test (run from repo root)
cd test_data/01_simple_c && ../../vmake build && ./hello

# Test with third-party package
cd test_data/08_with_package && ../../vmake build

# Run all integration tests
for d in test_data/0[1-9]_*/; do (cd "$d" && ../../vmake build) || break; done
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

## Package Structure

| Package | Responsibility | Plugin Importable |
|---------|---------------|-------------------|
| `pkg/api` | Core API (Package, Target, Option, Contexts, Semver) | **Yes** |
| `pkg/plugin` | Extension/plugin system for custom commands | **Yes** |
| `pkg/buildscript` | Build script scan, compile, load | No |
| `pkg/config` | Config storage | No |
| `pkg/build` | Build execution, compile, link, scheduler, install | No |
| `pkg/toolchain` | Toolchain abstraction (GCC, Clang) | No |
| `pkg/repo` | Package management, Git, source download | No |
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

**Principle**: `pkg/api` and `pkg/plugin` are public APIs that must remain stable.

**Dependency DAG**: `internal/*` → `pkg/toolchain` → `pkg/api` → `pkg/buildscript, pkg/repo` → `pkg/resolver, pkg/plugin, pkg/build` → `cmd/vmake`

**Key embedding**:
- `RepoManager` and `plugin.Manager` embed `*gitstore.Store` for git repo CRUD
- `buildscript.CompileResult` and `plugin.CompileResult` embed `gocompile.CompileResult`
- `Package`, `ConfigContext`, `BuildContext`, `InstallContext` embed `ConfigAccessor`

## CLI Architecture
- CLI uses `github.com/spf13/cobra`
- Commands in `cmd/vmake/` as package-level vars, registered in `init()`
- Flags bound with `Flags().BoolVarP/StringVarP`
- Command factories (`newAddCmd`, `newRemoveCmd`, `newUpdateCmd`) in `cmd/vmake/helpers.go` for repo/ext commands

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
Use functional conditionals instead of if statements:
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

## Package Reference Parsing
```go
repo, name, ok := api.SplitPackageRef("official/zlib")
```

## Extension System

Extensions are Go plugins that add CLI commands. Directory: `~/.vmake/extensions/<repo-name>/<plugin-name>/`

```go
package main
import "gitee.com/spock2300/vmake/pkg/plugin"
func Main(ctx *plugin.Context) {
    ctx.AddSubCommand(&cobra.Command{Use: "mycommand", Run: runMyCommand})
}
```

**Plugin Context** (`pkg/plugin/api.go`): `VMakeDir`, `PluginDir`, `CommandName`, `AddSubCommand`, `RegisterToolchain`, `GetToolchains`, `SetOnMissing`, `DownloadFile`, `ExtractArchive`, `RunGitLFS`

**Commands**: `vmake ext add|remove|list|update`

## AI Skill System

VMake provides an AI skill optimized for coding assistants (Claude Code, OpenCode, Cursor, etc.). The skill helps AI assistants understand VMake build configuration.

**Installation**:
```bash
vmake skill install           # Install to ~/.claude/skills/vmake/ and ~/.agents/skills/vmake/
vmake skill install --project # Also install to project-level .claude/skills/
```

**Commands**:
- `vmake skill install` - Install the VMake skill
- `vmake skill uninstall` - Remove installed skill
- `vmake skill path` - Show installation paths

**Skill content**:
- `SKILL.md` - Core guide (~180 lines): lifecycle, target, option, conditional, packages, CLI quick ref
- `references/api.md` - Full API reference (from source code)
- `references/cli.md` - CLI command tree (auto-generated from cobra)
- `examples/*.md` - 7 annotated build.go examples

**Updating**:
```bash
vmake update && vmake skill install  # Update vmake then reinstall skill
```

## TUI Styling (`pkg/tui/styles.go`)

Uses `lipgloss`. Color palette:
- Purple `#7D56F4`: Title, selection cursor, selected items
- Cyan `#00D9FF`: Choice dropdown values
- Pink `#F25D94`: Input fields, expanded tree nodes
- Green `#04B575`: Option names, checkboxes (checked)
- Gray `#626262`: Help text, descriptions
- Red `#FF6B6B`: Confirmation prompts, modified values

## What Not To Do
- IDE integration plugins
- Remote builds / distributed compilation
- MSVC toolchain (not yet supported)

## References
- `docs/VMAKE_HOME.md` - Storage Structure
- `docs/ARCHITECTURE.md` - Architecture
- `docs/PLUGIN_API.md` - Plugin API
- `test_data/01_simple_c` through `test_data/11_with_tinyexpr` - Test projects
