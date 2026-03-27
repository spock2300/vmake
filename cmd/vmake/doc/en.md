# VMake

Go-plugin-based C/C++ build system. Build instructions are written in Go (`build.go`) using a fluent API. Each `build.go` is compiled into a Go plugin and executed through a multi-phase lifecycle.

## Project Structure

	myproject/
	├── build.go          <- plugin entry (package main)
	├── src/
	│   └── main.c
	├── lib/
	│   └── build.go      <- sub-package
	└── include/

## Lifecycle

| Phase | Hook | Purpose |
|-------|------|---------|
| 1 | `OnRequire` | Declare third-party dependencies |
| 2 | `OnConfig` | Define build options |
| 3 | `OnBuild` | Generate build targets |
| 4 | `OnInstall` | Post-build install logic |

`OnPackage` runs during plugin extraction, right after `Main()` is called and before any lifecycle phases. It populates the `*Package` with metadata (Git URLs, versions, libs, description, license) so the dependency resolver and source downloader can use it.

## Usage Example

	package main

	import "gitee.com/spock2300/vmake/pkg/api"

	func Main(p *api.Package) {
		p.OnConfig(func(ctx *api.ConfigContext) {
			ctx.Option("debug").
				SetType(api.OptionBool).
				SetDefault(false).
				SetDescription("Enable debug mode")
			ctx.Option("ssl").
				SetType(api.OptionBool).
				SetDefault(false).
				SetDescription("Enable SSL support")
		})

		p.OnBuild(func(ctx *api.BuildContext) {
			ctx.Target("app").
				SetKind(api.TargetBinary).
				AddFiles("src/*.c").
				AddIncludes("include").
				AddDefines(ctx.If("debug", "DEBUG=1")).
				AddDefines(ctx.IfNot("debug", "NDEBUG")).
				AddCFlags(ctx.Select("opt", map[string]string{
					"O0": "-O0", "O2": "-O2",
				})).
				AddLinks(ctx.If("ssl", "ssl", "crypto")).
				AddDeps("lib:utils").
				AddPackages("official/zlib")

			ctx.Target("tests").
				SetKind(api.TargetBinary).
				AddFiles("tests/*.c").
				AddDeps("app").
				SetDefault(false)
		})
	}

Conditional API: `If` (if bool then values), `IfNot` (if not), `Select` (map option value), `Equal` (match string), `When` (compare value, returns bool), `IfGlobal`/`SelectGlobal` (built-in global options: `mode`, `toolchain`).

All setter methods return the receiver for chaining. Use `filepath.Join()` for filesystem paths. Package IDs use `/` (e.g., `official/zlib`), target IDs use `:` (e.g., `lib:utils`). `SetBuildFunc` callback receives `*Package`, returns `error`.

## Source Code

For complete details, read the vmake source code: `pkg/api/` directory contains all type definitions and public API.

---

# API Reference

## Package

Import: `gitee.com/spock2300/vmake/pkg/api`

### Lifecycle Hooks

| Method | Callback Type |
|--------|---------------|
| `OnRequire(fn RequireFunc)` | `func(ctx *RequireContext)` |
| `OnConfig(fn ConfigFunc)` | `func(ctx *ConfigContext)` |
| `OnBuild(fn BuildFunc)` | `func(ctx *BuildContext)` |
| `OnInstall(fn InstallFunc)` | `func(ctx *InstallContext)` |
| `OnPackage(fn PackageFunc)` | `func(p *Package)` |

### Metadata (fluent, returns `*Package`)

| Method | Signature | Description |
|--------|-----------|-------------|
| `SetGit` | `(urls ...string)` | Git repository URLs |
| `SetHomepage` | `(url string)` | Project homepage |
| `SetDescription` | `(desc string)` | Package description |
| `SetLicense` | `(license string)` | License identifier |
| `AddVersion` | `(version, ref string)` | Version to git ref mapping |
| `SetVersions` | `(versions map[string]string)` | Bulk version map |
| `SetSubmodules` | `(v bool)` | Enable git submodules |
| `SetLibs` | `(libs ...string)` | Library dependencies |
| `SetRepo` | `(repo string)` | Repository name |
| `SetName` | `(name string)` | Package name |
| `SetOutputDir` | `(dir string)` | Output directory |
| `SetSourceOrigin` | `(o SourceOrigin)` | Local or remote source |

### Targets & Dependencies

| Method | Signature | Description |
|--------|-----------|-------------|
| `Target` | `(name string) *Target` | Get or create a target |
| `AddPackages` | `(packages ...string) *Package` | Third-party package deps |
| `AddInstalls` | `(src, dest string) *Package` | Install entry |
| `SetInstallFilter` | `(filter InstallFilterFunc) *Package` | Install file filter |

### Build Helpers (run in OnBuild/OnInstall)

| Method | Description |
|--------|-------------|
| `CMakeConfigure(extraArgs...)` | cmake -S src -B build --prefix=... |
| `CMakeBuild(args...)` | cmake --build build |
| `CMakeInstall()` | cmake --install build |
| `Configure(extraArgs...)` | ./configure --prefix=... |
| `Make(args...)` | make -C build |
| `Run(name, args...)` | Run command in BuildDir |
| `RunIn(dir, name, args...)` | Run command in dir |
| `RunWithEnv(env, name, args...)` | Run with extra env vars |

All build helpers return `error`.

### Property Getters

| Method | Returns | Description |
|--------|---------|-------------|
| `CC()` | `string` | C compiler |
| `CXX()` | `string` | C++ compiler |
| `AR()` | `string` | Archiver |
| `CrossTarget()` | `string` | Cross-compilation target |
| `SysRoot()` | `string` | Sysroot path |
| `CFlags()` / `CXXFlags()` / `LDFlags()` | `string` | Compiler/linker flags |
| `SourceDir()` / `BuildDir()` / `InstallDir()` | `string` | Directories |
| `Env()` | `map[string]string` | Toolchain env vars |
| `PackageName()` | `string` | Full package name |
| `Libs()` | `[]string` | Library deps |
| `Deps()` | `map[string]*InstalledPackage` | Resolved dependencies |
| `SelectVersion(constraint)` | `(string, error)` | Best version match |

---

## Target

All setters are fluent (return `*Target`).

### Setters

| Method | Signature | Description |
|--------|-----------|-------------|
| `SetKind` | `(kind TargetKind)` | Binary/Static/Shared/Object/Void |
| `SetDefault` | `(isDefault bool)` | Include in default build |
| `AddFiles` | `(files ...any)` | Source files (supports globs, []string) |
| `AddIncludes` | `(dirs ...any)` | Include directories |
| `AddPublicIncludes` | `(args ...any)` | Includes propagated to dependents (use @"pattern" to match) |
| `AddDefines` | `(defines ...any)` | Preprocessor defines |
| `SetLanguages` | `(langs ...string)` | "c" or "c++" |
| `AddLinks` | `(libs ...any)` | Libraries to link |
| `AddDeps` | `(targets ...string)` | Target dependencies (same pkg: `"name"`, cross pkg: `"pkg:name"`) |
| `AddCFlags` | `(flags ...any)` | C compiler flags |
| `AddCxxFlags` | `(flags ...any)` | C++ compiler flags |
| `AddLdFlags` | `(flags ...any)` | Linker flags |
| `AddPackages` | `(packages ...string)` | Third-party packages |
| `SetBuildFunc` | `(fn func(p *Package) error)` | Custom build logic |
| `SetInstallDir` | `(dir string)` | Install directory |
| `SetInstall` | `(install bool)` | Control install |
| `SetOutput` | `(output string)` | Custom output path |

### Removers

`RemoveCFlags`, `RemoveCxxFlags`, `RemoveLdFlags`, `RemoveDefines`, `RemoveIncludes`, `RemovePublicIncludes`, `RemoveLinks`, `RemoveDeps` — each takes variadic `...string`, returns `*Target`.

### Key Getters

| Method | Returns |
|--------|---------|
| `Name()` | `string` |
| `Kind()` | `TargetKind` |
| `IsDefault()` | `bool` |
| `Files()` | `[]string` |
| `Includes()` | `[]string` |
| `Defines()` | `[]string` |
| `Links()` | `[]string` |
| `Deps()` | `[]string` |
| `CFlags()` / `CxxFlags()` / `LdFlags()` | `[]string` |
| `Packages()` | `[]string` |
| `BuildFunc()` | `func(p *Package) error` |
| `Output()` | `string` | Custom output path |

---

## Option

All setters are fluent (return `*Option`).

| Method | Signature | Description |
|--------|-----------|-------------|
| `SetType` | `(t OptionType)` | Bool/String/Int/Choice |
| `SetDefault` | `(v any)` | Default value |
| `SetDescription` | `(desc string)` | Description |
| `SetValues` | `(vals ...string)` | Choice values (OptionChoice) |
| `SetShowIf` | `(fn func(ctx *ConfigContext) bool)` | Conditional visibility |
| `SetGroup` | `(group string)` | Display group |
| `SetGlobal` | `()` | Mark as global option |

Getters: `Name()`, `Type()`, `Default()`, `Description()`, `Values()`, `ShowIf()`, `Group()`, `IsGlobal()`.

---

## ConfigContext

Embedded: `ConfigAccessor`

| Method | Description |
|--------|-------------|
| `Option(name) *Option` | Get or create option |
| `GlobalOption(name) *Option` | Get or create global option |
| `GlobalMode() *Option` | Built-in mode option |
| `SetConfigValue(name, val)` | Set config value |
| `GetOptions() map[string]*Option` | All options |
| `PackageName() string` | Package name |

---

## BuildContext

Embedded: `ConfigAccessor`, `GlobalAccessor`

| Method | Description |
|--------|-------------|
| `Target(name) *Target` | Get or create target |
| `GetTargets() map[string]*Target` | All targets |
| `PackageName() string` | Package name |
| `IfGlobal(option, then...) []string` | Conditional on global bool |
| `SelectGlobal(option, mapping) string` | Map global option value |
| `AddInstalls(src, dest)` | Install entry |
| `AddPackages(packages...)` | Third-party packages |
| `SubBuild(tcName, dir, args...)` | Invoke vmake as subprocess |
| `Exec(name, args...)` | Run command with logging |

---

## InstallContext

Embedded: `ConfigAccessor`, `GlobalAccessor`

| Method | Description |
|--------|-------------|
| `SetPrefix(prefix string)` | Install prefix |
| `Prefix() string` | Get prefix |
| `PrefixSet() bool` | Was prefix set |
| `AddInstalls(src, dest)` | Install entry |
| `PackageName() string` | Package name |

---

## RequireContext

Embedded: `ConfigAccessor`

| Method | Description |
|--------|-------------|
| `AddRequires(deps...)` | Add dependency (`"official/zlib >=1.2"`) |
| `GetRequires() []RequireInfo` | All requires |
| `ResetRequires()` | Clear requires |
| `RunFuncs()` | Execute registered funcs |

---

## ConfigAccessor

Embedded by all context types. Provides option value access.

| Method | Signature | Description |
|--------|-----------|-------------|
| `Bool` | `(name string) bool` | Get bool value |
| `String` | `(name string) string` | Get string value |
| `Int` | `(name string) int` | Get int value |
| `BoolStr` | `(name string) string` | Returns "ON"/"OFF" |
| `If` | `(option string, then ...string) []string` | Values if bool is true |
| `IfNot` | `(option string, then ...string) []string` | Values if bool is false |
| `Equal` | `(option, value, dep string) string` | Return dep if String==value |
| `Select` | `(option string, mapping map[string]string) string` | Map option value |
| `When` | `(option string, value any) bool` | Compare option value |
| `Option` | `(name string) *Option` | Get or create option |

---

## GlobalAccessor

Embedded by `BuildContext`, `InstallContext`.

| Method | Description |
|--------|-------------|
| `GlobalBool(name) bool` | Global bool value |
| `GlobalString(name) string` | Global string value |
| `Mode() string` | Current build mode |

---

## Constants

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
		OptionBool   OptionType = 0
		OptionString OptionType = 1
		OptionInt    OptionType = 2
		OptionChoice OptionType = 3
	)

	const ModeOptionName      = "mode"
	const ToolchainOptionName = "toolchain"
	const ModeDebug           = "debug"
	const ModeRelease         = "release"

---

## Types

	type PackageMeta struct { Repo string; Name string }

	type InstalledPackage struct {
		Name, Version, InstallDir string
		IncludeDir, LibDir, BinDir string
		Libs, Deps []string
	}

	type InstallItem struct { Src string; Dest string }
	type RequireInfo struct { Name string; Constraint string }

	type SourceOrigin int
	const (
		SourceLocal  SourceOrigin = 0
		SourceRemote SourceOrigin = 1
	)

	type Toolchain struct {
		Target, Prefix, CC, CXX, LD, AR string
		CFlags, CXXFlags, LDFlags, SysRoot string
	}

	type Version struct { Major, Minor, Patch int; Pre string }
	type Constraint struct { Op string; Version Version }

	func NewPackage() *Package
	func NewInstalledPackage(name, version, installDir string, libs []string) *InstalledPackage
	func NewToolchain() *Toolchain
