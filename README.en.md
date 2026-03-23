# VMake

VMake is a modern C/C++ project build tool developed in Go. It provides a concise and powerful API for configuring and building multi-module C/C++ projects.

## Features

- **Simple API Design**: Only 11 core methods, enabling declarative build configuration via Fluent API
- **Flexible Option System**: Supports configuration options of boolean, string, integer, and enum types
- **Conditional Build Support**: Enables conditional compilation through methods like `If` and `When`
- **Multi-Module Support**: Native support for managing builds of multi-module projects
- **Incremental Compilation**: Intelligent incremental compilation based on dependency analysis, significantly improving build efficiency
- **TUI Configuration Interface**: Provides an interactive terminal user interface for easy project configuration
- **Toolchain Management**: Supports flexible switching between multiple compiler toolchains
- **Cache Management**: Automatically manages build cache to enable incremental compilation decisions

## Quick Start

### Installation

```bash
go install gitee.com/spock2300/vmake/cmd/vmake@latest
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
│   ├── build/           # Compilation, linking, and cache management
│   ├── config/          # Configuration storage
│   ├── plugin/          # Plugin loader
│   ├── resolver/        # Dependency resolution
│   ├── repo/            # Package repository management
│   ├── toolchain/       # Toolchain management
│   ├── log/             # Logging
│   └── tui/             # Terminal user interface
├── internal/
│   ├── exec/            # Command execution
│   ├── glob/            # File matching
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

// Target configuration
ctx.Target(name string) *Target
```

## Configuration Options

### Project-Level Options

Define in `build.go` via the `OnConfig` function:

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Option("optimization").
        SetType(api.OptionChoice).
        SetDefault("O2").
        SetValues("O0", "O1", "O2", "O3").
        SetDescription("Compiler optimization level")
})
```

### Global Options

VMake includes the following built-in global options:

- `mode`: Build mode (`debug` / `release`)
- `toolchain`: Compiler toolchain

### Conditional Expressions

```go
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").
        AddDefines(ctx.If("mode", "DEBUG")...).
        AddCFlags(ctx.If("optimization", "O3", "-DNDEBUG")...)
})
```

## Multi-Module Projects

```
myproject/
├── build.go              # Root module configuration
├── app/
│   ├── build.go          # App module configuration
│   └── src/
├── lib/
│   ├── build.go          # Lib module configuration
│   └── src/
└── include/
```

### Root Module build.go

```go
func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        // Global options
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").
            SetKind(api.TargetBinary).
            AddFiles("app/src/main.c").
            AddDeps("lib")
    })
}
```

### Submodule build.go

```go
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("mylib").
            SetKind(api.TargetStatic).
            AddFiles("src/*.c").
            AddIncludes("include")
    })
}
```

## Command-Line Usage

### Build Commands

```bash
# Build current module
vmake build

# Verbose output
vmake build -v

# Very verbose output
vmake build -V
```

### Configuration Commands

```bash
# Enter interactive configuration interface
vmake config
```

### Toolchain Management

```bash
# List available toolchains
vmake toolchain list

# Show current toolchain info
vmake toolchain show

# Initialize new toolchain
vmake toolchain init <name>
```

### Cleanup Commands

```bash
# Clean build artifacts for current module
vmake clean

# Clean build artifacts for all modules
vmake clean --all

# Fully rebuild
vmake rebuild
```

## Configuration Files

### Project Configuration (`.vmake/config.json`)

```json
{
  "version": "1",
  "global": {
    "toolchain": "gcc",
    "mode": "debug",
    "options": { "ssl": true }
  },
  "entries": {
    "myproject": {
      "options": { "verbose": false }
    }
  }
}
```

### Global Configuration (`~/.vmake/config.json`)

```json
{
  "version": "1",
  "default_toolchain": "gcc",
  "toolchains": {
    "gcc": {
      "name": "gcc",
      "display_name": "System GCC",
      "host": "x86_64-linux-gnu",
      "tools": {
        "cc": "gcc", "cxx": "g++", "ar": "ar",
        "ld": "ld", "strip": "strip", "ranlib": "ranlib"
      },
      "default_flags": {
        "cflags": ["-O2", "-Wall"],
        "cxxflags": ["-O2", "-Wall", "-Wextra"],
        "ldflags": ["-Wl,--as-needed"]
      }
    }
  }
}
```

## Advanced Features

### Conditional Compilation

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Option("platform").
        SetType(api.OptionChoice).
        SetValues("windows", "linux", "macos")
    
    ctx.Option("features.encryption").
        SetType(api.OptionBool).
        SetDefault(false)
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").
        AddFiles("src/main.c").
        AddFiles(ctx.If("platform", "windows", "src/win32.c")...).
        AddDefines(ctx.If("features.encryption", "ENABLE_ENCRYPTION")...)
})
```

### Custom Compilation Flags

```go
ctx.Target("app").
    AddFiles("src/*.c").
    AddIncludes("include", "thirdparty/include").
    AddPublicIncludes("include").
    AddDefines("VERSION=1.0").
    AddLinks("pthread", "dl").
    AddCFlags("-Wall", "-Wextra").
    AddCxxFlags("-std=c++17").
    AddLdFlags("-Wl,--gc-sections")
```

## Documentation

Detailed design documents are available in the [docs](docs/) directory:

- [Plugin API](docs/PLUGIN_API.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Directory Structure](docs/VMAKE_HOME.md)

## Test Cases

The project includes the following test scenarios:

| Directory | Description |
|-----------|-------------|
| `test_data/01_simple_c` | Simple C project |
| `test_data/02_with_config` | Project with configuration options |
| `test_data/03_multi_target` | Multi-target project |
| `test_data/04_multi_module` | Multi-module project |
| `test_data/05_conditional` | Conditional compilation project |
| `test_data/06_complete_api` | Complete API test |

Run tests:

```bash
vmake build
```

## Architecture Design

### Core Components

1. **Package**: Plugin entry point for registering configuration and build callbacks
2. **ConfigContext**: Configuration context managing option definitions and values
3. **BuildContext**: Build context managing targets and build logic
4. **BuildGraph**: Build dependency graph analyzing relationships between targets
5. **Scheduler**: Scheduler coordinating compilation and linking tasks
6. **Compiler**: Compiler handling source file compilation
7. **Linker**: Linker handling object file linking

### Execution Flow

```
1. Scan project structure and collect Packages
2. Load and compile plugins
3. Execute OnConfig callbacks to collect option definitions
4. Load saved configuration values
5. (Optional) Launch TUI for interactive configuration
6. Execute OnBuild callbacks to build Target dependency graph
7. Schedule compilation tasks
8. Execute linking tasks
9. Save configuration and cache
```

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

## Contribution Guidelines

Issues and pull requests are welcome!

## Contact

- Project URL: https://gitee.com/spock2300/vmake