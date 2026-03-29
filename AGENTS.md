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

# Run all tests (01-13)
for d in test_data/0[1-9]_*/ test_data/1[0-9]_*/; do (cd "$d" && ../../vmake build) || break; done
```

Known pre-existing failures (ignore): `09_with_curl` (mbedtls submodule), `10_local_repo` (package not found).

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
- `vlog.Fatal()` in CLI/builder code is acceptable; in `pkg/api` context helpers it exists for user-facing errors

### Cross-Platform Paths
Use `filepath.Join()` for filesystem paths. Do NOT use for logical identifiers:
- Package identifiers: `repo/name` (string concatenation)
- Target identifiers: `pkg:target` (`:` as delimiter)

### Code Organization
- Struct fields are private; access via getter/setter methods
- Exceptions with public fields: `InstalledPackage`, `PackageMeta`, config types (`Toolchain`, `Tools`), resolver types (`PackageNode`, `Graph`)
- Within a file: setters first, then getters, then remove methods, then private helpers
- Struct fields ordered by logical grouping, not alphabetically
- Embedded types at the top of the struct definition

### No Interfaces
The codebase uses function type aliases (`type ConfigFunc func(...)`) and struct-embedded function fields instead of traditional Go interfaces.

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

### Build Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--force` | `-f` | Force buildscript recompilation |
| `--toolchain` | | Override toolchain |
| `--mode` | | Override build mode (debug/release) |
| `--install` | `-i` | Install after build |
| `--prefix` | `-p` | Installation prefix (default: `./install/`) |
| `--install-type` | | Install type: `runtime` (default) or `sdk` |

### Install Type Filtering

`--install-type` controls what gets installed:

| File type | runtime | sdk |
|---------|---------|-----|
| binary → `bin/` | yes | yes |
| shared (.so) → `lib/` | yes | yes |
| static (.a) → `lib/` | no | yes |
| public includes → `include/` | no | yes |
| AddInstalls custom files | yes | yes |

### Install Manifest

`--install` generates `<prefix>/manifest.json` recording build metadata and per-package info:

```json
{
  "vmake": "0.x.x",
  "toolchain": "gcc",
  "mode": "debug",
  "generated": "2026-03-29T01:55:53Z",
  "packages": [
    {
      "name": "myapp",
      "version": "v1.0.0-2-g3a4b5c6",
      "source": "local",
      "ref": "3a4b5c6789abcdef...",
      "path": "."
    },
    {
      "name": "test_build/mathlib",
      "version": "2.1.0",
      "source": "prefix",
      "url": "https://gitee.com/.../test_build_mathlib.git",
      "ref": "v2.1.0"
    }
  ]
}
```

- Local: `source: "local"`, version from `git describe`, `ref` from `git rev-parse HEAD`, `path` relative to cwd
- Prefix: `source: "prefix"`, `url` from `PrefixGitURL`, `ref` from `PrefixVersions`
- Index: `source: "index"`, `url` from first `GitURLs()`, `ref` from `Versions()`

CLI: `vmake manifest show <path>` / `vmake manifest checkout <path> [name]`

## Runtime Execution Flow
```
Phase 1: OnRequire       -> Scan/Compile/Load buildscripts -> Resolve dependencies
Phase 2a: ResolveDeferred -> Resolve remote (deferred) packages -> Update topological order
Phase 2b: OnConfig       -> Execute callbacks -> Collect Options -> Merge global options
Phase 3: OnBuild         -> Execute callbacks -> Generate Targets -> Compile/Link
(Optional) Install       -> Install targets to prefix directory + generate manifest.json
```

Pipeline: `cmd/vmake/root.go:runPipeline()`

## Key Types
```go
type TargetKind string
const (TargetBinary, TargetStatic, TargetShared, TargetObject, TargetVoid)

type OptionType int
const (OptionBool, OptionString, OptionInt, OptionChoice)

type SourceOrigin int
const (SourceLocal, SourceRemote)

type PkgDirs struct { SourceDir, BuildDir, InstallDir string }
type PostLinkStep struct { Tool string; Args []string }
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

## RTOS / Embedded Support
```go
ctx.Target("firmware").
    SetKind(api.TargetBinary).
    AddFiles("src/*.c").
    AddBinHeader("assets/logo.bin", "assets/font.bin").
    SetLinkerScript("ld/stm32f4.ld").
    AddPostLinkSize().
    AddPostLinkHex()
```
- `SetLinkerScript(path)` — passes `-T` to linker
- `AddPostLink(tool, args...)` — generic post-link step, supports `{output}` placeholder
- Shorthands: `AddPostLinkHex()`, `AddPostLinkBin()`, `AddPostLinkSize()`, `AddPostLinkStrip()`
- `AddBinHeader(inputs ...)` — converts binary files to `.h` headers with hex data; output to `build/<tc>-<mode>/generated/`, include path auto-added; incremental via mtime
- RTOS tool accessors: `Package.ObjCopy()`, `Size()`, `ObjDump()`, `NM()`

## Git Patches
```go
p.AddPatches("patches/fix-build.patch", "patches/add-feature.patch")
```
Patches are applied via `git apply --3way` after source download, before build. Already-applied patches are skipped.

## Sub-Graph Build
```go
ctx.BuildSubGraph("codegen")    // Build package and its deps as independent sub-graph
path := ctx.DepOutput("codegen:codegen")  // Get output path of dependency target
```

Use `ToolchainOption()` to allow per-package toolchain switching for sub-graph builds (e.g., host toolchain for codegen, embedded toolchain for firmware):
```go
ctx.ToolchainOption()  // Auto-populates from registered toolchains, default "host"
```

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

Index repos are checked first; prefix repos are fallback. Prefix build.go must NOT use `SetGit`/`AddVersion` — system handles automatically.

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

## Known Gotchas
- `cmd/vmake/mainfest_cmd.go` — filename is misspelled ("mainfest" not "manifest"), do NOT rename
- `vlog` has `Debug`, `Info`, `Error`, `Fatal` — no `Warn` method
- `test_data/09_with_curl` and `test_data/10_local_repo` have pre-existing failures unrelated to code changes

## What Not To Do
- IDE integration plugins
- Remote builds / distributed compilation
- MSVC toolchain (not yet supported)
