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
| 2a | `ResolveDeferred` | Clone/compile remote packages (automatic) |
| 2b | `OnConfig` | Define build options |
| 3 | `OnBuild` | Generate build targets |
| 4 | `OnInstall` | Post-build install logic |

`OnPackage` runs during plugin extraction, right after `Main()` is called and before any lifecycle phases. Use it for package metadata (`SetDescription`, `SetLicense`, `SetHomepage`). `SetGit`/`AddVersion` inside `OnPackage` is for **registry repo** packages only — native repo and local packages must NOT use these.

## Conditional API

`If` (if bool then values), `IfNot` (if not), `Select` (map option value), `Equal` (match string), `When` (compare value, returns bool).

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
| `SetGit` | `(urls ...string)` | Git repository URLs (registry repo only) |
| `SetHomepage` | `(url string)` | Project homepage |
| `SetDescription` | `(desc string)` | Package description |
| `SetLicense` | `(license string)` | License identifier |
| `AddVersion` | `(version, ref string)` | Version to git ref mapping |
| `SetVersions` | `(versions map[string]string)` | Bulk version map |
| `SetSubmodules` | `(v bool)` | Enable git submodules |
| `SetRepo` | `(repo string)` | Repository name |
| `SetName` | `(name string)` | Package name |
| `SetOutputDir` | `(dir string)` | Output directory |
| `SetDirs` | `(dirs PkgDirs)` | Source/Build/Install directories |
| `SetToolchain` | `(tc *toolchain.Toolchain)` | Set toolchain |
| `AddPatches` | `(paths ...string)` | Git patches to apply |
| `SetPatches` | `(paths ...string)` | Set git patches |
| `SetConfigFiles` | `(files ...string)` | Config files for stamp invalidation |
| `SetSrcDir` | `(dir string)` | Source code directory |
| `SetScriptDir` | `(dir string)` | Build script directory |
| `SetCfgVals` | `(vals map[string]any)` | Set config values |
| `SetGenConfigHeader` | `(v bool)` | Enable generated config header |
| `SetDryRun` | `(v bool)` | Dry run mode |

### Targets & Dependencies

| Method | Signature | Description |
|--------|-----------|-------------|
| `Target` | `(name string) *Target` | Get or create a target |

### Build Helpers (run in OnBuild/OnInstall)

All build helpers use `exec.RunFatal` (call `os.Exit` on failure) EXCEPT `RunEnv` which returns a real error.

| Method | Signature | Description |
|--------|-----------|-------------|
| `Run` | `(name string, args ...string) error` | Run command in BuildDir (os.Exit on failure) |
| `RunIn` | `(dir, name string, args ...string) error` | Run command in dir (os.Exit on failure) |
| `RunEnv` | `(env map[string]string, name string, args ...string) error` | Run with extra env vars (**returns real error**) |
| `CMakeConfigure` | `(extraArgs ...string) error` | cmake -S src -B build --prefix=... |
| `CMakeBuild` | `(args ...string) error` | cmake --build build |
| `CMakeInstall` | `() error` | cmake --install build |
| `Configure` | `(extraArgs ...string) error` | ./configure --prefix=... |
| `Make` | `(args ...string) error` | make -C build (uses `pkg.Env()`, passes `-j<ncpu>`) |

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
| `SourceDir()` | `string` | Package root (where build.go lives) |
| `SrcDir()` | `string` | Source code directory (SourceDir()/src/ when SetGit downloads) |
| `SrcDirRaw()` | `string` | Raw srcCodeDir without SourceDir fallback (empty if SetSrcDir not called) |
| `BuildDir()` | `string` | Build scratch directory |
| `InstallDir()` | `string` | Installation prefix |
| `OutputDir()` | `string` | Output directory |
| `ScriptDir()` | `string` | Build script directory (same as SourceDir) |
| `Env()` | `map[string]string` | Toolchain env vars (CC, CXX, AR, etc.) |
| `Deps()` | `map[string]*InstalledPackage` | Resolved dependencies |
| `GetRequires()` | `*Requires` | Package requires |
| `GitURLs()` | `[]string` | Git repository URLs |
| `Homepage()` | `string` | Project homepage |
| `Description()` | `string` | Package description |
| `License()` | `string` | License identifier |
| `Versions()` | `map[string]string` | Version to ref mapping |
| `GetVersions()` | `[]string` | Available version list (unsorted) |
| `GenConfigHeader()` | `bool` | Generated config header enabled |
| `GetRef` | `(version string) string` | Git ref for a version |
| `SelectVersion` | `(constraint string) (string, error)` | Best version match |
| `SelectVersionMulti` | `(constraints []string) (string, error)` | Best version matching multiple constraints |
| `Submodules()` | `bool` | Git submodules enabled |
| `GetPatches()` | `[]string` | Git patch paths |
| `ConfigFiles()` | `[]string` | Stamp-related config files |
| `DryRun()` | `bool` | Dry run mode |
| `SetDep` | `(name string, pkg *InstalledPackage) *Package` | Set resolved dependency |

### Package KConfig Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `AddKConfig` | `(name string) *KConfigEntry` | Create KConfig entry |
| `KConfigEntries` | `() []*KConfigEntry` | All KConfig entries |
| `SelectedPreset` | `() string` | Selected preset name |
| `EnsureConfig` | `(srcDir string) bool` | Check `.config` exists & non-empty, run `make <preset>` if not |

### Package Linker Script Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `SetProvidedLinkerScript` | `(path string) *Package` | Declare linker script for consumers (fatal on double-set) |
| `ProvidedLinkerScript` | `() string` | Get declared linker script path |

---

## Target

All setters are fluent (return `*Target`).

### Setters

| Method | Signature | Description |
|--------|-----------|-------------|
| `SetKind` | `(kind TargetKind)` | Binary/Static/Shared/Object/Void |
| `SetDefault` | `(isDefault bool)` | Include in default build |
| `SetTest` | `(v bool)` | Mark as test target (auto-sets isDefault=false) |
| `AddFiles` | `(files ...any)` | Source files (globs, strings, []string) |
| `RemoveFiles` | `(files ...any)` | Exclude files from AddFiles glob expansion (pattern matching) |
| `AddIncludes` | `(dirs ...any)` | Include directories |
| `AddPublicIncludes` | `(args ...any)` | Includes propagated to dependents (use @"pattern" to match) |
| `AddDefines` | `(defines ...any)` | Preprocessor defines |
| `AddLinks` | `(libs ...any)` | Libraries to link |
| `AddProvidedLibs` | `(libs ...string)` | Libraries this target provides to consumers (e.g. `"ssl"`, `"crypto"`) |
| `AddDeps` | `(targets ...string)` | Dependencies: same pkg (`"utils"`), cross pkg (`"pkg:name"`), wildcard (`"pkg:*"` / `"repo/pkg:*"`), third-party (`"official/zlib"`) |
| `AddCFlags` | `(flags ...any)` | C compiler flags |
| `AddCxxFlags` | `(flags ...any)` | C++ compiler flags |
| `AddLdFlags` | `(flags ...any)` | Linker flags |
| `SetBuildFunc` | `(fn func(p *Package) error)` | Custom build logic (for third-party packages) |
| `SetPrebuilt` | `(path string)` | Pre-compiled artifact — skip compilation, symlink to output path |
| `SetInstallDir` | `(dir string)` | Install directory |
| `SetInstall` | `(install bool)` | Control install |
| `SetLinkerScript` | `(path string)` | Linker script (passes `-T` to linker; fatal on double-set) |
| `UseDependencyLinkerScript` | `()` | Auto-inherit linker script from dependency |
| `AddPostLink` | `(tool string, args ...string)` | Post-link step: `{output}` placeholder |
| `AddPostLinkHex` | `()` | `objcopy -O ihex {output} {output}.hex` |
| `AddPostLinkBin` | `()` | `objcopy -O binary {output} {output}.bin` |
| `AddPostLinkSize` | `()` | `size {output}` |
| `AddPostLinkStrip` | `()` | `strip -o {output}.stripped {output}` |
| `AddBinHeader` | `(inputs ...any)` | Binary files → `.h` headers; output to `build/<tc>-<mode>/generated/`; incremental via mtime |

`SetLanguages(langs ...string)` exists but has **no effect** — language is auto-detected from file extension (`.c` → C, `.cc/.cpp/.cxx` → C++).

### Removers

`RemoveCFlags`, `RemoveCxxFlags`, `RemoveLdFlags`, `RemoveDefines`, `RemoveIncludes`, `RemovePublicIncludes`, `RemoveLinks`, `RemoveDeps` — each takes variadic `...string`, returns `*Target`. These perform immediate exact-match deletion from internal slices.

`RemoveFiles` takes variadic `...any` (like `AddFiles`) but uses **deferred pattern matching** — patterns are matched against glob-expanded paths at build time, not removed from the rule list. This lets you exclude files like `RemoveFiles("src/test_*.c")` after a broad `AddFiles("src/*.c")`.

### Key Getters

| Method | Returns |
|--------|---------|
| `Name()` | `string` |
| `Kind()` | `TargetKind` |
| `IsDefault()` | `bool` |
| `IsTest()` | `bool` |
| `Files()` | `[]string` |
| `ExcludedFiles()` | `[]string` |
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
| `Prebuilt()` | `string` |
| `LinkerScript()` | `string` |
| `UseDepLinkerScript()` | `bool` |
| `PostLinkSteps()` | `[]PostLinkStep` |
| `GenRules()` | `[]GenRule` |
| `HasDep(depRef)` | `bool` |
| `IncludeRule(dir)` | `[]string` |

---

## Context Types

All context types embed `ConfigAccessor` for option value access (see below).

### ConfigContext

| Method | Description |
|--------|-------------|
| `Option(name) *Option` | Get or create option |
| `GlobalOption(name) *Option` | Get or create global option |
| `GlobalMode() *Option` | Built-in mode option |
| `ToolchainOption() *Option` | Toolchain choice option (auto-populated) |
| `Toolchains() []string` | Available toolchain names |
| `SetConfigValue(name, val) *ConfigContext` | Set config value |
| `GetOptions() map[string]*Option` | All options |
| `PackageName() string` | Package name |
| `KConfig(name) *KConfigEntry` | Create/get KConfig entry |
| `SetProvidedLinkerScript(path) *ConfigContext` | Declare linker script for consumers |
| `AddGlobalCFlags(flags...)` | Add global C flags (OnApply only) |
| `AddGlobalCxxFlags(flags...)` | Add global C++ flags (OnApply only) |
| `AddGlobalLdFlags(flags...)` | Add global linker flags (OnApply only) |

### BuildContext

| Method | Description |
|--------|-------------|
| `Target(name) *Target` | Get or create target |
| `GetTargets() map[string]*Target` | All targets |
| `PackageName() string` | Package name |
| `AddInstalls(src, dest)` | Install entry |
| `SetInstallFilter(filter)` | Install file filter |
| `BuildSubGraph(pkgName)` | Build package as independent sub-graph |
| `DepOutput(depRef) string` | Get output path of dependency target |
| `DepBuildDir(depRef) string` | Get build directory of dependency target |
| `Exec(name, args...)` | Run command with logging (os.Exit on failure) |
| `GenerateConfigHeader()` | Set `genConfigHeader = true`; propagated to `Package.SetGenConfigHeader(true)` — generates `autoconf.h` during scheduler build |
| `GenerateConfigDefines()` | Set `genConfigDefines = true`; on processing, reads `ImportConfigs()`, merges local + imported options, adds `-DCONFIG_*` defines to all targets |
| `ExportConfig()` | Set `exportConfig = true`; propagated to `Package.SetExportConfig(true)` |
| `ImportConfig(names...)` | Append package names to `importConfigs` list (merge + `-D` injection happens inside `GenerateConfigDefines` processing) |
| `SyncConfigDefines(names...)` | Shorthand for `GenerateConfigDefines` + `ImportConfig` (for parent/orchestrator packages) |
| `GenConfigDefines() bool` | Whether `genConfigDefines` was set |
| `GenConfigHeader() bool` | Whether `genConfigHeader` was set |
| `ExportEnabled() bool` | Whether `exportConfig` was set |
| `SetDryRun(v bool) *BuildContext` | Set dry run mode |
| `GetInstallItems() []InstallItem` | All install items |
| `GetInstallFilter() InstallFilterFunc` | Install file filter |

### InstallContext

| Method | Description |
|--------|-------------|
| `SetPrefix(prefix string)` | Install prefix |
| `Prefix() string` | Get prefix |
| `PrefixSet() bool` | Was prefix set |
| `AddInstalls(src, dest)` | Install entry |
| `PackageName() string` | Package name |

### RequireContext

| Method | Description |
|--------|-------------|
| `AddRequires(deps...)` | Add dependency (`"official/zlib >=1.2"`) |
| `GetRequires() []RequireInfo` | All requires |
| `ResetRequires()` | Clear requires |

### ConfigAccessor (embedded by all context types)

| Method | Signature | Description |
|--------|-----------|-------------|
| `Bool` | `(name string) bool` | Get bool value |
| `String` | `(name string) string` | Get string value |
| `Int` | `(name string) int` | Get int value |
| `BoolStr` | `(name string) string` | Returns "ON"/"OFF" |
| `If` | `(option string, then ...string) []string` | Values if bool is true |
| `IfNot` | `(option string, then ...string) []string` | Values if bool is false |
| `Equal` | `(option, value, dep string) string` | Return dep if String==value |
| `Select` | `(option string, mapping map[string]string) string` | Map option value (returns `""` in discoverAll) |
| `When` | `(option string, value any) bool` | Compare option value (returns `true` in discoverAll) |
| `Option` | `(name string) *Option` | Get or create option |

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
| `SetOnApply` | `(fn func(ctx *ConfigContext, val string))` | Callback after option values resolved |
| `SetGroup` | `(group string)` | Display group |

Getters: `Name()`, `Type()`, `Default()`, `Description()`, `Values()`, `ShowIf()`, `OnApply()`, `Group()`, `IsGlobal()`.

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

	func (k TargetKind) Ext() string        // ".a" (static), ".so" (shared), ".o" (object), "" (binary/void)
	func (k TargetKind) Prefix() string     // "lib" for static/shared, "" otherwise
	func (k TargetKind) InstallDir() string // "bin" (binary), "lib" (static/shared), "" otherwise

	type OptionType int
	const (
		OptionBool   OptionType = 0
		OptionString OptionType = 1
		OptionInt    OptionType = 2
		OptionChoice OptionType = 3
	)

	func (t OptionType) String() string // "bool", "string", "int", "choice"

	const ModeOptionName      = "mode"
	const ToolchainOptionName = "toolchain"
	const ModeDebug           = "debug"
	const ModeRelease         = "release"

---

## Types

	type PkgDirs struct { SourceDir string; BuildDir string; InstallDir string }

	type Requires struct { ... }
	func (r *Requires) Add(deps ...string)
	func (r *Requires) AddInfos(infos ...RequireInfo)
	func (r *Requires) Get() []RequireInfo
	func (r *Requires) Reset()

	type PackageMeta struct { Repo string; Name string }
	func (m *PackageMeta) FullName() string

	type InstalledPackage struct {
		Name, Version, InstallDir string
		IncludeDir, LibDir, BinDir string
		Libs, Deps []string
	}

	func (ip *InstalledPackage) UpdateLibDir()

	type InstallItem struct { Src string; Dest string }
	type RequireInfo struct { Name string; Constraint string }
	type PostLinkStep struct { Tool string; Args []string }
	func (s PostLinkStep) OutputPaths(outputPath string) []string

	type SourceOrigin int
	const (
		SourceLocal  SourceOrigin = 0
		SourceRemote SourceOrigin = 1
	)

	type CopyFilter func(path string, isDir bool) bool
	type InstallFilterFunc func(path string, isTargetOutput bool) bool

	type KConfigEntry struct { ... }
	func (k *KConfigEntry) Name() string
	func (k *KConfigEntry) Description() string
	func (k *KConfigEntry) ConfigPath() string
	func (k *KConfigEntry) SrcDir() string
	func (k *KConfigEntry) Presets() []string
	func (k *KConfigEntry) DefaultPreset() string
	func (k *KConfigEntry) SelectedPreset() string
	func (k *KConfigEntry) MenuconfigCmd() string
	func (k *KConfigEntry) Patches() map[string]string
	func (k *KConfigEntry) SetDescription(desc string) *KConfigEntry
	func (k *KConfigEntry) SetConfigPath(path string) *KConfigEntry
	func (k *KConfigEntry) SetSrcDir(dir string) *KConfigEntry
	func (k *KConfigEntry) SetMenuconfigCmd(cmd string) *KConfigEntry
	func (k *KConfigEntry) AddPreset(name string) *KConfigEntry
	func (k *KConfigEntry) SetDefault(presetName string) *KConfigEntry
	func (k *KConfigEntry) SetSelectedPreset(name string) *KConfigEntry
	func (k *KConfigEntry) PatchKConfig(patches map[string]string) *KConfigEntry

	type GenRuleKind string
	const GenRuleBinHeader GenRuleKind = "binheader"

	type GenRule struct { ... }
	func (r *GenRule) Kind() GenRuleKind
	func (r *GenRule) Input() string
	func (r *GenRule) OutputStem() string

---

## Constructor Functions

	func NewPackage() *Package
	func NewInstalledPackage(name, version, installDir string, libs []string) *InstalledPackage
	func NewConfigContext(pkgName string) *ConfigContext
	func NewConfigContextWithPackage(pkgName string, pkg *Package) *ConfigContext
	func NewBuildContext(pkgName string, cfgVals map[string]any) *BuildContext
	func NewInstallContext(pkgName string, cfgVals map[string]any) *InstallContext

---

## Utility Functions

### KConfig

	func ApplyKConfigPatches(configPath string, patches map[string]string)

### File Copy

Available as `api.CopyFile`, `api.CopyDir`, etc. — useful in `SetBuildFunc` for post-install layout adjustments.

| Function | Description |
|----------|-------------|
| `CopyFile(src, dest string) error` | Copy a single file |
| `CopyDir(src, dest string) error` | Copy directory (skips `.git`) |
| `CopyDirWithFilter(src, dest string, filter CopyFilter) error` | Copy directory with filter |
| `CopyDirIfExists(src, dst string) error` | Copy directory only if source exists |

### Package Reference

	func SplitPackageRef(ref string) (repo, name string, ok bool)
	func MatchPatterns(patterns []string, name string) bool

### Build Mode

	func GetModeFlags(mode string) (cflags []string, defines []string)

### Semver

Version format: `[v]MAJOR[.MINOR][.PATCH][-PRERELEASE]` (e.g. `1.2.3`, `v2.0`, `1.0.0-rc.1`).

```go
type Version struct { Major, Minor, Patch int; Pre string }
type Constraint struct { Op string; Version Version }
func ParseVersion(s string) (Version, bool)
func ParseConstraint(s string) (Constraint, bool)
func (v Version) Compare(other Version) int
func (v Version) String() string
	func (c Constraint) Match(v Version) bool
	func MatchVersion(available []string, constraint string) (string, bool)
	func CheckCycle(path []string, current string) error
```

**Constraint operators:**

| Op | Meaning | Major lock |
|----|---------|------------|
| `>=` | ≥ (default when no operator) | Yes (major > 0 only) |
| `>` | > | No |
| `<=` | ≤ | No |
| `<` | < | No |
| `=` | exact match (including pre) | — |
| `~` | lock major.minor, patch ≥ | Yes |

`>=` with major > 0 restricts to same major version (`>=1.2` won't match `2.0.0`). Empty constraint (`>=0.0.0`) matches all. Selection: highest version satisfying all constraints. Pre-release: `1.0.0-rc.1 < 1.0.0`; numeric identifiers < alpha identifiers in pre-release segments.
