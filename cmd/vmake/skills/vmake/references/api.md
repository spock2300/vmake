# VMake

Go-based C/C++ build system. Build instructions are written in Go (`build.go`) using a fluent API. Each `build.go` is interpreted at runtime by yaegi (Go interpreter) and executed through a multi-phase lifecycle.

## Project Structure

	myproject/
	├── build.go          <- buildscript entry (package main)
	├── src/
	│   └── main.c
	├── lib/
	│   └── build.go      <- sub-package
	└── include/

## Lifecycle

| Phase | Hook / Step | Purpose |
|-------|-------------|---------|
| 1 | `OnRequire` | Declare deps (runs with nil config; all packages resolved eagerly) |
| 2 | `OnConfig` | Define build options, resolve values, run `OnApply` callbacks |
| 3 | `FilterDeps` | Re-runs `OnRequire` with real config; recomputes deps; BFS needed packages |
| 4 | `OnBuild` | Generate build targets |
| 5 | Compile & Link | Scheduler compiles sources and links targets |
| 6* | Install | Optional install (only with `--install` flag) |
| — | `OnInstall` | Install-phase hook: declare `AddInstalls` entries / install filter (runs before files are copied) |

`OnPackage` runs during buildscript extraction, right after `Main()` is called and before any lifecycle phases. Use it for package metadata (`SetDescription`, `SetLicense`, `SetHomepage`). `SetGit`/`AddVersion` inside `OnPackage` is for **registry repo** packages — native repo versions come from git tags automatically; local packages can also use `SetGit`/`AddVersion` for source download.

## Conditional API

`If` (if bool then values), `Select` (map option value), `When` (compare value, returns bool; negate for inverse conditions).

All setter methods return the receiver for chaining (exception: `SetDefaultFlags` returns nothing). Use `filepath.Join()` for filesystem paths. Package IDs use `/` (e.g., `official/zlib`), target IDs use `:` (e.g., `lib:utils`). `SetBuildFunc` callback receives `*Package`, returns `error`.

# API Reference

## Package

Import: `github.com/spock2300/vmake/pkg/api`

### Lifecycle Hooks

| Method | Callback Type |
|--------|---------------|
| `OnRequire(fn RequireFunc)` | `func(ctx *RequireContext)` |
| `OnConfig(fn ConfigFunc)` | `func(ctx *ConfigContext)` |
| `OnBuild(fn BuildFunc)` | `func(ctx *BuildContext)` |
| `OnInstall(fn InstallFunc)` | `func(ctx *InstallContext)` |
| `OnClean(fn CleanFunc)` | `func(ctx *CleanContext)` |
| `OnPackage(fn PackageFunc)` | `func(p *Package)` — single-slot: a second registration is a build error |

### Metadata (fluent, returns `*Package`)

| Method | Signature | Description |
|--------|-----------|-------------|
| `SetGit` | `(urls ...string)` | Git repository URLs (mirror list — tried in order); registry packages and local packages wrapping remote source |
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
| `SetConfigFiles` | `(files ...string)` | Package configuration-file metadata; does not control target skipping |
| `SetSrcDir` | `(dir string)` | Source code directory |
| `SetScriptDir` | `(dir string)` | Build script directory |
| `SetCfgVals` | `(vals map[string]any)` | Set config values |
| `SetGenConfigHeader` | `(v bool)` | Enable generated config header |
| `SetExportConfig` | `(v bool)` | Enable config export |
| `SetImportConfigs` | `(names []string)` | Set imported config package names |
| `SetDryRun` | `(v bool)` | Dry run mode |
| `SetRoot` | `(v bool)` | Mark as root package |
| `SetGlobalFlags` | `(cflags, cxxflags, ldflags, links []string)` | Set global compiler/linker flags |
| `SetCMakeBuildDir` | `(dir string) *Package` | Set CMake build directory; relative to BuildDir unless absolute |
| `SetCMakeInstallDir` | `(dir string) *Package` | Set CMake install prefix; relative to BuildDir unless absolute |
| `SetCMakeBuildType` | `(buildType string) *Package` | Set the default CMake configuration, e.g. MinSizeRel |

### Targets & Dependencies

| Method | Signature | Description |
|--------|-----------|-------------|
| `Target` | `(name string) *Target` | Get or create a target |

### Build Helpers (run in OnBuild/OnInstall/SetBuildFunc)

The following helpers return **nothing** and raise a script error on failure; the execution boundary catches it and releases resources: `Run`, `RunIn`, `CMakeConfigure`, `CMakeBuild`, `CMakeInstall` (plus the `CleanContext` `Run`/`RunIn` wrappers). Real-error helpers: `RunEnv`, `Make`, `Configure`. Never `return` a void-return helper — call it as a statement.

| Method | Signature | Description |
|--------|-----------|-------------|
| `Run` | `(name string, args ...string)` | Run command in BuildDir (script error on failure) |
| `RunIn` | `(dir, name string, args ...string)` | Run command in dir (script error on failure) |
| `RunEnv` | `(env map[string]string, name string, args ...string) error` | Run with extra env vars in BuildDir (**returns real error**) |
| `CMakeConfigure` | `(extraArgs ...string)` | Configure SrcDir into CMakeBuildDir with CMakeInstallDir, resolved tools, build type, target settings, package visibility, and global flags |
| `CMakeGlobalFlagsArgs` | `() []string` | Package visibility and global flag arguments for special manual CMake calls; CMakeConfigure already includes them |
| `DefaultVisibilityHidden` | `() bool` | Whether this package enables hidden default visibility |
| `VisibilityFlags` | `() (cflags, cxxflags []string)` | Package visibility defaults for C and C++; empty when not enabled |
| `MergedCFlags` | `(extra ...string) string` | Merge package visibility defaults + global C flags + extra, space-joined |
| `MergedCxxFlags` | `(extra ...string) string` | Merge package visibility defaults + global C++ flags + extra, space-joined |
| `MergedLdFlags` | `(extra ...string) string` | Merge global linker flags + extra, space-joined |
| `CMakeBuild` | `(args ...string)` | Build CMakeBuildDir with the default configuration and session jobs budget |
| `CMakeInstall` | `(args ...string)` | Install CMakeBuildDir using the same default configuration; accepts --component and other install options |
| `Configure` | `(extraArgs ...string) error` | SrcDir/configure --prefix=... (+ --host when cross) |
| `Make` | `(args ...string) error` | make -C BuildDir with `pkg.Env()` and the session jobs budget (**returns real error**) |

Dry-run aware: in dry-run mode (query/check-symbols/install), all helpers log commands without executing them.

### CMake Integration

Prefer `CMakeConfigure`, `CMakeBuild`, and `CMakeInstall` for CMake projects. Keep
project options in build.go and let the helpers manage the tools, paths, target
system, configuration, and parallelism. Direct `cmake` calls are for special
operations the helpers cannot express.

- Defaults: `SrcDir()` for sources, `BuildDir()/cmake` for the build tree, remote
  `InstallDir()` or local `BuildDir()/staging` for installation. Use
  `CMakeBuildDir()` / `CMakeInstallDir()` when referring to artifacts. Directory
  setters do not change `PkgDirs`; void callbacks still run once per session.
- `SetCMakeBuildType("MinSizeRel")` changes the configuration used by all three
  stages. Otherwise VMake debug/release mode chooses Debug/Release. Explicit
  `--config` on build or install overrides that invocation's configuration.
- Use setters for build directory, install prefix, and default build type. Raw
  directory overrides, `--prefix`, `-DCMAKE_INSTALL_PREFIX`, and
  `-DCMAKE_BUILD_TYPE` are rejected with a setter hint. Other project arguments
  are forwarded, including explicit generators and configure presets.
  `CMakeBuild` rejects `--preset` because a build preset's `binaryDir` overrides
  the managed build directory. Use `CMakeConfigure("--preset", "name")`, then
  select build targets/configuration with `--target` / `--config`.
- Configured compilers/binutils resolve strictly to paths. ASM defaults to the
  resolved CC driver. CMake chooses its underlying linker; VMake's `Tools.LD`
  is not passed as `CMAKE_LINKER`. The target OS sets `CMAKE_SYSTEM_NAME`; bare
  metal uses Generic, static-library compiler checks, host program search and
  target-only library/include search. Set `CMAKE_SYSTEM_PROCESSOR`
  explicitly when needed.
- C/CXX flags start with the package's visibility defaults, then inherit the
  project's global flags. Executable/shared/module linker flags inherit global
  flags. Explicit cache arguments for a
  flags variable replace its automatic value. Use `MergedCFlags(extra...)` when
  that override should retain package defaults and global flags. ASM flags must
  be explicit. Make/Configure inherit the merged C/CXX/linker flags through `pkg.Env()`.
- Windows defaults to Ninja unless a generator or configure preset is explicit. Build
  uses the normalized VMake jobs budget (`-j0` means CPU count). Explicit
  arguments, backend arguments and environment parallel settings must be positive
  and no greater than that budget; smaller requests reduce parallelism. Bare
  `-j` and inherited Make jobserver tokens are rejected. CMakeInstall stays serial
  unless parallelism is requested. Default make is resolved only when make is used.

```go
p.SetCMakeBuildType("MinSizeRel")
cflags := p.MergedCFlags("-fno-builtin")
p.CMakeConfigure(
    "-DBUILD_SHARED_LIBS=OFF",
    "-DCMAKE_C_FLAGS="+cflags,
    "-DCMAKE_ASM_FLAGS="+cflags,
)
p.CMakeBuild()
p.CMakeInstall()
```

Migration: old CMake artifacts directly under `BuildDir()` now live under
`CMakeBuildDir()`. Replace manual directory/prefix/build-type arguments with
setters, remove redundant `CMakeGlobalFlagsArgs()` from helper calls, and replace
make-after-CMake with `CMakeBuild()` / `CMakeInstall()`. Clean the old build tree
before rebuilding. For local `SetPrebuilt` wrappers, keep CMake's archive in its
own build tree and publish the staged archive through the getter.

### Property Getters

| Method | Returns | Description |
|--------|---------|-------------|
| `CC()` | `string` | C compiler |
| `CXX()` | `string` | C++ compiler |
| `AR()` | `string` | Archiver |
| `TargetTriple()` | `string` | Target triple from the project's `target_triple` global option, e.g. `arm-none-eabi` |
| `TargetOS()` | `string` | Target OS from the project's `target_os` global option; `none` for bare metal, host OS when unset |
| `Prefix()` | `string` | Raw configured toolchain prefix, including the trailing `-`, e.g. `arm-none-eabi-`; does not add the installation path |
| `CFlags()` / `CXXFlags()` / `LDFlags()` | `string` | Default compiler/linker flags (builtin `host` toolchain only) |
| `ObjCopy()` | `string` | objcopy tool path |
| `Size()` | `string` | size tool path |
| `ObjDump()` | `string` | objdump tool path |
| `NM()` | `string` | nm tool path |
| `SourceDir()` | `string` | Package root (where build.go lives) |
| `SrcDir()` | `string` | Source code directory (SourceDir()/src/ when SetGit downloads) |
| `SrcDirRaw()` | `string` | Raw srcCodeDir without SourceDir fallback (empty if SetSrcDir not called) |
| `BuildDir()` | `string` | Build scratch directory |
| `InstallDir()` | `string` | Installation prefix |
| `CMakeBuildDir()` | `string` | CMake build tree, default BuildDir()/cmake |
| `CMakeInstallDir()` | `string` | CMake installation prefix, default remote InstallDir() or local BuildDir()/staging |
| `OutputDir()` | `string` | Output directory |
| `ScriptDir()` | `string` | Build script directory (set via `SetScriptDir`; defaults to `""`) |
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
| `ExportConfig()` | `bool` | Config export enabled |
| `ImportConfigs()` | `[]string` | Imported config package names |
| `IsRoot()` | `bool` | Whether marked as root package |
| `GlobalCFlags()` / `GlobalCxxFlags()` / `GlobalLdFlags()` / `GlobalLinks()` | `[]string` | Global flags |
| `GetRef` | `(version string) string` | Git ref for a version |
| `SelectVersion` | `(constraint string) (string, error)` | Best version match |
| `SelectVersionMulti` | `(constraints []string) (string, error)` | Best version matching multiple constraints |
| `Submodules()` | `bool` | Git submodules enabled |
| `GetPatches()` | `[]string` | Git patch paths |
| `ConfigFiles()` | `[]string` | Package configuration-file metadata |
| `DryRun()` | `bool` | Dry run mode |
| `SetDep` | `(name string, pkg *InstalledPackage) *Package` | Set resolved dependency |

`Prefix()` includes the separator: append `"gcc"`, never `"-gcc"`. Migrate old callers that add a hyphen themselves. Use `TargetTriple()` for an external argument that expects a target triple such as `arm-none-eabi`.

`p.Env()` resolves configured tools to executable paths. For an installed toolchain, `p.Env()["CROSS_COMPILE"]` includes its `bin` directory and complete prefix, such as `/path/to/toolchain/bin/arm-none-eabi-`; do not append another hyphen. Prefer `p.CMakeConfigure()`, which passes resolved tools automatically. If a special operation requires calling CMake manually, pass `p.Env()["CC"]`, `["CXX"]` and `["AR"]` instead of reconstructing names from a prefix or relying on PATH.

### Package KConfig Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `AddKConfig` | `(name string) *KConfigEntry` | Create KConfig entry (**single entry per package** — a second `AddKConfig` is a build error) |
| `KConfigEntries` | `() []*KConfigEntry` | All KConfig entries |
| `SelectedPreset` | `() string` | Selected preset name (selected > default) |
| `EnsureConfig` | `(srcDir string) bool` | Check `.config` in srcDir exists & non-empty; if not, run `make <preset>` there and apply `SetKConfigPatches`. Fatal when no preset is selected (declare `SetDefaultPreset` or pick one in the TUI) |

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
| `SetTest` | `(v bool)` | Mark as test target (does NOT touch isDefault — scheduler decides inclusion; `vmake build` skips tests, `--tests`/`vmake test` include them) |
| `AddFiles` | `(files ...any)` | Source files (globs, strings, []string) |
| `RemoveFiles` | `(files ...any)` | Exclude files from AddFiles glob expansion (pattern matching) |
| `AddIncludes` | `(dirs ...any)` | Include directories |
| `AddPublicIncludes` | `(args ...any)` | Includes propagated to dependents (use @"pattern" to match) |
| `AddDefines` | `(defines ...any)` | Preprocessor defines |
| `AddLinks` | `(libs ...any)` | Libraries to link |
| `AddProvidedLibs` | `(libs ...string)` | Libraries this target provides to consumers (e.g. `"ssl"`, `"crypto"`) |
| `AddDeps` | `(targets ...string)` | Dependencies: same pkg (`"utils"`), cross pkg (`"pkg:name"`), wildcard (`"pkg:*"`), third-party (`"official/zlib"`). Invalid refs (whitespace, stray `:`, empty segments) are build errors |
| `AddCFlags` | `(flags ...any)` | C compiler flags |
| `AddCxxFlags` | `(flags ...any)` | C++ compiler flags |
| `AddLdFlags` | `(flags ...any)` | Linker flags |
| `SetBuildFunc` | `(fn func(p *Package) error)` | Custom build logic (for third-party packages) |
| `SetPrebuilt` | `(path string)` | Pre-compiled artifact — skip compilation, symlink to output path (fatal on double-set) |
| `SetInstallDir` | `(dir string)` | Install directory |
| `SetInstall` | `(install bool)` | Control install |
| `SetLinkerScript` | `(path string)` | Linker script (passes `-T` to linker; fatal on double-set) |
| `SetVersionScript` | `(path string)` | Version script for symbol visibility (Shared/Binary only; fatal on double-set) |
| `AddExcludeLibs` | `(libs ...string)` | Strip symbols from absorbed static archives via `-Wl,--exclude-libs=` |
| `SetSymbolBinding` | `(mode string)` | `"static"` → `-Bsymbolic`; `"static-functions"` → `-Bsymbolic-functions` |
| `SetSymbolPrefix` | `(prefix string)` | Post-link `objcopy --prefix-symbols=` (fatal on double-set) |

`AddDeps` ref grammar (`pkg/api/depref.go`): a ref without `:` and `/` is a target of the declaring package; `pkg:target` selects one target; `pkg:*` or a `/`-containing path (e.g. `"official/zlib"`) expands to all targets of that package plus its transitive package deps (flat closure, deduplicated). Validation is fatal at declaration time (empty/whitespace refs, multiple `:`, empty segments, malformed paths); unknown targets (`dependency not found`), unknown packages (`package not found in build graph`) and cycles fail at build-graph time. A `pkg` part without `/` in `pkg:target` is first resolved relative to the declaring (sub-)package.

Sub-packages: a nested `build.go` in a native remote package's checkout is an independent package named `parent/sub`, versioned by the parent (no separate lockfile entry). Only native repos have sub-packages (registry wrappers never do — design decision DD-1, see `docs/DESIGN_DECISIONS.md`); they load lazily when depended on. Reference from outside by full name (`"subtest/mother/sub_a:*"`; list the parent before its sub-packages in `AddRequires`); inside a sub-package use short names for siblings (`"sub_b:utils_b"`). A parent cannot require its own sub-packages in `OnRequire`. Example: `test_data/25_subpackage`.
| `UseDependencyLinkerScript` | `()` | Auto-inherit linker script from dependency |
| `AddPostLink` | `(tool string, args ...string)` | Post-link step: `{output}` placeholder |
| `AddPostLinkOutputs` | `(paths ...string)` | Explicit extra outputs for missing-output rebuilds and automatic installation; supports `{output}`, SourceDir-relative paths and absolute paths. Hex/Bin/Strip helpers declare their outputs automatically; command arguments never imply outputs |
| `AddPostLinkHex` | `()` | `objcopy -O ihex {output} {output}.hex` |
| `AddPostLinkBin` | `()` | `objcopy -O binary {output} {output}.bin` |
| `AddPostLinkSize` | `()` | `size {output}` |
| `AddPostLinkStrip` | `()` | `strip -o {output}.stripped {output}` |
| `AddPostLinkDeps` | `(files ...string)` | Extra input files for post-link steps (SourceDir-relative); any change/missing → relink + re-run all post-link (no-op on Prebuilt targets, which short-circuit before the relink check) |
| `AddBinHeader` | `(inputs ...any)` | Binary files → `.h` headers; output to `build/<buildKey>/generated/`; incremental via mtime |

`SetLanguages(langs ...string)` exists but has **no effect** — language is auto-detected from file extension (`.c` → C, `.cc/.cpp/.cxx/.C` → C++).

### Removers

`RemoveCFlags`, `RemoveCxxFlags`, `RemoveLdFlags`, `RemoveDefines`, `RemoveIncludes`, `RemovePublicIncludes`, `RemoveLinks`, `RemoveDeps`, `RemoveProvidedLibs` — each takes variadic `...string`, returns `*Target`. These perform immediate exact-match deletion from internal slices.

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
| `VersionScript()` | `string` |
| `ExcludeLibs()` | `[]string` |
| `SymbolBinding()` | `string` |
| `SymbolPrefix()` | `string` |
| `ProvidedLibs()` | `[]string` |
| `UseDepLinkerScript()` | `bool` |
| `PostLinkSteps()` | `[]PostLinkStep` |
| `PostLinkOutputs()` | `[]string` |
| `PostLinkDeps()` | `[]string` |
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
| `KConfig(name) *KConfigEntry` | Create KConfig entry (single slot — a second call is a build error) |
| `SetProvidedLinkerScript(path) *ConfigContext` | Declare linker script for consumers |
| `AddGlobalCFlags(flags...)` | Add global C flags (effective in OnConfig and OnApply; only applied for packages that survive FilterDeps) |
| `AddGlobalCxxFlags(flags...)` | Add global C++ flags (same) |
| `AddGlobalLdFlags(flags...)` | Add global linker flags (same) |
| `AddGlobalLinks(links...)` | Add global link libraries (same) |
| `SetDefaultVisibilityHidden() *ConfigContext` | Add `-fvisibility=hidden` to this package (C+C++) and `-fvisibility-inlines-hidden` (C++ only); idempotent in OnConfig/OnApply, BuildScriptError without a package |

### BuildContext

| Method | Description |
|--------|-------------|
| `Target(name) *Target` | Get or create target |
| `GetTargets() map[string]*Target` | All targets |
| `SetDefaultFlags(cflags, cxxflags, ldflags []string)` | Set default compile/link flags for all targets |
| `PackageName() string` | Package name |
| `AddInstalls(src, dest)` | Install entry |
| `SetInstallFilter(filter)` | Install file filter |
| `BuildSubGraph(pkgName)` | Build package as independent sub-graph |
| `DepOutput(depRef) string` | Get output path of dependency target |
| `DepBuildDir(depRef) string` | Get build directory of dependency target |
| `Exec(name, args...)` | Run command with logging (script error on failure) |
| `GenerateConfigHeader()` | Set `genConfigHeader = true`; propagated to `Package.SetGenConfigHeader(true)` — generates `autoconf.h` during scheduler build |
| `GenerateConfigDefines()` | Set `genConfigDefines = true`; on processing, reads `ImportConfigs()`, merges local + imported options, adds `-DCONFIG_*` defines to all targets |
| `ExportConfig()` | Set `exportConfig = true`; propagated to `Package.SetExportConfig(true)` |
| `ImportConfig(names...)` | Append package names to `importConfigs` list (merge + `-D` injection happens inside `GenerateConfigDefines` processing) |
| `ImportConfigs() []string` | Get imported config package names |
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
| `SetInstallFilter(filter InstallFilterFunc)` | Filter which files get installed (inherited from `InstallItemHolder`) |
| `GetInstallItems() []InstallItem` | All install items (inherited) |
| `GetInstallFilter() InstallFilterFunc` | Current install filter (inherited) |
| `PackageName() string` | Package name |

### CleanContext

| Method | Description |
|--------|-------------|
| `SourceDir() string` | Package root directory |
| `BuildDir() string` | Build output directory |
| `SrcDir() string` | Source code directory (differs from SourceDir when `SetGit()` is used) |
| `Run(name, args...)` | Run command in BuildDir (script error on failure; returns nothing) |
| `RunIn(dir, name, args...)` | Run command in specified directory (script error on failure; returns nothing) |
| `RunEnv(env, name, args...) error` | Run with custom environment (**returns real error**) |
| `Make(args...) error` | Run make in BuildDir with `pkg.Env()` (**returns real error**) |
| `PackageName() string` | Package name |

### RequireContext

OnRequire callbacks execute twice: first during graph discovery with nil config values (Phase 1), then again during FilterDeps with actual config values from config.json (Phase 3). This enables option-conditional dependencies. During discovery, direct value reads (`ctx.Bool/String/Int`) are build errors — use `ctx.When`/`ctx.If`/`ctx.Select`. Pass-2 requires **replace** the dependency edges: a dep whose guard is false in pass 2 is dropped, but a dep declared only in pass 2 (not discovered in Phase 1) is a build error — declare deps unconditionally and condition them with `ctx.When`/`ctx.If`.

| Method | Description |
|--------|-------------|
| `AddRequires(deps...)` | Add dependency (`"official/zlib >=1.2"`) |
| `GetRequires() []RequireInfo` | All requires |
| `ResetRequires()` | Clear requires |

### ConfigAccessor (embedded by all context types)

Strict in script contexts (`OnConfig`/`OnBuild`/`OnInstall`/`OnClean`/`OnRequire`): reading an unknown option name, or using an accessor that mismatches the option's `SetType`, is a **build error** (not a silent zero value). During `OnRequire` discovery, direct reads (`Bool`/`String`/`Int`) are also build errors — use the discovery-aware `If`/`Select`/`When`. TUI/CLI read paths stay lenient.

| Method | Signature | Description |
|--------|-----------|-------------|
| `Bool` | `(name string) bool` | Get bool value (strict: only for OptionBool) |
| `String` | `(name string) string` | Get string value (strict: OptionString/OptionChoice) |
| `Int` | `(name string) int` | Get int value (strict: OptionInt; coerces float64/int64) |
| `BoolStr` | `(name string) string` | Returns "ON"/"OFF" |
| `If` | `(option string, then ...string) []string` | `then` values if bool option is true; when config is nil (discovery) returns `then` unconditionally; when unset, falls back to declared default |
| `Select` | `(option string, mapping map[string]string) string` | Map option value; returns `""` when config is nil (discovery) or value unmapped |
| `When` | `(option string, value any) bool` | Compare option value (numerics across int/float64); falls back to declared default when unset; returns `true` when config is nil (discovery) |
| `Option` | `(name string) *Option` | Get or create option (build error after the config phase) |

---

## Option

All setters are fluent (return `*Option`).

| Method | Signature | Description |
|--------|-----------|-------------|
| `SetType` | `(t OptionType)` | Bool/String/Int/Choice |
| `SetDefault` | `(v any)` | Default value — must match `SetType` (validated after OnConfig; the classic trap is forgetting `SetType`, whose zero value is `OptionBool`, while setting a string default) |
| `SetDescription` | `(desc string)` | Description |
| `SetValues` | `(vals ...string)` | Choice values (OptionChoice); defaults must be among them |
| `SetShowIf` | `(fn func(ctx *ConfigContext) bool)` | Conditional visibility |
| `SetOnApply` | `(fn func(ctx *ConfigContext, val any))` | Callback after option values resolved, once per build, in sorted option-name order. `val` is normalized to the declared type: `bool` (OptionBool), `int` (OptionInt — JSON `float64` converted to `int`), `string` (OptionString/OptionChoice). The callback's context carries real option values, so reading other options works |
| `SetGroup` | `(group string)` | Display group |

Getters: `Name()`, `Type()`, `Default()`, `Description()`, `Values()`, `ShowIf()`, `OnApply()`, `Group()`, `IsGlobal()`.

Note: without `SetDefault`, the zero value applies (`false` for OptionBool, `""` for OptionString/OptionChoice, `0` for OptionInt). An unset Choice default is not validated against `SetValues`.

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
	func (k *KConfigEntry) MenuconfigArgs() []string
	func (k *KConfigEntry) Patches() map[string]string
	func (k *KConfigEntry) SetDescription(desc string) *KConfigEntry
	func (k *KConfigEntry) SetConfigPath(path string) *KConfigEntry
	func (k *KConfigEntry) SetSrcDir(dir string) *KConfigEntry
	func (k *KConfigEntry) SetMenuconfigCmd(program string, args ...string) *KConfigEntry
	func (k *KConfigEntry) AddPreset(name string) *KConfigEntry
	func (k *KConfigEntry) SetDefaultPreset(presetName string) *KConfigEntry
	func (k *KConfigEntry) SelectPreset(name string) *KConfigEntry
	func (k *KConfigEntry) SetKConfigPatches(patches map[string]string) *KConfigEntry

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
	func NewCleanContext(pkgName string, cfgVals map[string]any) *CleanContext

---

## Utility Functions

### KConfig

	func ApplyKConfigPatches(configPath string, patches map[string]string) error

Replaces lines matching a patch key (by `KEY=` prefix) with the patch value; keys sorted for determinism. Called automatically by `EnsureConfig`.

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
	func ResolveSubPackageName(currentPkg, depName string, subParents map[string]string, exists func(string) bool) string
	func CheckCycle(path []string, current string) error

### Build Mode

	func GetModeFlags(mode string) (cflags []string, defines []string)

### Config Export & Merge

	func ConfigToDefines(options map[string]*Option, cfgVals map[string]any) []string
	func ConfigToHeader(options map[string]*Option, cfgVals map[string]any) string
	func WriteConfigHeader(dir string, content string) error
	func MergeImportedOptions(localOpts map[string]*Option, localVals map[string]any, pkgs []*Package) (map[string]*Option, map[string]any)
	func MergeGlobalOptions(allDefs map[string]map[string]*Option, toolchainList []string) (map[string]*Option, error)

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
```

**Constraint operators:**

| Op | Meaning | Major lock |
|----|---------|------------|
| `>=` | ≥ (default when no operator) | Yes (when constraint major > 0) |
| `>` | > | Yes (when constraint major > 0) |
| `<=` | ≤ | No |
| `<` | < | No |
| `=` | exact match (including pre) | — |
| `~` | lock major.minor, patch ≥ | Yes |

`>=` and `>` with constraint major > 0 restrict to the same major version (`>=1.2` and `>1.0` won't match `2.0.0`); `>=0.x` / `>0.x` are not major-locked. Empty constraint (`>=0.0.0`) matches all. Selection: highest version satisfying all constraints (mutually satisfiable across packages). Pre-release: `1.0.0-rc.1 < 1.0.0`; pre-releases only match when the constraint itself pins a pre-release of the same `major.minor.patch`; numeric identifiers < alpha identifiers in pre-release segments.
