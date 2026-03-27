# VMake

VMake is a modern C/C++ project build tool developed in Go. It provides a concise and powerful API for configuring and building multi-module C/C++ projects.

## Features

- **Simple API Design**: Declarative build configuration via Fluent API
- **Flexible Option System**: Supports configuration options of boolean, string, integer, and enum types
- **Conditional Build Support**: Enables conditional compilation through methods like `If` and `When`
- **Multi-Module Support**: Native support for managing builds of multi-module projects
- **Third-Party Package Management**: Git-based package dependencies with automatic download and build
- **Extension Plugin System**: CLI command extensions and cross-compilation toolchain management
- **Incremental Compilation**: Intelligent incremental compilation based on dependency analysis
- **TUI Configuration Interface**: Interactive terminal user interface for project configuration
- **Toolchain Management**: Flexible switching between multiple compiler toolchains
- **Semantic Versioning**: Built-in semver parsing and constraint matching

## Quick Start

### Installation

```bash
go install gitee.com/spock2300/vmake/cmd/vmake@latest
```

### Debug Mode

Set `VMAKE_DIR` to point to local source when developing vmake or debugging `build.go`:

```bash
export VMAKE_DIR=/path/to/vmake

cd /path/to/vmake
go build -o vmake ./cmd/vmake
./vmake build
```

### Basic Usage

Create a `build.go` file:

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

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
│   ├── api/             # Core build API
│   ├── plugin/          # Plugin loader
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
│   ├── fs/              # Filesystem utilities
│   ├── gitstore/        # Git repo store (shared infra)
│   ├── glob/            # File matching
│   ├── gocompile/       # Go plugin compilation
│   └── jsonio/          # JSON serialization
└── docs/                # Design documentation
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

## Command-Line Usage

### Build Commands

```bash
vmake build [-f|--force] [--toolchain <name>] [--mode <mode>] [-i|--install] [-p|--prefix <dir>]
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
vmake toolchain init <name>
```

### Package Repository Management

```bash
vmake repo add <name> <url>
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
vmake doc                                             # AI documentation
```

Global flags: `-v` (verbose), `-V` (very verbose), `-q` (quiet)

## Documentation

Detailed design documents are available in the [docs](docs/) directory:

- [Plugin API](docs/PLUGIN_API.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Directory Structure](docs/VMAKE_HOME.md)

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

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

## Contact

- Project URL: https://gitee.com/spock2300/vmake
