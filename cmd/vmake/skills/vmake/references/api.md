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
| 1 | `OnRequire` | Declare deps (runs with nil config; top-level packages resolved eagerly, native sub-packages lazily) |
| 2 | `OnConfig` | Define build options, resolve values, run `OnApply` callbacks |
| 3 | `FilterDeps` | Re-runs `OnRequire` with this package's entry option values; recomputes deps; BFS needed packages |
| 4 | `OnBuild` | Generate build targets |
| 5 | Compile & Link | Scheduler compiles sources and links targets |
| 6* | Install | Optional install (only with `--install` flag) |
| — | `OnInstall` | Install-phase hook: declare `AddInstalls` entries / install filter (runs before files are copied) |

`OnPackage` runs during buildscript extraction, right after `Main()` is called and before any lifecycle phases. Use it for package metadata (`SetDescription`, `SetLicense`, `SetHomepage`). `SetGit`/`AddVersion` inside `OnPackage` is for **registry repo** packages — native repo versions come from git tags automatically; local packages can also use `SetGit`/`AddVersion` for source download.

## Conditional API

`If` (if bool then values), `Select` (map option value), `When` (matches any of the supplied values, returns bool), `WhenNot` (discovery-safe inverse compare; returns `true` during discovery when config is nil), `WhenAny` (alias for `When` with multiple values).

All setter methods return the receiver for chaining (exceptions: `SetDefaultFlags` returns nothing; `ConfigContext.AddGlobalCFlags`/`AddGlobalCxxFlags`/`AddGlobalDefines`/`AddGlobalLdFlags`/`AddGlobalLinks` return nothing). Use `filepath.Join()` for filesystem paths. Package IDs use `/` (e.g., `official/zlib`), target IDs use `:` (e.g., `lib:utils`). `SetBuildFunc` callback receives `*Package`, returns `error`.

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
| `SetGit` | `(urls ...string)` | Git repository URLs (tried in order); registry packages and local packages wrapping remote source |
| `SetHomepage` | `(url string)` | Project homepage |
| `SetDescription` | `(desc string)` | Package description |
| `SetLicense` | `(license string)` | License identifier |
| `AddVersion` | `(version, ref string)` | Version to git ref mapping |
| `SetVersions` | `(versions map[string]string)` | Bulk version map |
| `SetSubmodules` | `(v bool)` | Enable git submodules |
| `SetRepo` | `(repo string)` | Repository name |
| `SetName` | `(name string)` | Package name |
| `SetDirs` | `(dirs PkgDirs)` | Source/Build/Install directories |
| `SetToolchain` | `(tc *toolchain.Toolchain)` | Set toolchain |
| `AddPatches` | `(paths ...string)` | Git patches to apply |
| `SetPatches` | `(paths ...string)` | Set git patches |
| `SetSrcDir` | `(dir string)` | Source code directory |
| `SetScriptDir` | `(dir string)` | Build script directory |
| `SetCfgVals` | `(vals map[string]any)` | Set config values |
| `SetGenConfigHeader` | `(v bool)` | Enable generated config header |
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
| `AddRequires` | `(deps ...any) *Package` | Declare unconditional package dependencies (`string`, `[]string`) directly in `Main(p)` or `OnPackage` without a separate `OnRequire` callback |

### Build Helpers (methods on the bound `*Package`)

The following helpers return **nothing** and raise a script error on failure; the execution boundary catches it and releases resources: `Run`, `RunIn`, `CMakeConfigure`, `CMakeBuild`, `CMakeInstall` (plus the `CleanContext` `Run`/`RunIn` wrappers). Real-error helpers: `RunEnv`, `Make`, `Configure`. Never `return` a void-return helper — call it as a statement. These are methods on `*Package`: call them from `SetBuildFunc(func(p *api.Package) error { ... })` or from the `p` captured in `Main` while `OnBuild` runs, where VMake has bound the package's source/build directories and toolchain. `OnPackage`, `OnConfig`, and `OnRequire` run before that binding, so toolchain getters (`CC`, `Env`, ...) and command helpers must not be used there. `BuildContext` offers `Exec` for one-off commands; `CleanContext` has its own `Run`/`RunIn`/`RunEnv`/`Make` wrappers.

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
| `BuildPath` | `(elem ...string) string` | Path inside `BuildDir()` (returns symbolic `$builddir/...` when `BuildDir()` is empty during unmaterialized inspection) |
| `WriteBuildFile` | `(relPath string, content any) string` | Declaratively register a `string`/`[]byte` file under `BuildDir()` during `OnBuild` (written if changed when the package builds) and return its `BuildPath` |
| `GenerateFile` | `(relPath string, fn func() ([]byte, error)) string` | Declaratively register a generated file callback under `BuildDir()` during `OnBuild` and return its `BuildPath` |
| `Generate` | `(fn func() error) *Package` | Declaratively register a multi-file generator callback during `OnBuild` (runs before target resolution/compilation during `build`) |

Dry-run aware: in dry-run mode (inspect/query/check-symbols), command helpers log without executing, and `WriteBuildFile`/`GenerateFile`/`Generate` record declarative generators without touching the filesystem. Prefer `WriteBuildFile`/`GenerateFile`/`Generate` and `BuildPath` over raw `os.WriteFile` and `filepath.Rel(p.SourceDir(), p.BuildDir())` in `OnBuild`; target/package path setters (`AddFiles`, `AddIncludes`, `AddPublicIncludes`, `SetLinkerScript`, `SetProvidedLinkerScript`, `SetVersionScript`, `SetPrebuilt`, `AddPostLinkDeps`, `AddPostLinkOutputs`, `AddInstalls`) accept `SourceDir`-relative paths, absolute paths, and `BuildPath` (`$builddir/...`) paths directly.

```go
ctx.Target("zlib").SetKind(api.TargetVoid).SetBuildFunc(func(p *api.Package) error {
    p.CMakeConfigure("-DBUILD_SHARED_LIBS=OFF")
    p.CMakeBuild()
    p.CMakeInstall()
    return nil
})
```

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
  stages. Otherwise VMake debug/release/size mode chooses Debug/Release/MinSizeRel.
  Explicit `--config` on build or install overrides that invocation's configuration.
- Use setters for build directory, install prefix, and default build type. Raw
  directory overrides, `--prefix`, `-DCMAKE_INSTALL_PREFIX`, and
  `-DCMAKE_BUILD_TYPE` are rejected with a setter hint. Other project arguments
  are forwarded, including explicit generators and configure presets.
  `CMakeBuild` rejects `--preset` because a build preset's `binaryDir` overrides
  the managed build directory. Use `CMakeConfigure("--preset", "name")`, then
  elsewhere select build targets/configuration with `--target` / `--config`.
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
  uses the normalized VMake jobs budget (`-j0` means CPU count). If the caller
  passes `-j`/`--jobs`/`--parallel` or sets `CMAKE_BUILD_PARALLEL_LEVEL`, VMake
  forwards that value unchanged and does not add its own `-jN`; explicit values
  are not clamped to the budget. Make jobserver flags in
  `MAKEFLAGS`/`GNUMAKEFLAGS`/`MFLAGS` also suppress the automatic `-jN`.
  CMakeInstall stays serial unless parallelism is requested. Default make is
  resolved only when make is used.
- `CMakeConfigure` re-runs configure when its fingerprint changes or the recorded
  configure outputs are missing, so repeated builds reuse an intact configured
  tree. The fingerprint covers the generated and
  forwarded configure arguments, `CMAKE_*`/`PATH` environment, the resolved cmake
  executable contents, and the contents of every
  `CMakeLists.txt`/`CMakePresets.json`/`CMakeUserPresets.json`/`*.cmake`/`*.cmake.in`
  under `SrcDir()`; the scan skips `.git`, `.vmake_deps`, `CMakeFiles/`,
  `cmake_install.cmake`, and the invocation's build roots when they are nested
  inside `SrcDir()` (`BuildDir()`, `CMakeBuildDir()`, and the parent of `BuildDir()`).
  A cache hit also requires `CMakeCache.txt`, `CMakeFiles/`, and (when the
  generator is known) its top-level build file to still exist; it logs
  `cmake configure (cached): <CMakeBuildDir>` and skips configure; each successful
  configure writes `<CMakeBuildDir>/.vmake_configure.json`. Dry-run neither reads
  nor writes the stamp, and any fingerprint failure is treated as a miss, never a
  build error. Projects using `file(GLOB)` must add `CONFIGURE_DEPENDS` or list
  files explicitly, or new files will not re-trigger configure.

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
| `CC()` | `string` | Configured C compiler name (may be relative; use `p.Env()["CC"]` for the absolute path) |
| `CXX()` | `string` | Configured C++ compiler name (use `p.Env()["CXX"]` for the absolute path) |
| `AR()` | `string` | Configured archiver name (use `p.Env()["AR"]` for the absolute path) |
| `TargetTriple()` | `string` | Resolved target triple (package entry value → package-local non-global default → project global config/default → toolchain `target_triple` default), e.g. `arm-none-eabi` |
| `TargetOS()` | `string` | Resolved target OS (same resolution order); `none` for bare metal, host OS when unset |
| `Prefix()` | `string` | Raw configured toolchain prefix, including the trailing `-`, e.g. `arm-none-eabi-`; does not add the installation path |
| `CFlags()` / `CXXFlags()` / `LDFlags()` | `string` | Default compiler/linker flags (builtin `host` toolchain only) |
| `ObjCopy()` | `string` | objcopy tool path |
| `Size()` | `string` | size tool path |
| `ObjDump()` | `string` | objdump tool path |
| `NM()` | `string` | nm tool path |
| `SourceDir()` | `string` | Package source root: the local package directory, or the downloaded working tree (`src[/<member>]`) for remote packages; a registry wrapper's build.go stays in `ScriptDir()`, not here |
| `SrcDir()` | `string` | Source code directory (SourceDir()/src/ when SetGit downloads) |
| `SrcDirRaw()` | `string` | Raw source dir without the SourceDir fallback; empty unless a source dir was set (the framework sets it itself for `SetGit` workspaces) |
| `BuildDir()` | `string` | Build scratch directory |
| `InstallDir()` | `string` | Installation prefix |
| `CMakeBuildDir()` | `string` | CMake build tree, default BuildDir()/cmake |
| `CMakeInstallDir()` | `string` | CMake installation prefix, default remote InstallDir() or local BuildDir()/staging |
| `ScriptDir()` | `string` | Directory containing the loaded build.go, set by the loader via `SetScriptDir` (empty only for packages created without a script directory) |
| `Env()` | `map[string]string` | Toolchain env vars (CC, CXX, AR, etc.) |
| `Deps()` | `map[string]*InstalledPackage` | Resolved dependencies |
| `GetOptions()` | `map[string]*Option` | Package option map |
| `GetRequires()` | `*Requires` | Package requires |
| `GitURLs()` | `[]string` | Git repository URLs |
| `Homepage()` | `string` | Project homepage |
| `Description()` | `string` | Package description |
| `License()` | `string` | License identifier |
| `Versions()` | `map[string]string` | Version to ref mapping |
| `GetVersions()` | `[]string` | Available version list (unsorted) |
| `GenConfigHeader()` | `bool` | Generated config header enabled |
| `ImportConfigs()` | `[]string` | Imported config package names |
| `IsRoot()` | `bool` | Whether marked as root package |
| `GlobalCFlags()` / `GlobalCxxFlags()` / `GlobalLdFlags()` / `GlobalLinks()` | `[]string` | Global flags |
| `GetRef` | `(version string) string` | Git ref for a version |
| `SelectVersion` | `(constraint string) (string, error)` | Best version match |
| `SelectVersionMulti` | `(constraints []string) (string, error)` | Best version matching multiple constraints |
| `Submodules()` | `bool` | Git submodules enabled |
| `GetPatches()` | `[]string` | Git patch paths |
| `DryRun()` | `bool` | Dry run mode |

`Prefix()` includes the separator: append `"gcc"`, never `"-gcc"`. Migrate old callers that add a hyphen themselves. Use `TargetTriple()` for an external argument that expects a target triple such as `arm-none-eabi`.

`p.Env()` resolves configured tools to **absolute executable paths**. For an installed toolchain, `p.Env()["CROSS_COMPILE"]` includes its `bin` directory and complete prefix, such as `/path/to/toolchain/bin/arm-none-eabi-`; do not append another hyphen. When the toolchain contributes directories (an installed toolchain or absolute compilers), `p.Env()["PATH"]` already has the toolchain's `bin` and compiler directories prepended; the built-in `host` leaves `PATH` to the inherited process environment. Do not export PATH or CC/CXX/AR/LD in the shell or in the script. `p.CC()`/`p.CXX()`/`p.AR()` return the configured names, which may be relative; use `p.Env()["CC"]`, `["CXX"]` and `["AR"]` when an external command needs the executable path. Prefer `p.CMakeConfigure()`, which passes resolved tools automatically.

### Dependency State

| Method | Signature | Description |
|--------|-----------|-------------|
| `SetDep` | `(name string, pkg *InstalledPackage) *Package` | Record one resolved dependency (fluent) |
| `SetDeps` | `(deps map[string]*InstalledPackage) *Package` | Replace the resolved dependency map (fluent) |

### Package KConfig Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `AddKConfig` | `(name string) *KConfigEntry` | Create KConfig entry (**single entry per package** — a second `AddKConfig` is a build error) |
| `KConfigEntries` | `() []*KConfigEntry` | All KConfig entries |
| `SelectedPreset` | `() string` | Selected preset name (selected > default) |
| `EnsureConfig` | `(srcDir string) bool` | Check the KConfig entry's `ConfigPath()` (default `.config`) under srcDir exists & non-empty; if not, run `make <preset>` there (passing `KCONFIG_CONFIG` for a non-default path) and apply `SetKConfigPatches`. Returns true when it regenerated the file; when regeneration is needed but no preset is selected, it is fatal (declare `SetDefaultPreset` or pick one in the TUI). An existing non-empty config needs no preset |

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
| `SetTest` | `(v bool)` | Mark as test target (does NOT touch isDefault — scheduler decides inclusion; `vmake build` skips tests, `--tests`/`vmake test` include them, but a target with `SetDefault(false)` stays excluded everywhere) |
| `SetLinkable` | `(linkable bool)` | Default `true`; when `false` on `TargetObject`/`TargetStatic`, compiles/archives and orders via `AddDeps`, but skips auto-linking its output into dependent binaries/libraries (query path via `DepOutput`) |
| `AddFiles` | `(files ...any)` | Source files (globs, strings, []string) |
| `RemoveFiles` | `(files ...any)` | Exclude files from AddFiles glob expansion (pattern matching) |
| `AddIncludes` | `(dirs ...any)` | Include directories |
| `AddPublicIncludes` | `(args ...any)` | Includes applied to this target and propagated to dependents along `AddDeps` (use `@"pattern"` as last arg to filter installed headers) |
| `AddDefines` | `(defines ...any)` | Preprocessor defines |
| `AddPublicDefines` | `(defines ...any)` | Preprocessor defines applied to this target and propagated to dependents along `AddDeps` |
| `AddLinks` | `(libs ...any)` | Libraries to link (`-l`) |
| `AddPublicLinks` | `(libs ...any)` | Link libraries (`-l`) applied to this target and propagated to dependents along `AddDeps` |
| `AddProvidedLibs` | `(libs ...any)` | Libraries this target provides to consumers (e.g. `"ssl"`, `"crypto"`; accepts slices via `flattenAny`) |
| `AddDeps` | `(targets ...any)` | Dependencies (`string`, `[]string` such as `ctx.If`/`ctx.Select`): same pkg (`"utils"`), bare required/known pkg (`"shell"` → `"shell:*"`), cross pkg (`"<pkg>:<target>"`), wildcard (`"<pkg>:*"`), third-party (`"official/zlib"`). Empty entries are ignored; whitespace, multiple `:`, empty segments, and path-shape errors in refs without `:` are declaration-time build errors |
| `UseRequires` | `()` | Wire all active required packages of this package (post-`FilterDeps`) as `<pkg>:*` dependencies on this target |
| `AddCFlags` | `(flags ...any)` | C compiler flags |
| `AddPublicCFlags` | `(flags ...any)` | C compiler flags applied to this target and propagated to dependents along `AddDeps` |
| `AddCxxFlags` | `(flags ...any)` | C++ compiler flags |
| `AddPublicCxxFlags` | `(flags ...any)` | C++ compiler flags applied to this target and propagated to dependents along `AddDeps` |
| `AddLdFlags` | `(flags ...any)` | Linker flags |
| `AddPublicLdFlags` | `(flags ...any)` | Linker flags applied to this target and propagated to dependents along `AddDeps` |
| `SetBuildFunc` | `(fn func(p *Package) error)` | Custom build logic; executed only for `TargetVoid` (ignored for other kinds) |
| `AddInputs` | `(paths ...any)` | Literal input files for opt-in incremental `TargetVoid` (no glob expansion). Both `AddInputs` and `AddOutputs` are required when either is declared; declaring only one is a build error. With neither, the callback runs once per invocation. Skip decision covers the action signature plus input/output hashes |
| `AddOutputs` | `(paths ...any)` | Literal output files for opt-in incremental `TargetVoid` (no glob expansion) |
| `SetPrebuilt` | `(path string)` | Pre-compiled artifact — skip compilation; output is a symlink to the source (copy when post-link steps exist) (fatal on double-set; ignored for `TargetVoid`) |
| `SetInstallDir` | `(dir string)` | Install directory |
| `SetInstall` | `(install bool)` | Control install |
| `SetLinkerScript` | `(path string)` | Linker script (passes `-T` to linker; `TargetBinary` only; fatal on double-set) |
| `SetVersionScript` | `(path string)` | Version script for symbol visibility (Shared/Binary only, ELF only; fatal on double-set) |
| `AddExcludeLibs` | `(libs ...any)` | Strip symbols from absorbed static archives via `-Wl,--exclude-libs=` (Shared/Binary only, ELF only; accepts slices via `flattenAny`) |
| `SetSymbolBinding` | `(mode string)` | `"static"` → `-Bsymbolic`; `"static-functions"` → `-Bsymbolic-functions` (Shared/Binary only, ELF only) |
| `SetSymbolPrefix` | `(prefix string)` | Post-link `objcopy --prefix-symbols=` (fatal on double-set) |
| `SetKeepGlobalSymbols` | `(path string)` | Post-link `objcopy --keep-global-symbols=<path> {output}` and auto-registers `path` in `AddPostLinkDeps` (fatal on double-set) |
| `UseDependencyLinkerScript` | `()` | Auto-inherit linker script from dependency (`TargetBinary` only) |
| `AddPostLink` | `(tool string, args ...string)` | Post-link step; only the toolchain tools objcopy/size/objdump/nm/strip resolve (other names fail with "post-link tool not found"). Supports the `{output}` placeholder |
| `AddPostLinkOutputs` | `(paths ...string)` | Explicit extra outputs for missing-output rebuilds and automatic installation; supports `{output}`, SourceDir-relative paths and absolute paths. Hex/Bin/Strip helpers declare their outputs automatically; command arguments never imply outputs |
| `AddPostLinkHex` | `()` | `objcopy -O ihex {output} {output}.hex` |
| `AddPostLinkBin` | `()` | `objcopy -O binary {output} {output}.bin` |
| `AddPostLinkSize` | `()` | `size {output}` |
| `AddPostLinkStrip` | `()` | `strip -o {output}.stripped {output}` |
| `AddPostLinkDeps` | `(files ...string)` | Extra input files for post-link steps (SourceDir-relative); any content or permission change (a bare `touch` does not count) or a missing input → relink + re-run all post-link steps. Prebuilt targets with post-link steps are copied before modification and still relink when these inputs change |
| `AddBinHeader` | `(inputs ...any)` | Binary files → `.h` headers; output to `BuildDir()/generated/`; incremental via mtime and the input recorded in the generated header's first line |

`AddDeps` ref grammar (`pkg/api/depref.go`): a ref without `:` and `/` matches a target of the declaring package first, or if none exists and the name matches a required/known package in the build graph, expands to `<pkg>:*`; `<pkg>:<target>` selects one target; `<pkg>:*` or a `/`-containing path (e.g. `"official/zlib"`) expands to all targets of that package plus its transitive package deps (flat closure, deduplicated). The text before `:` is the package name; there is no literal `pkg:` marker. `t.UseRequires()` wires all active required packages of the declaring package (post-`FilterDeps`) as `<pkg>:*` deps automatically. Validation is fatal at declaration time for whitespace, multiple `:`, empty segments, and path-shape errors (leading/trailing `/` or `//`) in refs without `:`; an exactly empty entry is silently ignored. Unknown targets (`dependency not found`), unknown packages (`package not found in build graph`) and cycles fail at build-graph time. A package part without `/` in `<pkg>:<target>` is first resolved relative to the declaring (sub-)package.

`AddPublicIncludes`, `AddPublicDefines`, `AddPublicCFlags`, `AddPublicCxxFlags`, `AddPublicLdFlags`, and `AddPublicLinks` apply to the target itself and propagate transitively along `AddDeps` edges to dependents. Prefer these over `AddGlobal*` when flags/defines belong to a module or library rather than every package in the workspace (global flag changes rotate every package's `BuildDir`).

Source language is auto-detected from file extension (`.c` → C, `.cc/.cpp/.cxx/.C` → C++).

Sub-packages: a nested `build.go` in a native remote package's checkout is an independent package named `parent/sub`, versioned by the parent (no separate lockfile entry). Only native repos have sub-packages (registry wrappers never do — design decision DD-1; repository design docs are not shipped with this skill); they load lazily when depended on. Reference from outside by full name (`"subtest/mother/sub_a:*"`; list the parent before its sub-packages in `AddRequires`); inside a sub-package use short names for siblings (`"sub_b:utils_b"`). `DepOutput`/`DepBuildDir`, `BuildSubGraph` and `ImportConfig` use the same short-name rule. A needed sub-package requires its native parent to be needed too. A parent cannot require its own sub-packages in `OnRequire`. Example: `test_data/25_subpackage`.

### Removers

`RemoveCFlags`, `RemovePublicCFlags`, `RemoveCxxFlags`, `RemovePublicCxxFlags`, `RemoveLdFlags`, `RemovePublicLdFlags`, `RemoveDefines`, `RemovePublicDefines`, `RemoveIncludes`, `RemovePublicIncludes`, `RemoveLinks`, `RemovePublicLinks`, `RemoveDeps`, `RemoveProvidedLibs`, `RemoveInputs`, `RemoveOutputs` — each takes variadic `...any` (slices accepted via `flattenAny`), returns `*Target`. These perform immediate exact-match deletion from internal slices.

`RemoveFiles` takes variadic `...any` (like `AddFiles`) but uses **deferred pattern matching** — patterns are matched against glob-expanded paths at build time, not removed from the rule list. This lets you exclude files like `RemoveFiles("src/test_*.c")` after a broad `AddFiles("src/*.c")`.

### Key Getters

| Method | Returns |
|--------|---------|
| `Name()` | `string` |
| `Kind()` | `TargetKind` |
| `IsDefault()` | `bool` |
| `IsTest()` | `bool` |
| `IsLinkable()` | `bool` |
| `UsesRequires()` | `bool` |
| `Files()` | `[]string` |
| `ExcludedFiles()` | `[]string` |
| `Includes()` / `PublicIncludes()` | `[]string` |
| `Defines()` / `PublicDefines()` | `[]string` |
| `Links()` / `PublicLinks()` | `[]string` |
| `Deps()` | `[]string` |
| `CFlags()` / `PublicCFlags()` | `[]string` |
| `CxxFlags()` / `PublicCxxFlags()` | `[]string` |
| `LdFlags()` / `PublicLdFlags()` | `[]string` |
| `Inputs()` / `Outputs()` | `[]string` |
| `InstallDir()` | `string` |
| `NoInstall()` | `bool` |
| `BuildFunc()` | `func(p *Package) error` |
| `Prebuilt()` | `string` |
| `LinkerScript()` | `string` |
| `VersionScript()` | `string` |
| `ExcludeLibs()` | `[]string` |
| `SymbolBinding()` | `string` |
| `SymbolPrefix()` | `string` |
| `KeepGlobalSymbols()` | `string` |
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
| `AddGlobalCFlags(flags ...any)` | Add global C flags (`string`, `[]string` such as `ctx.If`/`ctx.Select`; effective in OnConfig and OnApply; only applied for packages that survive FilterDeps) |
| `AddGlobalCxxFlags(flags ...any)` | Add global C++ flags (same) |
| `AddGlobalDefines(defines ...any)` | Add `-D` preprocessor defines to both global C and C++ flags (same) |
| `AddGlobalLdFlags(flags ...any)` | Add global linker flags (same) |
| `AddGlobalLinks(links ...any)` | Add global link libraries (same) |
| `SetDefaultVisibilityHidden() *ConfigContext` | Add `-fvisibility=hidden` to this package (C+C++) and `-fvisibility-inlines-hidden` (C++ only); idempotent in OnConfig/OnApply, BuildScriptError without a package |

### BuildContext

| Method | Description |
|--------|-------------|
| `Target(name) *Target` | Get or create target |
| `GetTargets() map[string]*Target` | All targets |
| `SetDefaultFlags(cflags, cxxflags, ldflags []string)` | Set default compile/link flags for targets created afterwards |
| `PackageName() string` | Package name |
| `AddInstalls(src, dest)` | Install entry |
| `SetInstallFilter(filter)` | Install file filter |
| `BuildSubGraph(pkgName)` | Build package as independent sub-graph; no-op in dry-run |
| `DepOutput(depRef) string` | Get output path of dependency target (`<pkg>` or `<pkg>:<target>`; no same-package shorthand or `<pkg>:*` wildcard); returns `""` in dry-run |
| `DepBuildDir(depRef) string` | Get build directory of dependency target (same ref rules as `DepOutput`); returns `""` in dry-run or when `DepOutput` is empty |
| `SetProvidedLinkerScript(path string) *BuildContext` | Forward a linker script to the bound package (`SetProvidedLinkerScript`) |
| `BuildPath(elem...) string` | Join path elements under `BuildDir()` (or `$builddir` when `BuildDir()` is empty) |
| `WriteBuildFile(relPath, content) string` | Register a build-dir file write (`string` or `[]byte`) deferred until build execution; returns `BuildPath(relPath)` |
| `GenerateFile(relPath, fn) string` | Register a single-file generator callback deferred until build execution; returns `BuildPath(relPath)` |
| `Generate(fn) *BuildContext` | Register a generator callback deferred until build execution |
| `Exec(name, args...)` | Run command with logging (script error on failure) |
| `GenerateConfigHeader()` | Set `genConfigHeader = true`; propagated to `Package.SetGenConfigHeader(true)` — generates `autoconf.h` during scheduler build |
| `GenerateConfigDefines()` | Set `genConfigDefines = true`; on processing, reads `ImportConfigs()`, merges local + imported options, adds `-DCONFIG_*` defines to all targets |
| `ImportConfig(names...)` | Append package names to `importConfigs` list (merge + `-D` injection happens inside `GenerateConfigDefines`/`GenerateConfigHeader` processing) |
| `ImportConfigs() []string` | Get imported config package names |
| `SyncConfigDefines(names...)` | Shorthand for `GenerateConfigDefines` + `ImportConfig` (for parent/orchestrator packages) |
| `GenConfigDefines() bool` | Whether `genConfigDefines` was set |
| `GenConfigHeader() bool` | Whether `genConfigHeader` was set |
| `SetDryRun(v bool) *BuildContext` | Set dry run mode |
| `GetInstallItems() []InstallItem` | All install items |
| `GetInstallFilter() InstallFilterFunc` | Install file filter |

### InstallContext

| Method | Description |
|--------|-------------|
| `SetPrefix(prefix string)` | Per-package install prefix, applied when `--prefix` is not passed (prefer an absolute path) |
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
| `SourceDir() string` | Package source root (the downloaded working tree for remote packages) |
| `BuildDir() string` | Build output directory |
| `SrcDir() string` | Source code directory (differs from SourceDir when `SetGit()` is used) |
| `Run(name, args...)` | Run command in BuildDir (script error on failure; returns nothing) |
| `RunIn(dir, name, args...)` | Run command in specified directory (script error on failure; returns nothing) |
| `RunEnv(env, name, args...) error` | Run with custom environment (**returns real error**) |
| `Make(args...) error` | Run make in BuildDir with `pkg.Env()` (**returns real error**) |
| `PackageName() string` | Package name |

### RequireContext

Unconditional package dependencies can be declared directly via `p.AddRequires(deps...)` in `Main(p)`. `OnRequire` callbacks execute twice: first during graph discovery with nil config values (Phase 1), then again during FilterDeps with this package's `entries.<pkg>.options` values (Phase 3). Global configuration values are not merged into this second pass; unset reads fall back to declared defaults. This enables option-conditional dependencies. During discovery, direct value reads (`ctx.Bool/String/Int`) are build errors — use `ctx.When`/`ctx.WhenNot`/`ctx.WhenAny`/`ctx.If`/`ctx.Select`. Pass-2 requires **replace** this package's dependency edges: a dep whose guard is false in pass 2 is dropped. Adding a dep in pass 2 is accepted only when that package is already in the resolved graph; otherwise `FilterDeps` fails with `dependency ... not found in dependency graph`. Declare each dependency where pass 1 can see it — either unconditionally or behind a discovery-aware guard (`ctx.When`/`ctx.WhenNot`/`ctx.WhenAny`/`ctx.If`/`ctx.Select`); pass 1 evaluates `When`/`WhenNot`/`WhenAny` as true, `If` as its then-values, and `Select` as all mapped values, so guarded deps are still discovered.

| Method | Description |
|--------|-------------|
| `AddRequires(deps ...any)` | Add dependency (`"official/zlib >=1.2"`, `ctx.If(...)`, `ctx.Select(...)`, `[]string`) |
| `GetRequires() []RequireInfo` | All requires |
| `ResetRequires()` | Clear requires |

### ConfigAccessor (embedded by all context types)

Strict in script contexts (`OnConfig`/`OnBuild`/`OnInstall`/`OnClean`/`OnRequire`): reading an unknown option name, or using an accessor that mismatches the option's `SetType`, is a **build error** (not a silent zero value). During `OnRequire` discovery, direct reads (`Bool`/`String`/`Int`) are also build errors — use the discovery-aware `If`/`Select`/`When`/`WhenNot`/`WhenAny`. TUI/CLI read paths stay lenient.

| Method | Signature | Description |
|--------|-----------|-------------|
| `Bool` | `(name string) bool` | Get bool value (strict: only for OptionBool) |
| `String` | `(name string) string` | Get string value (strict: OptionString/OptionChoice) |
| `Int` | `(name string) int` | Get int value (strict: OptionInt; coerces float64/int64) |
| `BoolStr` | `(name string) string` | Returns "ON"/"OFF" |
| `SetCfgVals` | `(vals map[string]any) *ConfigAccessor` | Replace the raw configuration values |
| `SetOptions` | `(options map[string]*Option) *ConfigAccessor` | Replace the option map |
| `MergeGlobals` | `(globalOptions map[string]*Option, globalVals map[string]any)` | Merge global options/values into this accessor |
| `If` | `(option string, then ...string) []string` | `then` values if the bool option is true; returns `nil` when false. When config is nil (discovery) returns `then` unconditionally; when unset, falls back to the declared default. Bool options only — other types are a build error |
| `Select` | `(option string, mapping map[string]string) []string` | Map option value to `[]string{mapped}` (or `nil` when unmapped/empty); when config is nil (discovery), returns all unique non-empty values in `mapping` sorted so `AddRequires(ctx.Select(...))` discovers every branch. In strict mode on `OptionChoice`, keys not in `SetValues` are a build error |
| `When` | `(option string, values ...any) bool` | True when the option matches any supplied value (numerics across int/float64); requires at least one value; falls back to declared default when unset; returns `true` when config is nil (discovery) |
| `WhenNot` | `(option string, values ...any) bool` | Discovery-safe inverse compare; requires at least one value; returns `true` when config is nil (discovery), otherwise `!When(option, values...)` |
| `WhenAny` | `(option string, values ...any) bool` | Discovery-safe multi-value compare: returns `true` when config is nil (discovery), otherwise `true` if the option matches any of `values` |
| `Option` | `(name string) *Option` | Get or create option. Intended for `OnConfig`: `BuildContext`, `InstallContext`, and `CleanContext` reject declaration after the config phase (build error). `ConfigContext`, `RequireContext`, and `*Package` do not enforce this, but a late declaration never participates in value resolution — reads fall back to its default/zero value |

---

## Option

All setters are fluent (return `*Option`).

| Method | Signature | Description |
|--------|-----------|-------------|
| `SetType` | `(t OptionType)` | Bool/String/Int/Choice (optional when `SetDefault` or `SetValues` is called) |
| `SetDefault` | `(v any)` | Default value; auto-infers `OptionType` (`bool` → `OptionBool`, `string` → `OptionString`, `int` → `OptionInt`) when `SetType` was not explicitly set |
| `SetDescription` | `(desc string)` | Description |
| `SetValues` | `(vals ...string)` | Choice values; auto-infers `OptionChoice` when `SetType` was not explicitly set; defaults must be among them |
| `SetShowIf` | `(fn func(ctx *ConfigContext) bool)` | Conditional visibility |
| `SetOnApply` | `(fn func(ctx *ConfigContext, val any))` | Callback after option values resolved, once per build per package, in sorted option-name order. `val` is normalized for `OptionInt` only (JSON `float64`/`int64` converted to `int`); other types are passed through unchanged. The callback's context carries real option values, so reading other options works |
| `SetGroup` | `(group string)` | Display group; `GroupGlobal` also makes the option global (`IsGlobal()` is `group == GroupGlobal`) |
| `SetMacroName` | `(name string)` | Override the generated `CONFIG_<NAME>` macro name on both package options (`GenerateConfigDefines`/`GenerateConfigHeader`) and global options (auto-exported); a `%s`/`%v` in the name renders the option value (`PY32F539xx%s` + `M` → `PY32F539xxM=1`) |

Getters: `Name()`, `Type()`, `Default()`, `Description()`, `Values()`, `ShowIf()`, `OnApply()`, `Group()`, `MacroName()`, `IsGlobal()`, `MacroDefines(val any) ([]string, error)`.

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

	func (k TargetKind) Ext() string          // host OS extension; use ExtFor(targetOS) for cross targets
	func (k TargetKind) ExtFor(targetOS string) string
	func (k TargetKind) Prefix() string       // host OS prefix; use PrefixFor(targetOS) for cross targets
	func (k TargetKind) PrefixFor(targetOS string) string
	func (k TargetKind) InstallDir() string   // "bin" (binary), "lib" (static/shared), "" otherwise

	func TargetFilename(kind TargetKind, name, targetOS string) string

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
	const ModeSize            = "size"

	const TargetOSOptionName     = "target_os"
	const TargetTripleOptionName = "target_triple"
	const BuildDirToken          = "$builddir"
	const GroupGlobal            = "Global"

	var BuiltInGlobalOptions = map[string]*Option{...}

Cross-compilation projects declare the platform through these global options; artifact names then follow the target OS (`TargetFilename`, `ExtFor`, `PrefixFor`). A toolchain may declare `target_os`/`target_triple` defaults in `toolchain.json`; precedence is package entry value → package-local non-global option default → project global config/default → toolchain default, and empty values count as unset:

```go
ctx.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString)
ctx.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString)
```

---

## Types

	type PkgDirs struct { SourceDir string; BuildDir string; InstallDir string }

	type Platform struct { OS string; Triple string }
	func (p Platform) OSOrHost() string

	type DefaultFlags struct { CFlags, CxxFlags, LdFlags []string }

	type DepRef struct { Raw, Pkg, Target string; Wildcard, Qualified bool }

	type BuildScriptError struct { Package, Op string; Err error }
	func (e *BuildScriptError) Error() string
	func (e *BuildScriptError) Unwrap() error

	type InstallItemHolder struct { ... }
	func (h *InstallItemHolder) AddInstalls(src, dest string) *InstallItemHolder
	func (h *InstallItemHolder) GetInstallItems() []InstallItem
	func (h *InstallItemHolder) SetInstallFilter(filter InstallFilterFunc) *InstallItemHolder
	func (h *InstallItemHolder) GetInstallFilter() InstallFilterFunc

	type TargetRegistry struct { ... }
	func (r *TargetRegistry) Target(name string) *Target
	func (r *TargetRegistry) GetTargets() map[string]*Target
	func (r *TargetRegistry) SetDefaultFlags(cflags, cxxflags, ldflags []string)

	type Requires struct { ... }
	func (r *Requires) Add(deps ...any)
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
	func NewConfigAccessor(cfgVals map[string]any, options map[string]*Option) ConfigAccessor
	func NewTargetRegistry() *TargetRegistry
	func NewRequireContextForConfig(pkgName string, cfgVals map[string]any, options map[string]*Option, funcs []RequireFunc) *RequireContext

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

### Build Mode and Defaults

	func GetModeFlags(mode string) (cflags []string, defines []string) // debug: -O0 -g; release: -O2 + NDEBUG; size: -Os + NDEBUG
	func DefaultBuildFlags(toolchainName, targetOS string) DefaultFlags // host-only defaults; empty for other toolchains

### Options and Dependency Refs

	func NormalizeOptionValue(opt *Option, val any) any
	func ValidateOption(o *Option) error
	func ParseDepRef(ref string) (*DepRef, error)
	func SubPackageCandidates(currentPkg, depName string, subParents map[string]string) []string

### Config Export & Merge

	func ConfigToDefines(options map[string]*Option, cfgVals map[string]any) []string
	func ConfigToHeader(options map[string]*Option, cfgVals map[string]any) string
	func WriteConfigHeader(dir string, content string) error
	func MergeImportedOptions(localOpts map[string]*Option, localVals map[string]any, pkgs []*Package) (map[string]*Option, map[string]any)
	func MergeGlobalOptions(allDefs map[string]map[string]*Option, toolchainList []string) (map[string]*Option, error)

### Semver

Version format: `[v]MAJOR[.MINOR][.PATCH][-PRERELEASE]` (e.g. `1.2.3`, `v2.0`, `1.0.0-rc.1`).

```text
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

`>=` and `>` with constraint major > 0 restrict to the same major version (`>=1.2` and `>1.0` won't match `2.0.0`); `>=0.x` / `>0.x` are not major-locked. An empty constraint parses as `>=0.0.0`, but prerelease versions still match only when the constraint pins a prerelease of the same `major.minor.patch`; an empty constraint therefore does not match a prerelease-only list. Selection: highest version satisfying all constraints (mutually satisfiable across packages). Pre-release: `1.0.0-rc.1 < 1.0.0`; numeric identifiers < alpha identifiers in pre-release segments.
