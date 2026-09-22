# VMake - AGENTS.md

VMake: C/C++ build system using Go buildscripts. AI coding agents working in this codebase should follow these guidelines.

## Build / Lint / Test

Requires Go 1.26+ (see `go.mod`).

Build vmake with `CGO_ENABLED=0`, including Windows cross-compiles. Build scripts and plugins use yaegi and do not require CGO.

```bash
CGO_ENABLED=0 go build -o vmake ./cmd/vmake    # Build
gofmt -w .                       # Format
```

### Lint and unit tests — scope to vmake's own packages

**Do NOT run bare `go vet ./...` or `go test ./...`** — they walk into `test_data/`, which contains C source files inside Go packages, and fail with "C source files not allowed when not using cgo". Always scope to vmake's own code:

```bash
go vet ./cmd/... ./pkg/... ./internal/...      # Lint
go test ./cmd/vmake/... ./pkg/... ./internal/... # Unit tests
```

Go tests live in `cmd/vmake`, `pkg/api`, `pkg/build`, `pkg/buildscript`, `pkg/config`, `pkg/plugin`, `pkg/pipeline`, `pkg/repo`, `pkg/resolver`, `pkg/tui`, `internal/scriptfs`.

### Integration tests via `test_data/` (run each from its own directory)

```bash
# Single test
cd test_data/01_simple_c && ../../vmake build

# Run all test_data integration tests (01-16, 18-25; 17 is NOT in test_data — see below; 25 needs `sh test_data/25_subpackage/setup.sh` once)
for d in test_data/0[1-9]_*/ test_data/1[0-9]_*/ test_data/2[0-5]_*/; do (cd "$d" && ../../vmake build) || break; done
```

Never build `test_data/` from the parent directory — each sub-project must run from its own dir.

Firmware test (17) lives in `test_linux/17_firmware` (NOT `test_data/`), tests stamp skip, KConfig presets, EnsureConfig:
```bash
cd test_linux/17_firmware && ../../vmake build
```

### Snapshot tests (`test_data/_snapshot/` — separate Go module)

Golden-file tests that build every test_data project from clean and hash the results. Preferred verification for build-behavior changes — much faster feedback than the manual loop above:

```bash
go build -o vmake ./cmd/vmake                  # REQUIRED first: tests invoke ./vmake at repo root
cd test_data/_snapshot && go test              # compare against baselines
go test -run TestSnapshotsTestData/01_simple_c # single project
go test -update                                # regenerate baselines after intended changes (or VMAKE_SNAPSHOT_UPDATE=1)
```

- Snapshotted per project (`build --install`): `install/` tree SHA256 hashes (paths normalized to `<ROOT>`/`<HOME>`), redacted `manifest.json`, `build/compile_commands.json` (compile flag drift detector)
- `TestSnapshotsTestLinux` also covers `test_linux/17_firmware` (skipped if absent)
- Skipped projects: 07, 08, 09, 10 (codegen/network-dependent)
- Baselines are per-OS: `baseline/` on Unix, `baseline-windows/` on Windows (`baselineDirName()`). Artifact names, path separators and `compile_commands.json` contents are OS-specific, so a shared baseline is not possible; generate the Windows set once with `go test -update` on Windows
- Projects 06, 11, 13 and 25 need a C++ frontend, a registered registry repo, or `test_data/25_subpackage/setup.sh`; they fail in environments lacking those, independently of vmake
- On drift: intended change → `go test -update`; unintended regression → fix before committing

Known pre-existing integration failures (ignore): none — all test_data tests currently pass.

Notable test purposes: 15=`subgraph_siblings`, 16=`subgraph_cross_tc`, 18=`config_header` (GenerateConfigHeader), 19=`config_defines` (GenerateConfigDefines), 20=`config_propagate` (ImportConfig/cross-package config), 21=`root_package`, 22=`version_script`, 23=`link_strategy`, 24=`symbol_prefix`, 25=`subpackage` (native sub-package discovery/short-name refs; run `sh test_data/25_subpackage/setup.sh` once first — registers local native repo `subtest`).

## Development Mode

Buildscripts AND extension plugins are interpreted by yaegi (Go interpreter) — no `go build -buildmode=plugin`, no `.so` files, no `VMAKE_DIR` environment variable.

```bash
go build -o vmake ./cmd/vmake    # Build vmake itself
```

## Platform Support

vmake compiles and runs on Unix and natively on Windows. Windows-hosted ARM bare-metal builds use the Windows ARM GNU toolchain to produce ARM ELF, binary and hex artifacts. Cross-compile vmake itself with `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o vmake.exe ./cmd/vmake`. The pieces that matter:

- **`internal/gitusr`** — Windows only. Locates the Git for Windows installation (`git --exec-path`, falling back to deriving it from the resolved `git.exe`) and prepends `<root>\usr\bin` and `<root>\<msystem>\bin` to the **process** `PATH`, so `sh`, coreutils, `sed`/`awk`/`grep`/`find`, `tar`, `unzip` and `curl` resolve. The default installer only adds `Git\cmd` to `PATH`, so these tools exist on disk but are invisible to `exec.LookPath`. Called once from `cmd/vmake/main.go` before `loadPlugins()`; `os.Setenv` is not goroutine-safe, so it must stay first.
- **`internal/gitcmd`** — every git invocation goes through `gitcmd.Args()`, which forces `core.autocrlf=false`, `core.eol=lf` and `core.longpaths=true` (plus `core.symlinks=true` only when this process can actually create symlinks). The Git for Windows installer defaults `autocrlf` to true; leaving it would rewrite line endings in `~/.vmake/cache` checkouts and break content hashes (`stamp.go`) and `git apply` patch series.
- **`internal/flock`** — `syscall.Flock` on Unix, `LockFileEx`/`UnlockFileEx` (`golang.org/x/sys/windows`) on Windows. The platform layer owns a `lockState` because the `OVERLAPPED` must outlive the lock. Lock files live in `~/.vmake/cache/_locks/`, outside the directories they guard, so they are never deleted while held.
- **Symlinks — no junction fallback.** The storage layout is symlink-based (`vmake_deps/<repo>/<pkg>/src`, `<versionDir>/src|out`, `SetGit` sources, prebuilt outputs). On Windows `os.Symlink` needs Developer Mode or elevation; `internal/fs.EnsureSymlink` decorates the failure with that hint and `fs.SymlinksSupported()` probes the capability once per process. Junctions were rejected: Go sets `ModeSymlink` only for `IO_REPARSE_TAG_SYMLINK`, not for `IO_REPARSE_TAG_MOUNT_POINT`, so `filepath.EvalSymlinks` would not resolve a junction and `filepath.Walk` would not descend into one — silently breaking `Resolver.scanSubPackages` (`pkg/resolver/resolver.go`). Supporting junctions would mean replacing that `EvalSymlinks` and auditing every `Walk` root.
- **Target OS, not host OS.** Toolchain definitions must declare `target_os`; ARM bare-metal uses `none`, embedded Linux uses `linux`, and PE uses `windows`. `Toolchain.TargetTriple`/JSON `target_triple` describes the compiler target (for example `arm-none-eabi`); the old `Host`/`host` field and `Package.CrossTarget()` are removed. Scripts use `Package.TargetTriple()`. `api.TargetKind.ExtFor/PrefixFor` and `api.TargetFilename` decide target artifact names: `.exe`/`.dll` for PE targets, no binary extension/`.so` otherwise. Low-level `TargetOSOf(nil)` and unset in-memory toolchains retain host defaults, but selected toolchains reject missing `target_os`. `Toolchain.Tools.MAKE` (`MakeTool()`) names the make program.
- **Host-specific installations.** `toolchain.json` has `installations` keyed by host OS/architecture (`linux/amd64`, `windows/amd64`) instead of the old single `install` field. Each entry declares its archive and exact `root_dir`; `.` means files are at the archive root. Unsupported hosts fail explicitly. Install directories are `~/.vmake/toolchains/<host-os>/<host-arch>/<name>/<version>/`, so Linux and Windows archives cannot reuse one another's compiler files. The `vmake-tools` ARM profile selects its Linux `.tar.xz` or Windows `.zip` Git LFS archive automatically and declares `target_triple=arm-none-eabi`, `target_os=none`.
- **Configured tools resolve strictly.** An explicit absolute tool path must exist; a relative tool with `InstallPath` resolves under that installation's `bin`, with no PATH fallback. Windows resolution preserves the actual `.exe` path. Required compiler/archive/linker tools and every configured optional tool are validated. `Package.Env()` exports raw resolved paths without changing the shared toolchain. Make/Configure quote tool paths in their shell environments; Make also escapes dollar signs for make expansion. EnsureConfig and CleanContext.Make share the Make path. Explicit MAKE is resolved the same way, while unset MAKE uses the host's `make`. Toolchain build identity includes host OS/architecture, toolchain configuration, resolved paths and compiler versions.
- **PE linking.** `Linker.LinkShared` adds `-Wl,--out-implib=<output>.a` for PE targets; `collectDepArtifacts` links consumers against that import library (`libfoo.dll.a`) rather than the DLL, `installTarget`/`publishTarget` copy it into `<prefix>/lib` (`importLibraryPath`), and `needRelink` re-links when it is missing. The import library rides as a **normal group input, never inside `-Wl,--whole-archive`** (`wholeArchiveInput` excludes `.dll.a`) — whole-archive on an import library force-imports every DLL export. Prebuilt shared targets ship only the declared file, so consumers link the DLL itself. `-pie`, `-Wl,--as-needed` and `-Wl,-z,relro,-z,now` are ELF-only and are omitted for PE; the default flags are chosen per target OS in `pkg/toolchain/flags.go`.
- **Paths handed to MSYS binaries are slash-separated.** The MSYS runtime de-quotes backslashes in argv, and autoconf derives `srcdir` from `$0` via `dirname`, which only understands `/`. `p.Configure` (script path and `--prefix=`), `p.Make`'s `-C`, the TUI's `-C`, `tar -C` and `curl -o` all pass `filepath.ToSlash` paths — a no-op on Unix. Any new argument forwarded to `sh`/`tar`/`curl`/MSYS `make` must do the same; `cmd.Dir` (native CreateProcess) correctly keeps backslashes.
- **Windows compiler arguments.** GNU compiler, linker and archive invocations use temporary response files with individually quoted and escaped arguments. Path arguments are made relative to the command working directory where possible and slash-separated. `compile_commands.json` retains the expanded `arguments` array, not a reference to a deleted response file. Command failures retain the original program, arguments and underlying error.
- **Assembly inputs.** `.s` selects the compiler's assembler mode; `.S` selects assembler-with-cpp and saves the preprocessed assembly while collecting dependencies. Assembler `.include` dependencies and `.S` preprocessor dependencies are merged into the target depfile, so edits to either included source cause recompilation. Windows drive letters and escaped spaces are handled when parsing depfiles. Clang uses external GNU assembler; `.S` preprocessing writes an object-specific intermediate file and merges both dependency lists. Existing toolchain flags select the compiler target and assembler search path; there is no automatic assembler fallback. Clang outputs must be COFF for Windows or relocatable ELF for Linux/bare-metal. Clang bare-metal is not part of the validated support; Windows ARM uses GNU ARM GCC.
- **CMake.** Prefer `CMakeConfigure`, `CMakeBuild`, and `CMakeInstall` for CMake projects. Keep only project options and special steps in build.go; call cmake directly only for operations the API cannot express. All three helpers use `CMakeBuildDir()` (default `BuildDir()/cmake`) and `CMakeInstallDir()` (remote `InstallDir()`, local `BuildDir()/staging`). Directory setters resolve relative paths against `BuildDir()` without changing `PkgDirs` or stamp/publication rules. `SetCMakeBuildType` sets the shared default configuration, otherwise Debug/Release follows VMake mode. Build/install accept explicit `--config`; raw directory, install-prefix and default-build-type overrides must use the setters. CMake receives resolved compiler/binutils paths (ASM uses CC, `Tools.LD` is not mapped to `CMAKE_LINKER`) and a system name based on the target OS. `none` uses `Generic`, static-library compiler checks, host program search and target-only library/include search; projects explicitly supply the processor when needed. Global C/CXX and executable/shared/module linker flags are automatic, explicit flags override them, and toolchain `DefaultFlags` are not added. ASM flags are explicit. Windows defaults to Ninja unless a generator or configure preset is explicit. Configure presets are supported; build presets are rejected because their binaryDir overrides the managed build directory (use --target/--config for building). Build defaults to CPU-count parallelism unless a parallel argument or `CMAKE_BUILD_PARALLEL_LEVEL` is set. The helpers preserve independent argv, normalized paths and dry-run behavior, without resolving default make in advance.
- **Kconfig.** `SetMenuconfigCmd(program, args...)` stores the executable and arguments separately and runs them in `SrcDir`; `MenuconfigCmd()`/`MenuconfigArgs()` return those values. The default is the selected make with `menuconfig`. Preset generation always uses the selected make independently of any custom menuconfig program.
- **ELF-only symbol management is rejected, not ignored.** `LinkPolicy.Validate()` fails the build when `SetVersionScript`, `AddExcludeLibs` or `SetSymbolBinding` is used on a PE target.
- **`check-symbols` follows artifact capability.** On either host, it reads ELF dynamic symbols with the selected toolchain's `nm -D`. PE files and ELF files without a dynamic symbol table report that the audit is not applicable. PE export analysis is deliberately not implemented.
- **`vmake doctor`** reports symlink capability, the Git userland, selected make and the selected toolchain's configured tools. It reads the project selection or `--toolchain NAME`; an ARM build does not require unrelated native GCC tools. `vmake test` refuses bare-metal/cross-toolchain execution with a `vmake build --tests` hint.

Prerequisites for Windows ARM builds: Git for Windows (full installer, including Git LFS for LFS archives), the Windows ARM GNU toolchain and Developer Mode or elevation for symlinks. Native Windows builds instead need a native toolchain such as MinGW-w64. Install Ninja/CMake or make when the build scripts use them; Git for Windows supplies neither a C compiler nor make. `unzip` is not needed: `ExtractToDir` unpacks `.zip` with `archive/zip` inside the destination root and rejects escaping entries and symlink entries. The installer applies the declared archive root explicitly. Windows tar extraction uses `--force-local` for drive-letter paths.

## Storage Layout

### Project-Local (`vmake_deps/`)
Auto-added to `.gitignore` on first build via `ensureGitignore()` in `resolveToConfig()`. Each project has its own independent `vmake_deps/`.

```
vmake_deps/
  <repo>/<pkg>/src    # Symlink -> ~/.vmake/cache/<repo>/<pkg>/<version>/src
  <repo>/<pkg>/out    # Symlink -> ~/.vmake/cache/<repo>/<pkg>/<version>/out
```

`.vmake/vmake.lock` pins remote versions+commits for reproducible builds (`vmake lock update` re-resolves). Commit it alongside `.vmake/config.json`. With a valid lock entry, native version resolution reads local refs tags only — no network. Refs are refreshed (git fetch) only when resolving fresh (no lock entry) or in `vmake lock update` mode.

`findProjectDir()` (in `cmd/vmake/paths.go`) walks upward from cwd to find `.vmake/` or `build.go` to locate the project root (fatal when none found; `findProjectDirSoft()` is the non-fatal variant used by `cleanupLegacyStorage`).

### Global (`~/.vmake/`)
- `~/.vmake/repos/` — registry repo clones
- `~/.vmake/toolchains/<host-os>/<host-arch>/<name>/<version>/` — host-specific compiler installations
- `~/.vmake/extensions/` — extension repos
- `~/.vmake/cache/<repo>/<pkg>/<version>/src` — immutable per-version source checkouts (temp-clone + atomic rename; never mutated in place)
- `~/.vmake/cache/<repo>/<pkg>/<version>/out/<buildKey>/` — shared binary cache (build/install staging, shared across projects)
- `~/.vmake/cache/_localgit/<sha256(url)>/src` — shared clones for local `SetGit` packages
- `~/.vmake/cache/_locks/<repo>_<pkg>.lock` — lock files OUTSIDE the guarded package dirs (never deleted; deleted-in-place locks broke mutual exclusion)
- `~/.vmake/config.json` — `trustedRepos` (remote-script trust)

`BuildKey` hashes toolchain+mode+options+version+commit+`GlobalFlagsHash()`. `SourceManager.EnsureVersion()` materializes version dirs; per-package build locks serialize shared-out access. `VMAKE_CACHE` overrides the cache root; `VMAKE_FETCH_TIMEOUT` (seconds) the git fetch timeout; `VMAKE_TRUST_ALL=1` bypasses trust gating (CI). Legacy `~/.vmake/sources` and stale `vmake_deps/` are auto-removed on first run (layout marker `.vmake/layout`).

## Core Concepts

- **Repo** — Package source. **Registry** (wraps third-party C/C++ libs, build.go is a wrapper) vs **Native** (vmake-native, standalone git repo, build.go at root, version from git tag).
- **Package** — Build unit described by `build.go`. Callbacks: `OnRequire` (declare deps), `OnConfig` (configure options), `OnBuild` (define targets), `OnClean` (custom clean).
- **Target** — Build artifact: `TargetBinary`, `TargetStatic`, `TargetShared`, `TargetObject`, `TargetVoid`.

## Key Design Decisions

### Everything is a Package
uboot, kernel, busybox, app, partitions, firmware are ALL packages with the same lifecycle. No special-casing.

### No Fallbacks — Fix the Root Cause
When a function receives wrong input, fix the caller. Never add fallback chains or `if x == "" { x = y }` guards to hide bugs.

### Local vs Remote Unification
The build pipeline treats local and remote packages identically for target scheduling, compilation and linking. `IsLocal()` distinguishes only: directory resolution (`makeLocalPkgDirs` vs `makeRemotePkgDirs`), where sources are materialized (remote = immutable version dir in the global cache, local = mutable checkout), and how patches apply (remote = content-addressed `EnsurePatched` clone; local = in-place).

### SrcDir vs SourceDir vs BuildDir
- `SourceDir`: package root (where build.go lives)
- `SrcDir()`: source code directory (may differ if `SetGit()` downloads source to `<SourceDir>/src/`)
- `BuildDir`: local packages use `<SourceDir>/build/<key>/`; remote packages use `<depsDir>/<name>/out/<key>/build/`
- `pkg.Make()` uses BuildDir. When Makefile is in SourceDir, use `pkg.RunIn(srcDir, "make", ...)`
- CMake helpers use `CMakeBuildDir()` and `CMakeInstallDir()`. Keep CMake-produced archives separate from VMake's `SetPrebuilt` symlink destinations; use the getters for artifact paths.

### KConfig Preset = Make Target Name
Preset files under `configs/` are partial configs (defconfig format), NOT complete `.config`. The preset name is passed to `make <preset>` to generate `.config`. Lifecycle: TUI select → save name to config.json → on build: check `.config` → if missing, `make <preset>` → build.

### Stamp-Based Skip for Void Targets
Void targets with a BuildFunc and no InstallDir record `.vmake_stamp` (JSON) in BuildDir after a successful run: `config_hash` = SHA256 over the contents of `SetConfigFiles()` files (missing files are skipped by the hash, so deletion counts as a change) + `source_rev` = git HEAD of SrcDir. Stale when: stamp missing/corrupt, config file contents changed, git HEAD changed, or a dependency artifact is newer than the stamp (`isVoidUpToDate` + `depArtifactsNewer` in `pkg/build/scheduler.go`; hash/rev logic in `pkg/build/stamp.go`).

### Post-Link Incremental Tracking
`target.AddPostLinkDeps(files...)` declares extra input files (SourceDir-relative, like `AddFiles`) that post-link steps consume. The relink check (`needRelink` in `pkg/build/scheduler.go`) compares each dep's mtime against the link output: any dep newer or missing → relink + re-run ALL post-link steps. Without this, post-link inputs (e.g. `objcopy --keep-global-symbols=file.sym`) are invisible to staleness — editing `file.sym` is silently skipped, forcing `rebuild`/`distclean`. Granularity is whole-target: one dep change → relink + full post-link re-run (no per-step incremental). Dep deletion also triggers relink (mirrors `SetConfigFiles` void-target semantics). Applies to Binary/Shared/Object (Prebuilt short-circuits before `needRelink`, so `AddPostLinkDeps` is a no-op there). Diagnostic on trigger: `RELINK <name> (post-link dep <file> newer|missing)` via `vlog.Info`.

`target.AddPostLinkOutputs(paths...)` explicitly declares extra post-link outputs; `PostLinkOutputs()` returns their templates. The scheduler and installer expand `{output}` and resolve relative paths against SourceDir. Missing declared outputs trigger relinking and all post-link steps, even if the main ELF is current. Hex/Bin/Strip helpers automatically declare their outputs; Size/SymbolPrefix do not. Command arguments never imply outputs, and the primary artifact is not counted twice. Custom steps must declare outputs to obtain missing-output rebuilds and automatic installation.

### EnsureConfig + ApplyKConfigPatches Abstraction
`pkg.EnsureConfig(srcDir) bool` checks `.config` existence + size > 0, runs `make <preset>` if needed. `ApplyKConfigPatches(configPath, patches)` applies post-defconfig patches in EnsureConfig, restoreKConfigFiles, and TUI ensureConfigCmd.

### Abstraction Boundaries
- `restoreKConfigFiles` only restores from config.json — does NOT call `make <preset>`
- `make <preset>` stays in `EnsureConfig` (build) and `ensureConfigCmd` (TUI)
- Only abstract `.config` check (`EnsureConfig`), NOT a full `BuildKConfigMake` wrapper

### Toolchain Definition Errors

Toolchain manifests are registered independently. Malformed/legacy definitions and unavailable host installations retain their original errors; healthy definitions remain usable. Known error names fail selection and query, while errors without a reliable JSON name are identified by manifest path. `toolchain list` displays errors, and TUI retains an already selected invalid name instead of replacing it. Built-in `ext` commands skip plugin execution so update/remove remain available. Unset MAKE is resolved only for actual make operations; explicitly configured MAKE stays strictly validated.

### Double-Set Protection
`SetLinkerScript`, `SetProvidedLinkerScript`, `SetVersionScript`, and `SetSymbolPrefix` panic with `*BuildScriptError` on second invocation — cannot silently overwrite. Consistent with the "No Fallbacks" principle. `OnPackage` and `AddKConfig` are also single-slot (second registration fatals).

### Symbol Management (Five Layers)
- `ctx.SetDefaultVisibilityHidden()` adds `-fvisibility=hidden` to the declaring package's C+C++ flags, `-fvisibility-inlines-hidden` to its C++ flags only. It is idempotent and valid in `OnConfig`/`OnApply`; a context without a package raises `BuildScriptError`. Dependencies retain their own visibility policy. Explicit `AddGlobalCFlags`/`AddGlobalCxxFlags` remain global, including explicit hidden flags. `Package.DefaultVisibilityHidden()` and `VisibilityFlags()` expose the package policy; native compilation, `MergedCFlags`/`MergedCxxFlags`, and CMake place it before explicit flags. Package visibility participates in that package's build key.
- `target.SetVersionScript("file.map")` valid only on `TargetShared`/`TargetBinary` — scheduler returns error otherwise. Path resolved against package SourceDir. Adds `-Wl,--version-script=` to link command
- `target.AddExcludeLibs("libfoo")` adds `-Wl,--exclude-libs=` (appends; the old `SetExcludeLibs` name was removed). GNU ld quirk: matches the full archive basename minus `.a`, so use `libfoo` form (with `lib` prefix), not `foo`
- `target.SetSymbolBinding("static"|"static-functions")` adds `-Wl,-Bsymbolic` or `-Wl,-Bsymbolic-functions`
- `target.SetSymbolPrefix("pfx_")` appends post-link `objcopy --prefix-symbols=pfx_` step. Implemented via existing AddPostLink mechanism
- `LinkShared` strips `-pie`/`-no-pie` from ldflags (incompatible with `-shared`)
- `LinkPolicy` struct in `pkg/build/linker.go` carries VersionScript/ExcludeLibs/SymbolBinding across scheduler→linker boundary

### Config Cross-Package Propagation
- `GenerateConfigDefines()` sets `genConfigDefines = true` on BuildContext; during build processing, reads `ImportConfigs()`, calls `MergeImportedOptions` to merge local + imported options, then calls `ConfigToDefines` and `AddDefines` on all targets
- `ExportConfig()` sets `exportConfig = true` on BuildContext; propagated to `Package.SetExportConfig(true)` in `applyBuildContextConfig` (`pkg/pipeline/pipeline.go`)
- `ImportConfig(names...)` appends package names to `importConfigs []string` on BuildContext; the actual merge and `-D` injection happens inside the `GenerateConfigDefines` block of `applyBuildContextConfig`
- `SyncConfigDefines(names...)` = `GenerateConfigDefines` + `ImportConfig` (convenience for orchestrator packages)
- `GenerateConfigHeader()` sets `genConfigHeader = true` on BuildContext; propagated to `Package.SetGenConfigHeader(true)` — generates `autoconf.h` from merged config options when called by scheduler
- Merged options: local options take priority over imported (no overwrite on name collision)
- `autoconf.h` does NOT propagate across packages — only `-D` defines do
- **Public headers must not `#include "autoconf.h"`** — it's package-local only

### Prebuilt Targets (`SetPrebuilt`)
`Target.SetPrebuilt(path)` marks a `TargetStatic`/`TargetShared`/`TargetBinary` as pre-compiled. The scheduler skips compilation and creates a symlink from the expected output path to the prebuilt file. Up-to-date check compares symlink target via `os.Readlink`. Source file existence is verified before symlink creation.

### Option OnApply Callback
`Option.SetOnApply(fn)` registers a callback invoked after all options are resolved. The callback receives `val any` — normalized to the option's declared type (`bool` for OptionBool, `int` for OptionInt, `string` for OptionString/OptionChoice; `api.NormalizeOptionValue` converts JSON `float64` before the call). The context carries real option values (CfgVals from config.json + all declared options), so reading other options inside the callback works. Callbacks run in sorted option-name order, during config phase, after option values are finalized.

### Strict Config Accessors
Script-facing contexts (Config/Build/Install/Clean/Require) have strict accessors: reading an unknown option name, using an accessor that mismatches the option's `SetType`, or reading values directly during OnRequire discovery (`ctx.Bool` etc.) panics with `*BuildScriptError`. OnRequire must use the discovery-aware helpers (`ctx.When`, `ctx.If`, `ctx.Select`). TUI/CLI accessors remain lenient (`NewConfigAccessor` without `setStrictOwner`).

### Dependency Linker Script
A package declares `ctx.SetProvidedLinkerScript("path/to/script.ld")` in `OnConfig`. A consumer target calls `.UseDependencyLinkerScript()` — at link time, the scheduler resolves the first dependency that provides a linker script and passes `-T` to the linker. `SetProvidedLinkerScript` may only be called once per package (fatalScript panic on double-set).
### Sub-Packages (Native Repos Only)
Nested `build.go` inside a **native** remote package checkout becomes a sub-package: independently loaded, named `parent/sub`, own options/targets/build dirs, version follows the parent (no separate lockfile entry). Lazy: build.go interpreted only when depended on. Registry wrapper packages deliberately have NO sub-packages; a parent cannot require its own sub-packages in `OnRequire` (scan runs after dep resolution); local projects have no sub-package concept. Recorded decisions and known limitations: `docs/DESIGN_DECISIONS.md` (DD-1..DD-3). Integration test: `test_data/25_subpackage`.

### Auto-Wire Require → Build Deps (REMOVED in v2)

**Historically**: `OnRequire`/`AddRequires` declared package-level deps, and
`autoWireRequireDeps()` (in `build_cmd.go`) silently auto-added `AddDeps` edges
to any target whose package had `AddRequires` calls but no explicit `AddDeps`.

**v2**: `autoWireRequireDeps` was REMOVED (violates No-Fallbacks principle).
Each target must declare its build-graph edges explicitly via `AddDeps`.

Run `vmake doctor` to detect build.go files that still rely on the old
auto-wire behavior.

### restoreKConfigFiles Skip Rules
- No config.json entry for package → skip entirely (don't delete `.config`)
- config.json has entry but kconfig empty (preset switch) → delete `.config`
- config.json has kconfig content → write only if content changed (avoid mtime update invalidating stamp)
- Empty kconfig content → don't write anything

## Runtime Execution Flow

### Build Pipeline
```
Phase 1: OnRequire       -> Scan/Interpret buildscripts (yaegi) -> Resolve dependencies (all eager, nil config)
Phase 2a: OnConfig       -> Execute callbacks -> Collect Options -> Run OnApply callbacks -> Merge global options
Phase 2b: FilterDeps     -> Re-run OnRequire with real config -> Replace node.Deps -> Update order -> BFS collect needed (inside runBuildPhase)
Phase 3: OnBuild         -> Execute callbacks -> Generate Targets -> Compile/Link
(Optional) Install       -> Install targets to prefix directory + generate manifest.json
```

### Clean Pipeline
```
Phase 1-2a: Same as build (OnRequire → OnConfig)
Phase 3: OnClean         -> Execute callbacks -> Directory cleanup
```

Entry point: `cmd/vmake/main.go` → `loadPlugins()` → `Execute()` (cobra). Pipeline: `resolveToConfig()` (cmd/vmake/root.go: config load → gitignore/legacy side effects → `pipeline.NewContext` → `pipeline.Require` → `pipeline.Configure`) → `pipeline.RunBuild(ctx, opts)` (pkg/pipeline/build_phase.go: filter → materialize → lock → patch → kconfig → OnBuild → schedule). `vmake lock update` = `pipeline.UpdateLock(ctx)`; clean/check-symbols/query consume `pipeline.Inspect(ctx)` + `pipeline.DeclareTargets(...)` instead of replaying phase internals. Build command logic in `cmd/vmake/build_cmd.go`.

## Code Style

### No Comments
Never add comments unless explicitly requested.

### Imports Ordering
Three groups separated by blank lines: stdlib -> external -> local:
```go
import (
    "context"
    "fmt"

    "github.com/spf13/cobra"

    "github.com/spock2300/vmake/pkg/api"
)
```
Internal packages may use short aliases: `vlog "github.com/spock2300/vmake/pkg/log"`, `exec "github.com/spock2300/vmake/internal/exec"`

### Naming Conventions
- **SetXxx**: Set a single value (SetKind, SetDefault)
- **AddXxx**: Append multiple values (AddFiles, AddIncludes)
- **RemoveXxx**: Remove from slices (RemoveCFlags, RemoveDefines)
- **Type aliases**: Use for readability (`type TargetKind string`)
- **Logging**: Always use alias `vlog "github.com/spock2300/vmake/pkg/log"` — methods: Debug, Info, Error, Fatal — **no Warn**

### Fluent API
All public APIs use method chaining - return `*Target`, `*Package`, `*Option`:
```go
ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c").AddIncludes("include")
```

### Error Handling
- Library code never panics, always return error with context: `fmt.Errorf("git clone %s -> %s: %w", url, dir, err)`
- CLI code uses `vlog.Fatal()` or `os.Exit(1)` for user-facing errors
- `pkg/api` misuse (double-set, invalid args, unavailable context funcs) panics with `*BuildScriptError` via `fatalScript(pkgName, op, ...)` (`pkg/api/errors.go`); recovered in `execFuncs` (`pkg/api/package.go`) and `runScriptFunc` (`pkg/buildscript/yaegi_loader.go`), normalized to `*BuildScriptError` with package name + operation, then reported via `vlog.Fatal` (exit 1). New `pkg/api` fatal paths must follow this pattern, not `vlog.Fatal`
- Dependency cycles are returned as errors (`api.CheckCycle`, wrapped by resolver as "dependency cycle detected") — never logged-and-ignored

### Cross-Platform Paths
- Filesystem paths: `filepath.Join()`
- Logical identifiers: string concatenation (`repo/name`, `pkg:target`)

### Code Organization
- Struct fields are private; access via getter/setter methods
- Exceptions: `InstalledPackage`, `PackageMeta`, `Toolchain`, `Tools`, `PackageNode`, `Graph` have public fields
- Within a file: setters first, then getters, then remove methods, then private helpers
- No interfaces — use function type aliases (`type ConfigFunc func(...)`) and struct-embedded function fields

### Struct Embedding (Composition)
Context types embed shared accessors to inherit behavior:
```go
type BuildContext struct {
    ConfigAccessor    // embedded: provides Bool(), String(), Option()
    *TargetRegistry   // embedded: provides Target(), GetTargets()
    *InstallItemHolder
    pkgBase           // embedded: provides PackageName()
    pkg               *Package
    genConfigHeader   bool
    genConfigDefines  bool
    exportConfig      bool
    importConfigs     []string
    buildSubGraphFunc func(pkgName string) error
    depOutputFunc     func(depRef string) string
    dryRun            bool
}
```

### Function Type Patterns
Common callback types defined in `pkg/api/`: `RequireFunc`, `ConfigFunc`, `BuildFunc`, `InstallFunc`, `CleanFunc`, `PackageFunc`, `CopyFilter`, `InstallFilterFunc`.
Define as `type XxxFunc func(...)` and store as struct fields.

### CLI Error Helpers
```go
func fatalErr(err error) {
    if err != nil { vlog.Error("Error: %v", err); os.Exit(1) }
}
func fatalMsg(format string, args ...any) {
    vlog.Error(format, args...); os.Exit(1)
}
```

### API Execution Methods
Methods on `*Package` (used in build.go scripts):
- `p.Run(name, args...)` — run command in BuildDir (uses exec.RunFatal, exits on failure)
- `p.RunIn(dir, name, args...)` — run command in specified directory
- `p.RunEnv(env, name, args...)` — run with custom environment in BuildDir
- `p.TargetTriple()` — target triple from the selected toolchain; replaces removed `CrossTarget()`
- `p.Env()` — compiler/binutils environment with resolved configured paths and installation-qualified `CROSS_COMPILE`
- `p.Make(args...)` — always uses BuildDir with `p.Env()`, passes `-C BuildDir` automatically
- `p.CMakeConfigure(args...)`, `p.CMakeBuild(args...)`, `p.CMakeInstall(args...)` — preferred CMake configure/build/install APIs, including project global flags
- `p.CMakeBuildDir()`, `p.CMakeInstallDir()` — actual CMake build and installation directories
- `p.SetCMakeBuildDir(dir)`, `p.SetCMakeInstallDir(dir)`, `p.SetCMakeBuildType(buildType)` — fluent settings shared across the CMake stages
- `p.CMakeGlobalFlagsArgs()` — returns package visibility and global-flag arguments for special manual cmake calls; `CMakeConfigure` already includes them
- `p.MergedCFlags(extra...)`, `p.MergedCxxFlags(extra...)` — merge package visibility defaults + global flags + extra into space-joined strings for CMake or toolchain files; `p.MergedLdFlags(extra...)` merges global linker flags + extra
- `p.Configure(args...)` — autotools configure

Methods on `BuildContext`:
- `ctx.Exec(name, args...)` — build-phase command execution (exits on failure via `exec.RunFatal`)
- `ctx.BuildSubGraph(pkgName)` — build a sub-package as independent sub-graph
- `ctx.DepOutput(depRef)` — get dependency target output file path
- `ctx.DepBuildDir(depRef)` — get dependency build directory
Methods on `ConfigContext`:
- `ctx.ToolchainOption()` — create toolchain choice option auto-populated with available toolchains
- `ctx.AddGlobalCFlags(flags...)` — add global C compiler flags (effective in OnConfig and OnApply callbacks; only applied for packages that survive FilterDeps)
- `ctx.AddGlobalCxxFlags(flags...)` — add global C++ compiler flags (effective in OnConfig and OnApply callbacks; only applied for packages that survive FilterDeps)
- `ctx.AddGlobalLdFlags(flags...)` — add global linker flags (effective in OnConfig and OnApply callbacks; only applied for packages that survive FilterDeps)
- `ctx.AddGlobalLinks(links...)` — add global link libraries (effective in OnConfig and OnApply callbacks; only applied for packages that survive FilterDeps)
- `ctx.SetProvidedLinkerScript(path)` — declare linker script for consumer targets (fatal on double-set)
Methods on `CleanContext`:
- `ctx.SourceDir()` — package root directory
- `ctx.BuildDir()` — build output directory
- `ctx.SrcDir()` — source code directory (differs from SourceDir when `SetGit()` is used)
- `ctx.Run(name, args...)` — run command in BuildDir (os.Exit on failure)
- `ctx.RunIn(dir, name, args...)` — run command in specified directory (os.Exit on failure)
- `ctx.RunEnv(env, name, args...)` — run with custom environment (returns real error)
- `ctx.Make(args...)` — run make in BuildDir with `pkg.Env()` (returns real error)

## Package Structure

| Package | Responsibility | Plugin Importable |
|---------|---------------|-------------------|
| `pkg/api` | Core API (Package, Target, Option, Contexts, Semver, KConfig, Copy) | **Yes** |
| `pkg/plugin` | Extension/plugin system | **Yes** |
| `pkg/buildscript` | Build script scan, yaegi interpretation, multi-file merge | No |
| `pkg/build` | Build execution, compile, link, scheduler, install, subgraph | No |
| `pkg/pipeline` | Phase orchestration (require/configure/materialize/lock/declare), RuntimeContext, dir/key derivation | No |
| `pkg/toolchain` | Toolchain abstraction (GCC, Clang) | No |
| `pkg/repo` | Package management, Git, native repos | No |
| `pkg/resolver` | Dependency graph, resolution | No |
| `pkg/config` | Project configuration management | No |
| `pkg/lockfile` | `.vmake/vmake.lock` read/write (pinned versions+commits) | No |
| `pkg/log` | Logging (Debug, Info, Error, Fatal) | No |
| `pkg/tui` | Terminal UI (interactive config) | No |
| `pkg/version` | Version information | No |
| `internal/*` | exec, flock, fs, gitcmd, gitstore, gitusr, glob, gosrc, jsonio, scriptfs, toposort, yaegibase, yaegisym | No |

**Dependency DAG**: `internal/*` -> `pkg/toolchain` -> `pkg/api` -> `pkg/buildscript, pkg/repo` -> `pkg/resolver, pkg/plugin, pkg/build` -> `pkg/pipeline` -> `cmd/vmake`

Extension plugins are interpreted by yaegi at runtime (same as buildscripts). Cobra/pflag symbols are pre-generated via `yaegi extract` into `internal/yaegisym/` (regenerate with `go generate ./internal/yaegisym/`). The plugin loader (`pkg/plugin/loader.go`) uses `internal/yaegibase.New()` + `internal/gosrc.MergeGoSources()` — no compilation step. Each discovered extension also registers a root-level cobra command named after the plugin (`loadPlugins()` in `cmd/vmake/ext_cmd.go`); the plugin adds subcommands via `plugin.Context.AddSubCommand`. Working scaffold + enforced-contract notes: `examples/plugins/README.md`. The plugin import path must be exactly `github.com/spock2300/vmake/pkg/plugin` (symbols are registered under that literal string; the old `gitee.com/...` spelling fails to resolve), and `plugin.json` must set `"enabled": true` or the plugin is skipped.

## CLI Architecture
- `github.com/spf13/cobra`, package-level vars, `init()` registration
- Command factory `newActionCmd` (repo remove/update, ext remove) plus shared helpers (`fatalErr`, `fatalMsg`, `newPkgRef`) in `cmd/vmake/helpers.go`; build/install flags registered once in `cmd/vmake/flags.go` (`addBuildFlags`/`addInstallFlags`, shared by build/rebuild/test)
- Global flags: `--verbose/-v`, `--very-verbose/-V`, `--quiet/-q`
- `vmake` (no subcommand) — defaults to `build`
- `vmake build` — build all targets (flags: `--toolchain`, `--mode`, `--install/-i`, `--prefix/-p`, `--install-type runtime|sdk`, `--tests`, `--manifest`, `--jobs/-j`, `--keep-going/-k`). `--jobs` controls package-level parallelism AND compile jobs per target (0 = NumCPU, 1 = sequential; `pkg/build/scheduler_parallel.go`); `--keep-going` keeps building independent targets after a failure
- `vmake test` — build with `--tests` then execute native test binaries; bare-metal, a different target OS or a nonempty target triple refuses execution and directs users to `vmake build --tests`
- `vmake clean [--all]` — execute OnClean hooks then clean build artifacts; `--all` removes all build key dirs
- `vmake rebuild` — clean local packages then build
- `vmake distclean` — deep clean: local build dirs, install/, `vmake_deps/`; the shared global cache survives by default (rebuild re-links without recompiling). `--purge-cache` also deletes global cache entries for every remote package this project materialized (affects other projects too)
- `vmake config` — interactive TUI for build options
- `vmake query [targets|config]` — show dependency tree (uses `newQueryCmd` factory, registered in root.go init)
- `vmake check-symbols [--strict]` — scan built Shared/Binary ELF outputs via the selected toolchain's `nm -D` and report: cross-target duplicate exports, C++ mangled leaks (`_Z*`), reserved-prefix leaks (`__libc_*` etc.), version-script violations (when `SetVersionScript` is set), and missing version-script warnings. No per-target declaration required. `--strict` exits non-zero on warn/error findings (info-level still passes). Both Linux and Windows hosts are supported; PE files and ELF files without dynamic symbols report that the audit is not applicable
- `vmake lock update|show` — re-resolve latest / show pinned versions (`.vmake/vmake.lock`)
- `vmake doctor [--toolchain NAME]` — two parts. Platform prerequisites: symlink capability, Git for Windows userland, selected make and configured toolchain tools. The project selection is used unless overridden; ARM profiles do not require native GCC. Then build.go findings: reliance on removed auto-wire (`AddRequires` with no `AddDeps`), deprecated APIs, `SetRoot` count. Error-severity findings fail the command; a missing project is also reported after platform diagnostics
- `vmake manifest show|checkout <path> [name]` — show manifest contents / checkout packages at recorded versions
- `vmake toolchain list|show [name]` — list/show toolchain manifests
- `vmake repo trust|untrust <name>` — manage supply-chain trust for remote repos (stored in `~/.vmake/config.json`)
- `vmake init-editor` — generate editor support files for build.go (go.mod for gopls)
- `vmake update [version]` — self-update
- `vmake version` — print version information
- `vmake skill install/uninstall/path` — install AI assistant skill files to `~/.claude/skills/vmake/` and `~/.agents/skills/vmake/`
- `vmake git tag`, `vmake completion`, `vmake ext add/remove/list/update`, `vmake pkg list/search/clean/update` — see README

## Build Script System

Buildscripts are interpreted at runtime by [yaegi](https://github.com/traefik/yaegi) (Go interpreter) — no `go build -buildmode=plugin`, no `.so` files, no `go.mod`/`go.sum` generation. Each `vmake` invocation re-interprets all `build.go` files fresh.

### Multi-File Support

A single buildscript package can be split across multiple `.go` files in the same directory. The loader (`pkg/buildscript/yaegi_loader.go`) selects files using `internal/gosrc.ListGoFiles`: GOOS/GOARCH filename suffixes and build constraints use the host running vmake, with CGO disabled; `_test.go` and files importing `C` are excluded. Extension plugins use the same selection. This host selection is independent of the C/C++ target OS. Selected files are merged at source level (deduplicating imports, combining declarations), written to a temp file, and interpreted via `yaegi.EvalPath`. This enables cross-file function calls, shared constants, and modular build logic.

### Symbol Table

`pkg/api` exports `YaegiSymbols()` (in `pkg/api/yaegi_symbols.go`) returning `map[string]reflect.Value` for all exported types, functions, constants, and variables. `pkg/toolchain` has a similar file. These are registered with the yaegi interpreter via `i.Use()`. A test (`pkg/api/yaegi_symbols_test.go`) uses `go/types` to verify completeness — adding a new export to `pkg/api` without registering it will fail the test.

### Buildscript Structure
```go
package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) { ... })
    p.OnBuild(func(ctx *api.BuildContext) { ... })
    p.OnClean(func(ctx *api.CleanContext) { ... })
}
```

### Yaegi Limitations

- **`[]string` spread to `...any` fails** — `reflect.CallSlice` can't convert `[]string` to `[]interface{}`. Pass `[]string` directly (without `...`); `AddCFlags`/`AddDefines`/`AddLinks` etc. use `flattenAny` to handle `[]string` items.
- **Type name not preserved** — `reflect.TypeOf(x).Name()` returns empty string for interpreted types; `fmt.Sprintf("%T", x)` prints underlying type (e.g. `struct {}`) not the named type.
- **No CGo, no assembly, no `//go:embed`, no `//go:generate`** — compiler directives are silently ignored.
- **`//line` directives in merged buildscripts crash the interpreter** (nil deref in type analysis when a directive precedes a function whose signature uses a binary-registered type, e.g. `func Main(p *api.Package)`). Do not emit them in `MergeGoSources`; revisit on a yaegi upgrade.
- **Performance** — interpreted code is slower than compiled, but build.go callbacks are lightweight (declare targets/options). Heavy operations go through `exec.Command` which is native.
- **Go modules not supported** by yaegi — irrelevant since build.go only imports `pkg/api` (provided as binary symbols via `i.Use()`).
- Working fine: generics, goroutines, channels, closures, defer, error wrapping, struct embedding, JSON marshal/unmarshal, `os/exec` (via unrestricted symbols), `net/http`, file I/O.
- **Script-relative file IO**: every interpreter carries its script dir (`internal/scriptfs`). In build.go code, relative paths passed to wrapped stdlib (`os.Open/OpenFile/Create/ReadFile/WriteFile/Stat/Lstat/Mkdir/MkdirAll/Remove/RemoveAll/Rename/ReadDir/CreateTemp`, `filepath.Walk/WalkDir`, `exec.Command` with unset `Dir`) resolve against the build.go's own directory — in ALL phases (Main, OnRequire discovery + FilterDeps re-run, OnApply, OnBuild declarations, SetBuildFunc closures). `os.Getwd()` returns the script dir; `os.Chdir` returns an error (process-wide chdir races with the parallel scheduler). Long-tail unwrapped APIs still see the process cwd — e.g. `exec.CommandContext` (only `exec.Command` is wrapped), `text/template.ParseFiles`, `io/ioutil` — use absolute paths built from `p.SourceDir()`/`p.BuildDir()` for those.

## Package Repositories
Registry checked first; native is fallback. Add commands:
```bash
vmake repo add name url                  # Registry
vmake repo add --native name "https://..../{name}.git"  # Native
```

## Key Types
```go
type TargetKind string  (TargetBinary, TargetStatic, TargetShared, TargetObject, TargetVoid)
type OptionType int     (OptionBool, OptionString, OptionInt, OptionChoice)
type SourceOrigin int   (SourceLocal, SourceRemote)
type PkgDirs struct { SourceDir, BuildDir, InstallDir string }
```

## Test Targets (`SetTest` / `vmake test`)

- `SetTest(true)` marks a target as a test (does NOT touch `isDefault` — ordering of `SetTest`/`SetDefault` is irrelevant)
- `vmake test` only runs targets where `IsTest() && Kind() == TargetBinary`
- Test inclusion is scheduler-level: `vmake build` skips `IsTest()` targets; `--tests`/`vmake test` include them (threaded via `Scheduler.SetIncludeTests` / `BuildPipeline.IncludeTests` / `SubGraphParams.IncludeTests`)
- `vmake build` skips test targets; `vmake build --tests` includes them
- `publishTarget` and `installTarget` skip `IsTest()` targets (test binaries are never installed)
- Test targets can depend on other test targets (e.g., `TargetStatic` test lib used by multiple test binaries); only `TargetBinary` tests are executed
- Subgraph builds inside `executeAllOnBuild` activate tests via `includeTests` parameter

## Known Gotchas
- `pipeline.DeclareTargets` (dry-run OnBuild re-execution used by query/check-symbols/install) intentionally does NOT run `applyBuildContextConfig` (config defines only affect compile flags, and install re-declares after a real build — re-applying would duplicate `-D`). Its `tc` argument may be nil, meaning "leave the package's existing toolchain wiring untouched"
- `cleanPackages` (cmd/vmake/clean.go) falls back to `cleanAllBuildDirs` when no build-key dir matches — deliberate UX decision, not a fallback chain to fix
- clean's OnClean hooks treat `pipeline.Inspect` errors as non-fatal (log, skip hooks, still clean build dirs) — best-effort by design; hash failures used to hard-exit inside the old loop
- `cmd/vmake/manifest_cmd.go` — was historically misspelled as `mainfest_cmd.go`; renamed in Phase 1 cleanup. References to the old name in older docs/scripts should be updated.
- `vlog` has `Debug`, `Info`, `Error`, `Fatal` — no `Warn` method
- `exec.Command` doesn't expand shell features — `$(nproc)` won't work, must use `runtime.NumCPU()`
- Go 1.26 `filepath.Join("/a/b", "/a/b/c")` returns `/a/b/a/b/c` — NOT `/a/b/c`
- `computeReachable` must use BFS from `IsLocal()` roots — NOT `node.Pkg != nil` and NOT full graph mark-all
- `tea.ExecProcess` (takes `*exec.Cmd`) vs `tea.Exec` (takes `tea.ExecCommand` interface) — use `ExecProcess` for external interactive commands
- `busybox` kconfig has no `olddefconfig` — only `oldconfig`, `defconfig`, `allnoconfig`
- `vmake_deps/` is in `scanner.go`'s `skipDirs` — build.go scanner will not recurse into it
- `ensureGitignore` writes only to project root `.gitignore` (via `findProjectDir()`), not to subdirectories
- `scanner.go` `skipDirs`: `.git`, `.vmake`, `build`, `vendor`, `node_modules`, `vmake_deps` — build.go files in these dirs are invisible to the scanner
- `filepath.Walk` does NOT follow a symlinked walk root — `Resolver.scanSubPackages` must `EvalSymlinks` the checkout dir first (`vmake_deps/.../src` is a symlink to the cache)
- Sub-package refs by full name require the parent to be resolved first — in one `AddRequires` list, parent before sub-package
- Buildscripts are re-interpreted on every `vmake` invocation — no `.so` cache, no `go.mod`/`go.sum` generated.
- Extension plugins are re-interpreted by yaegi on every `vmake` invocation — no `.so` compilation, no version-mismatch issues. Cobra/pflag symbols in `internal/yaegisym/` must be regenerated (`go generate`) when cobra version bumps.

## What Not To Do
- IDE integration plugins
- Remote builds / distributed compilation
- MSVC toolchain (not yet supported)
- Don't add fallback chains — fix root cause
- Don't use `pkg.Make()` when Makefile is in SourceDir — use `pkg.RunIn(srcDir, "make", ...)`
- Don't run tests from `test_data/` parent — each sub-project must be built from its own directory

## Coding Guidelines

**Think before coding.** State assumptions explicitly. If multiple interpretations exist, present them. If a simpler approach exists, say so.

**Minimum code that solves the problem.** No speculative features, abstractions, or configurability. If 200 lines could be 50, rewrite it.

**Surgical changes.** Touch only what the task requires. Match existing style. Remove imports/variables made unused by your changes — don't touch pre-existing dead code.

**Goal-driven execution.** Convert tasks into verifiable outcomes: "Add validation" → "write tests, then make them pass". For multi-step tasks, state a plan with verify checks.
