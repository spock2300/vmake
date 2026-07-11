# VMake

VMake is a modern C/C++ project build tool developed in Go. It provides a concise and powerful API for configuring and building multi-module C/C++ projects.

## Features

- **Simple API Design**: Declarative build configuration via Fluent API
- **Flexible Option System**: Supports configuration options of boolean, string, integer, and enum types
- **Conditional Build Support**: Enables conditional compilation through methods like `If` and `When`
- **Multi-Module Support**: Native support for managing builds of multi-module projects
- **Third-Party Package Management**: Supports Registry (wrapping CMake/Autotools) and Native (vmake native packages) repository types, declare dependencies via OnRequire, automatic download, version matching, and build
- **Extension Plugin System**: CLI command extensions and cross-compilation toolchain management
- **Incremental Compilation**: Intelligent incremental compilation based on dependency analysis
- **TUI Configuration Interface**: Interactive terminal user interface for project configuration
- **Toolchain Management**: Flexible switching between multiple compiler toolchains, supports cross-compilation
- **Semantic Versioning**: Built-in semver parsing and constraint matching
- **Symbol Management**: Five-layer defense (`SetDefaultVisibilityHidden` + `SetVersionScript` + `SetExcludeLibs` + `SetSymbolBinding` + `vmake check-symbols`) controls exported symbols to prevent conflicts and leaks in complex dependency graphs

## Quick Start

### Installation

```bash
go install github.com/spock2300/vmake/cmd/vmake@latest
```

### Debug Mode

Buildscripts are interpreted by yaegi directly — no plugin compilation needed:

```bash
cd /path/to/vmake
go build -o vmake ./cmd/vmake
./vmake build
```

### Basic Usage

Create a `build.go` file:

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("debug").
            SetType(api.OptionBool).
            SetDefault(true).
            SetDescription("Enable debug mode")
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").
            SetKind(api.TargetBinary).
            AddFiles("src/main.c").
            AddDefines(ctx.If("debug", "DEBUG")...)
    })
}
```

Run the build:

```bash
vmake build
```

## Project Structure

```
vmake/
├── cmd/vmake/           # CLI command entry
├── pkg/
│   ├── api/             # Core build API (importable from build scripts)
│   ├── plugin/          # Extension plugin system (importable from plugins)
│   ├── build/           # Compilation, linking, and cache management
│   ├── buildscript/     # Build script scan, compile, load
│   ├── config/          # Configuration storage
│   ├── resolver/        # Dependency resolution
│   ├── repo/            # Package repository management
│   ├── toolchain/       # Toolchain management
│   ├── log/             # Logging
│   ├── tui/             # Terminal user interface
│   └── version/         # Version info
├── internal/
│   ├── exec/            # Command execution
│   ├── flock/           # File locking (cross-project sync)
│   ├── fs/              # Filesystem utilities
│   ├── gitstore/        # Git repo store (shared infra)
│   ├── glob/            # File matching
│   ├── gocompile/       # Go plugin compilation (extension system only)
│   ├── jsonio/          # JSON serialization
│   └── toposort/        # Topological sort
└── docs/                # Design documentation
```

## Package Repository

VMake supports two types of package repositories:

**Registry Repository**: Wraps third-party C/C++ libraries (such as zlib, curl). `build.go` acts as a wrapper that calls CMake/Autotools to build source code. Versions are manually mapped via `AddVersion()`.

**Native Repository**: VMake native packages, for sharing across projects. Each package is an independent Git repository, with `build.go` at the repository root. Versions are automatically recognized via git tags.

| | Registry Repository | Native Repository |
|--|--|--|
| **Purpose** | Wrapping third-party C/C++ libraries | VMake native packages, cross-project sharing |
| **build.go** | Wrapper (calls CMake, etc.) | Actual build description |
| **Version Source** | `AddVersion()` manual mapping | git tag (auto-detected semver) |
| **Add Command** | `vmake repo add name url` | `vmake repo add --native name "https://..../{name}.git"` |

### Usage Flow

1. Add a repository:

```bash
vmake repo add official https://github.com/user/vmake-packages    # Registry
vmake repo add --native myorg https://git.example.com/{name}.git   # Native
```

2. Declare dependencies in `build.go`:

```go
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("official/zlib >=1.2")
})
```

3. Use in a Target:

```go
ctx.Target("app").
    SetKind(api.TargetBinary).
    AddFiles("src/*.c").
    AddDeps("official/zlib")
```

## API Overview

### Option Types

| Type | Description |
|------|-------------|
| `OptionBool` | Boolean type |
| `OptionString` | String type |
| `OptionInt` | Integer type |
| `OptionChoice` | Enum type |

### Target Types

| Type | Description |
|------|-------------|
| `TargetBinary` | Executable |
| `TargetStatic` | Static library |
| `TargetShared` | Shared library |
| `TargetObject` | Object file |
| `TargetVoid` | Third-party package build (with `SetBuildFunc`) |

### Core Methods

```go
// Configure options
ctx.Option(name string) *Option
ctx.Bool(name string) bool
ctx.String(name string) string
ctx.Int(name string) int

// Conditional evaluation
ctx.If(option string, then ...string) []string
ctx.IfNot(option string, then ...string) []string
ctx.When(option string, value any) bool
ctx.Select(option string, mapping map[string]string) string

// Target configuration
ctx.Target(name string) *Target
```

## Extension Plugins

Extension plugins extend vmake's CLI commands and toolchain management through Go plugins (`.so`). Each extension repository is a Git repository, where each subdirectory (containing a `plugin.json`) at the repository root is an independent plugin.

### Capabilities

- **CLI Command Extension**: Add custom subcommands via `AddSubCommand`
- **Toolchain Management**: Register custom toolchains with auto-download on first use via `toolchain.json` + `tc` plugin (Git LFS or HTTP)
- **Global Build/Link Flags**: Inject C/CXX/linker flags into all builds via `AddGlobalCFlags`, `AddGlobalCxxFlags`, and `AddGlobalLdFlags`. Pass to CMake external builds via `CMakeGlobalFlagsArgs()` or `MergedCFlags()`

### Usage Flow

1. Add an extension repository:

```bash
vmake ext add <name> <git-url>
```

2. Plugins are auto-discovered and compiled on the next run. Restart vmake to use new commands.

See the [Extension Plugin Guide](docs/EXTENSION_PLUGIN.md) for the complete plugin authoring tutorial, all interface references, and practical examples.

## Command-Line Usage

### Build Commands

```bash
vmake build [-f|--force] [--toolchain <name>] [--mode <mode>] [-i|--install] [-p|--prefix <dir>] [--install-type <type>] [--manifest <file>]
vmake clean [--all]
vmake rebuild
```

### Configuration Commands

```bash
vmake config    # Interactive TUI configuration
```

### Toolchain Management

```bash
vmake toolchain list
vmake toolchain show [name]
```

### Package Repository Management

```bash
vmake repo add <name> <url>                # Registry repo
vmake repo add --native <name> <url>       # Native repo (URL template with {name})
vmake repo remove <name>
vmake repo list
vmake repo update <name>
```

### Package Management

```bash
vmake pkg list
vmake pkg search <keyword>
vmake pkg clean <repo/name> [-a]
vmake pkg update <repo/name>
```

### Extension Management

```bash
vmake ext add <name> <url>
vmake ext remove <name>
vmake ext list
vmake ext update [name]
```

### Other Commands

```bash
vmake git tag [version] [--major|--minor|--patch]    # Version tagging
vmake update [version]                                # Self-update
vmake version                                         # Version info
vmake skill install                                   # Install AI skill
vmake skill uninstall                                 # Uninstall AI skill
vmake skill path                                      # Show skill paths
```

Global flags: `-v` (verbose), `-V` (very verbose), `-q` (quiet)

## Documentation

Detailed design documents are available in the [docs](docs/) directory:

- [Build Script API](docs/BUILD_SCRIPT_API.md) - Build script and third-party package API
- [Extension Plugin Guide](docs/EXTENSION_PLUGIN.md) - CLI extension and toolchain repository authoring
- [Architecture](docs/ARCHITECTURE.md) - System architecture and execution flow
- [Directory Structure](docs/VMAKE_HOME.md) - ~/.vmake directory structure
- [AI Install Guide](docs/AI_INSTALL_GUIDE.md) - AI assistant skill installation
- [Firmware Build Design](docs/FIRMWARE_BUILD_DESIGN.md) - Firmware build system design

## Test Cases

| Directory | Description |
|-----------|-------------|
| `test_data/01_simple_c` | Simple C project |
| `test_data/02_with_config` | Project with configuration options |
| `test_data/03_multi_target` | Multi-target project |
| `test_data/04_multi_module` | Multi-module project |
| `test_data/05_conditional` | Conditional compilation project |
| `test_data/06_complete_api` | Complete API test |
| `test_data/07_subbuild_codegen` | Sub-build / code generation |
| `test_data/08_with_package` | Third-party package dependency |
| `test_data/09_with_curl` | Package requiring curl download |
| `test_data/10_local_repo` | Local repository test |
| `test_data/11_with_tinyexpr` | Package with tinyexpr dependency |
| `test_data/12_rtos_simulate` | RTOS simulation project |
| `test_data/13_with_prefix_repo` | Native repository dependencies |
| `test_data/14_bin_header` | Binary header embedding |
| `test_data/15_subgraph_siblings` | Subgraph sibling targets build (host codegen tool + library) |
| `test_data/16_subgraph_cross_tc` | Subgraph build with cross-toolchain |
| `test_data/18_config_header` | Generated config header (GenerateConfigHeader) |
| `test_data/19_config_defines` | Generated config defines (GenerateConfigDefines) |
| `test_data/20_config_propagate` | Cross-package config propagation (ImportConfig) |
| `test_linux/17_firmware` | Full firmware build (Linux, U-Boot, BusyBox, App, RootFS, Firmware) |

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

## Contact

- Project URL: https://github.com/spock2300/vmake
