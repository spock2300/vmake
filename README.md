# VMake

> [中文文档 🇨🇳](README.zh.md)

VMake is a modern C/C++ project build tool developed in Go. It provides a concise and powerful API for configuring and building multi-module C/C++ projects.

## Features

- **Simple API Design**: Declarative build configuration via Fluent API
- **Flexible Option System**: Supports configuration options of boolean, string, integer, and enum types
- **Conditional Build Support**: Enables conditional compilation through methods like `If` and `When`
- **Multi-Module Support**: Native support for managing builds of multi-module projects
- **Third-Party Package Management**: Supports Registry (wrapping CMake/Autotools) and Native (vmake native packages) repository types, declare dependencies via OnRequire, automatic download, version matching, and build. Prefer `CMakeConfigure` / `CMakeBuild` / `CMakeInstall` for CMake projects; the API manages tools, directories, global flags, and parallelism.
- **Extension Plugin System**: CLI command extensions and cross-compilation toolchain management
- **Incremental Builds**: Per-action success records compare ordered commands, tools, environment, and input/output content; objects are isolated by target and source. Targets execute serially; `-j` bounds compilation and Make/CMake helper parallelism within a target. External build callbacks run once per session and delegate incremental work to their build system
- **TUI Configuration Interface**: Interactive terminal user interface for project configuration
- **Toolchain Management**: Flexible switching between multiple compiler toolchains, supports cross-compilation
- **Semantic Versioning**: Built-in semver parsing and constraint matching
- **Symbol Management**: Five-layer defense (`SetDefaultVisibilityHidden` + `SetVersionScript` + `AddExcludeLibs` + `SetSymbolBinding` + `vmake check-symbols`) controls exported symbols to prevent conflicts and leaks in complex dependency graphs. Default hidden visibility applies only to the declaring package; dependencies keep their own export rules.

## Quick Start

### Installation

```bash
go install github.com/spock2300/vmake/cmd/vmake@latest
```

### Windows

vmake runs natively on Windows and supports Windows-hosted ARM bare-metal builds.

1. **Git for Windows** — install the *full* installer, not MinGit. vmake locates the
   bundled MSYS userland (`sh`, coreutils, `sed`/`awk`/`grep`/`find`, `tar`, `unzip`,
   `curl`) and prepends it to its own `PATH`. vmake also forces
   `core.autocrlf=false`/`core.eol=lf` on every git invocation, so cached checkouts
   stay byte-identical to their repositories regardless of the installer's answer to
   the "Checkout Windows-style, commit Unix-style" question.
2. **A toolchain for the target** — select the ARM GNU toolchain for bare-metal
   firmware, or MinGW-w64 for native Windows binaries. An ARM build does not need
   a native MinGW C compiler. Install the host tools used by the build scripts:
   CMake uses Ninja by default on Windows; `p.Make()`, preset generation and the
   default menuconfig need the selected toolchain's `make` program. Ordinary C/C++
   builds and custom menuconfig programs with an existing config do not require
   default make. An explicitly configured MAKE is still validated. Git for Windows
   supplies neither C compilers nor make.

vmake's storage layout is built on symbolic links, which Windows only permits in
**Developer Mode** (Settings → System → For developers) or from an elevated shell.
Run `vmake doctor` to check all of the above; it reports the status of symlinks, the
Git userland, `make` and the selected C toolchain. Use `vmake doctor --toolchain NAME`
to diagnose a specific toolchain.

Toolchains describe only which programs to run; the target platform belongs to the
project. `build.go` declares the global options `target_os` (bare metal: `none`)
and `target_triple` (e.g. `arm-none-eabi`), and owns the matching CPU/ABI flags.
The target OS controls output naming, linker flags and CMake settings
independently of the host OS.
`vmake test` runs native test binaries; use `vmake build --tests` for cross-compiled
test targets. `vmake check-symbols` can inspect ELF dynamic symbols on either host
using the selected toolchain's `nm`; PE files and ELF files without a dynamic
symbol table report that the audit is not applicable. Windows subgraph behavior
is outside the current compatibility validation.

GCC and Clang assembly inputs track both assembler `.include` files and `.S`
preprocessor headers. Clang requires an external GNU assembler supported by its
driver; configure the target and assembler search path through the toolchain flags
(`--target` and `-B` as needed). Clang bare-metal support is outside this validation;
use GNU ARM GCC for Windows ARM firmware builds.

Build the Windows executable from Linux with CGO disabled:

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o vmake.exe ./cmd/vmake
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
            AddDefines(ctx.If("debug", "DEBUG"))
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
│   ├── buildscript/     # Build script scan, interpret, load
│   ├── pipeline/        # Phase orchestration (require/configure/build)
│   ├── config/          # Configuration storage
│   ├── lockfile/        # .vmake/vmake.lock read/write (pinned versions)
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
│   ├── gosrc/           # Go source merging (buildscript + plugin)
│   ├── jsonio/          # JSON serialization
│   ├── scriptfs/        # Script-relative file IO for interpreted code
│   ├── toposort/        # Topological sort
│   ├── yaegibase/       # yaegi interpreter init helper
│   └── yaegisym/        # cobra/pflag yaegi symbols (go generate)
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
ctx.When(option string, value any) bool
ctx.Select(option string, mapping map[string]string) string

// Target configuration
ctx.Target(name string) *Target
```

## Extension Plugins

Extension plugins are dynamically loaded by the yaegi Go interpreter — no `.so` compilation. Each extension repository is a Git repository, where each subdirectory (containing a `plugin.json`) at the repository root is an independent plugin.

### Capabilities

- **CLI Command Extension**: Add custom subcommands via `AddSubCommand`
- **Toolchain Management**: Register custom toolchains with auto-download on first use via `toolchain.json` + `tc` plugin (Git LFS or HTTP)
- **Global Build/Link Flags**: Inject C/CXX/linker flags into all builds via `AddGlobalCFlags`, `AddGlobalCxxFlags`, and `AddGlobalLdFlags`. `CMakeConfigure()` inherits them automatically; use `MergedCFlags()` when adding project flags to an explicit override.

### Usage Flow

1. Add an extension repository:

```bash
vmake ext add <name> <git-url>
```

2. Plugins are auto-discovered and interpreted on the next run. Restart vmake to use new commands.

Toolchain definitions load independently. Invalid or legacy manifests and definitions
without an installation for the current host are reported by `vmake toolchain list`;
healthy toolchains remain available. Selecting an invalid toolchain fails with its
original error and never switches to the host toolchain. The built-in `vmake ext`
commands skip plugin execution so a broken extension can still be updated or removed.

See the [Extension Plugin Guide](docs/EXTENSION_PLUGIN.md) for the complete plugin authoring tutorial, all interface references, and practical examples.

## Command-Line Usage

### Build Commands

```bash
vmake build [--toolchain <name>] [--mode <mode>] [-i|--install] [-p|--prefix <dir>] [--install-type <type>] [--manifest <file>] [--tests] [--jobs/-j <n>] [--keep-going/-k]
vmake test
vmake clean [--all]
vmake distclean [--purge-cache]
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
vmake repo trust <name>                    # Trust a remote repo's buildscripts
vmake repo untrust <name>                  # Revoke trust
```

### Package Management

```bash
vmake pkg list
vmake pkg search <keyword>
vmake pkg clean <repo/name> [-a]
vmake pkg update <repo/name>[@version]
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
vmake git tag [version] [--minor|--major] [--no-push] [-m|--message <msg>]   # Version tagging
vmake query [targets|config]                          # Show dependency tree / package config
vmake check-symbols [--strict]                        # Scan built outputs for symbol issues
vmake lock update|show                                # Re-resolve / show pinned versions (.vmake/vmake.lock)
vmake init-editor                                     # Generate editor support files for build.go
vmake doctor                                          # Diagnose build.go patterns
vmake manifest show <path>                            # Show manifest contents
vmake manifest checkout <path> [name]                 # Checkout packages to recorded versions
vmake completion <shell>                              # Generate shell completion (bash|zsh|fish|powershell)
vmake completion install                              # Auto-install shell completion
vmake update [version]                                # Self-update
vmake version                                         # Version info
vmake skill install                                   # Install AI skill
vmake skill uninstall                                 # Uninstall AI skill
vmake skill path                                      # Show skill paths
```

Global flags: `-v` (verbose), `-V` (very verbose), `-q` (quiet), `-y` (assume yes for interactive prompts)

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
| `test_data/21_root_package` | Root package selection (SetRoot) |
| `test_data/22_version_script` | Version script linker integration |
| `test_data/23_link_strategy` | Link strategy tests |
| `test_data/24_symbol_prefix` | Symbol prefix (objcopy --prefix-symbols) |
| `test_linux/17_firmware` | Full firmware build (Linux, U-Boot, BusyBox, App, RootFS, Firmware) |

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

## Contact

- Project URL: https://github.com/spock2300/vmake
