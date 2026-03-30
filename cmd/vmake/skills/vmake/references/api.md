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
| 2a | `ResolveDeferred` | Clone/compile remote packages (deferred resolution) |
| 2b | `OnConfig` | Define build options |
| 3 | `OnBuild` | Generate build targets |
| 4 | `OnInstall` | Post-build install logic |

`OnPackage` runs during plugin extraction, right after `Main()` is called and before any lifecycle phases. It populates the `*Package` with metadata (Git URLs, versions, libs, description, license) so the dependency resolver and source downloader can use it.

**Note**: `OnPackage` with `SetGit`/`AddVersion` is for **registry repo** packages only. **Native repo** packages do NOT use these — versions come from git tags automatically, and the git URL is resolved from the repository URL template.

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
				AddDeps("official/zlib")

			ctx.Target("tests").
				SetKind(api.TargetBinary).
				AddFiles("tests/*.c").
				AddDeps("app").
				SetDefault(false)
		})
	}

Conditional API: `If` (if bool then values), `IfNot` (if not), `Select` (map option value), `Equal` (match string), `When` (compare value, returns bool).

All setter methods return the receiver for chaining. Use `filepath.Join()` for filesystem paths. Package IDs use `/` (e.g., `official/zlib`), target IDs use `:` (e.g., `lib:utils`). `SetBuildFunc` callback receives `*Package`, returns `error`.

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
| `SetDirs` | `(dirs PkgDirs)` | Source/Build/Install directories |
| `SetToolchain` | `(tc *toolchain.Toolchain)` | Set toolchain |
| `AddPatches` | `(paths ...string)` | Git patches to apply |
| `SetPatches` | `(paths ...string)` | Set git patches |

### Targets & Dependencies

| Method | Signature | Description |
|--------|-----------|-------------|
| `Target` | `(name string) *Target` | Get or create a target |
| `AddInstalls` | `(src, dest string) *InstallItemHolder` | Install entry |
| `SetInstallFilter` | `(filter InstallFilterFunc) *InstallItemHolder` | Install file filter |

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
| `RunEnv(env, name, args...)` | Run with extra env vars |

All build helpers return `error`.

### Property Getters

| Method | Returns | Description |
|--------|---------|-------------|
| `CC()` | `string` | C compiler |
| `CXX()` | `string` | C++ compiler |
| `AR()` | `string` | Archiver |
| `CrossTarget()` | `string` | Cross-compilation target |
| `Prefix()` | `string` | Toolchain prefix |
| `CFlags()` / `CXXFlags()` / `LDFlags()` | `string` | Compiler/linker flags |
| `ObjCopy()` | `string` | objcopy tool path |
| `Size()` | `string` | size tool path |
| `ObjDump()` | `string` | objdump tool path |
| `NM()` | `string` | nm tool path |
| `SourceDir()` / `BuildDir()` / `InstallDir()` / `OutputDir()` | `string` | Directories |
| `GetRequires()` | `*Requires` | Package requires |
| `Env()` | `map[string]string` | Toolchain env vars |
| `Libs()` | `[]string` | Library deps |
| `Deps()` | `map[string]*InstalledPackage` | Resolved dependencies |
| `SelectVersion(constraint)` | `(string, error)` | Best version match |
| `GetVersions()` | `[]string` | Sorted version list |
| `GitURLs()` | `[]string` | Git repository URLs |
| `Homepage()` | `string` | Project homepage |
| `Description()` | `string` | Package description |
| `License()` | `string` | License identifier |
| `Versions()` | `map[string]string` | Version to ref mapping |
| `Submodules()` | `bool` | Git submodules enabled |
| `ScriptDir()` | `string` | Build script directory |
| `GetPatches()` | `[]string` | Git patch paths |
| `SetDep(name, pkg)` | | Set resolved dependency |

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
| `AddDeps` | `(targets ...string)` | Dependencies: same pkg (`"name"`), cross pkg (`"pkg:name"`), third-party (`"official/zlib"`) |
| `AddCFlags` | `(flags ...any)` | C compiler flags |
| `AddCxxFlags` | `(flags ...any)` | C++ compiler flags |
| `AddLdFlags` | `(flags ...any)` | Linker flags |
| `SetBuildFunc` | `(fn func(p *Package) error)` | Custom build logic (for third-party packages with external build systems) |
| `SetInstallDir` | `(dir string)` | Install directory |
| `SetInstall` | `(install bool)` | Control install |
| `SetLinkerScript` | `(path string)` | Linker script for binary target (passes `-T` to linker) |
| `AddPostLink` | `(tool string, args ...string)` | Post-link step: runs `tool args...` after linking, supports `{output}` placeholder |
| `AddPostLinkHex` | `()` | Shorthand: `objcopy -O ihex {output} {output}.hex` |
| `AddPostLinkBin` | `()` | Shorthand: `objcopy -O binary {output} {output}.bin` |
| `AddPostLinkSize` | `()` | Shorthand: `size {output}` |
| `AddPostLinkStrip` | `()` | Shorthand: `strip -o {output}.stripped {output}` |
| `AddBinHeader` | `(inputs ...any)` | Convert binary files to `.h` hex headers (e.g. `AddBinHeader("assets/logo.bin")`); accepts `string` or `[]string`; output to `build/<tc>-<mode>/generated/<stem>.h`; include path auto-added; incremental via mtime |

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
| `PublicIncludes()` | `[]string` |
| `Defines()` | `[]string` |
| `Links()` | `[]string` |
| `Deps()` | `[]string` |
| `CFlags()` / `CxxFlags()` / `LdFlags()` | `[]string` |
| `Languages()` | `[]string` |
| `InstallDir()` | `string` |
| `NoInstall()` | `bool` |
| `BuildFunc()` | `func(p *Package) error` |
| `LinkerScript()` | `string` |
| `PostLinkSteps()` | `[]PostLinkStep` |
| `GenRules()` | `[]GenRule` |

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

Getters: `Name()`, `Type()`, `Default()`, `Description()`, `Values()`, `ShowIf()`, `Group()`, `IsGlobal()`.

---

## ConfigContext

Embedded: `ConfigAccessor`

| Method | Description |
|--------|-------------|
| `Option(name) *Option` | Get or create option |
| `GlobalOption(name) *Option` | Get or create global option |
| `GlobalMode() *Option` | Built-in mode option |
| `ToolchainOption() *Option` | Toolchain choice option (auto-populated from registered toolchains) |
| `Toolchains() []string` | Available toolchain names |
| `SetConfigValue(name, val)` | Set config value |
| `GetOptions() map[string]*Option` | All options |
| `PackageName() string` | Package name |

---

## BuildContext

Embedded: `ConfigAccessor`

| Method | Description |
|--------|-------------|
| `Target(name) *Target` | Get or create target |
| `GetTargets() map[string]*Target` | All targets |
| `PackageName() string` | Package name |
| `AddInstalls(src, dest)` | Install entry |
| `SetInstallFilter(filter)` | Install file filter |
| `BuildSubGraph(pkgName)` | Build package as independent sub-graph |
| `DepOutput(depRef) string` | Get output path of dependency target |
| `Exec(name, args...)` | Run command with logging (calls Fatal on error) |

---

## InstallContext

Embedded: `ConfigAccessor`

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
| `Select` | `(option string, mapping map[string]string) string` | Map option value (returns `""` in discoverAll mode) |
| `When` | `(option string, value any) bool` | Compare option value (returns `true` in discoverAll mode) |
| `Option` | `(name string) *Option` | Get or create option |
| `SetOptions` | `(options map[string]*Option)` | Set options map |
| `MergeGlobals` | `(globalOptions map[string]*Option, globalVals map[string]any)` | Merge global options/values as fallback |

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

	type PkgDirs struct { SourceDir string; BuildDir string; InstallDir string }

	type Requires struct { ... }
	func (r *Requires) Add(deps ...string)
	func (r *Requires) Get() []RequireInfo
	func (r *Requires) Reset()

	type PackageMeta struct { Repo string; Name string }

	type InstalledPackage struct {
		Name, Version, InstallDir string
		IncludeDir, LibDir, BinDir string
		Libs, Deps []string
	}

	type InstallItem struct { Src string; Dest string }
	type RequireInfo struct { Name string; Constraint string }
	type PostLinkStep struct { Tool string; Args []string }

	type SourceOrigin int
	const (
		SourceLocal  SourceOrigin = 0
		SourceRemote SourceOrigin = 1
	)

	type Version struct { Major, Minor, Patch int; Pre string }
	type Constraint struct { Op string; Version Version }

	func NewPackage() *Package
	func NewInstalledPackage(name, version, installDir string, libs []string) *InstalledPackage
