# VMake agent guide

VMake builds C/C++ projects from Go scripts interpreted by yaegi. The Go executable and the projects it builds have separate build/test workflows.

## Build and verify

Requires Go 1.26+ (`go.mod`). Use CGO-disabled builds, including Windows cross-compiles. From the repository root:

```bash
CGO_ENABLED=0 go build -o vmake ./cmd/vmake
CGO_ENABLED=0 go vet ./cmd/vmake/... ./pkg/... ./internal/...
CGO_ENABLED=0 go test ./cmd/vmake/... ./pkg/... ./internal/...
```

- Never use bare `go test ./...` or `go vet ./...`: they traverse C/C++ fixtures containing Go buildscripts and fail with C-source errors.
- Focus a package/test with, for example, `CGO_ENABLED=0 go test ./pkg/api -run '^TestYaegiSymbolsComplete$' -count=1`.
- Run each integration project from its own directory: `(cd test_data/01_simple_c && ../../vmake build)`. Do not invoke a build from the `test_data/` parent.
- Firmware fixture 17 is in `test_linux/17_firmware`, not `test_data/`.
- Windows cross-build: `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o vmake.exe ./cmd/vmake`.
- CI is Windows-only (`.github/workflows/windows.yml`); it runs the scoped vet/test commands above with `-skip '(?i)subgraph|TestDeclarePackageTargetsDryRunWiring'` and leaves snapshots disabled. Run Linux/macOS verification locally.
- `build`/`rebuild` publish `build/vmake-build.json` and `test` publishes `build/vmake-test.json` (schemaVersion 2: command, status, duration, concise error, parsed diagnostics, success summary/counts, provenance, per-target status/duration, test results and hashed installed artifacts); `clean`/`distclean`/`pkg clean` remove both, and `clean` also removes the stale `build/compile_commands.json` and notes remaining other-configuration build keys. Reports are separate from `install/manifest.json`; see `docs/BUILD_RESULT.md`.

### Build-behavior snapshots

`test_data/_snapshot` is a separate Go module. Rebuild the root `vmake` binary first; the tests execute it rather than compiling it themselves:

```bash
CGO_ENABLED=0 go build -o vmake ./cmd/vmake
(cd test_data/_snapshot && go test)
(cd test_data/_snapshot && go test -run '^TestSnapshotsTestData/01_simple_c$')
```

- Snapshots compare installed artifacts, a redacted manifest, and `build/compile_commands.json`. Use them for compilation/linking/install behavior changes.
- The harness deletes fixture-local build/install/dependency directories and `.vmake` configuration before and after each test. It sets an isolated `VMAKE_CACHE` and `VMAKE_TRUST_ALL=1`.
- Discovery is automatic for any `test_data/*` directory containing `build.go`. `skipProjects` excludes 07–10 and `skipProjectsDarwin` excludes ELF-only 12/22–24 on darwin; `TestSnapshotsTestLinux` adds firmware 17 when present.
- Prerequisites: 06 uses C++; 11 needs `official/tinyexpr`; 13 needs the `test_build` native repo; 25 requires `sh test_data/25_subpackage/setup.sh` after building vmake. That setup registers/trusts the local `subtest` repo in `~/.vmake`.
- For intentional drift, run `go test -update` inside `test_data/_snapshot` (optionally with `-run`). Only `baseline/` is committed; the code uses `baseline-windows/` or `baseline-darwin/` on those hosts, so create that directory before updating. Investigate drift before updating.
- CMake integration: `python3 test_windows/verify_cmake.py ./vmake` exercises real CMake/Ninja builds; `--arm` also requires the ARM toolchain. Windows ARM setup and verification commands live in that workflow.

## Execution boundaries

- Targets execute serially in target dependency order. Only source compilation within the active target is parallel. `-j0` resolves to CPU count once; Make/CMake helpers share the same ceiling.
- Required Void build callbacks execute once per invocation, including synchronous subgraphs. VMake does not skip arbitrary callbacks across invocations; Make/CMake own their incremental checks.
- Installation reuses the invocation's BuildCtxs. Do not replay OnBuild during installation. Synchronous subgraph roots are skipped when collecting OnInstall items.
- `cmd/vmake/main.go` initializes Git userland before plugin loading and Cobra execution. Built-in `ext` and `cache` commands skip plugin execution so broken extensions remain removable/updatable and cache maintenance still works.
- `cmd/vmake/root.go:resolveToConfig` calls `pipeline.Require` then `pipeline.Configure`. `pkg/pipeline/build_phase.go:RunBuild` owns filtering, source preparation, lockfile writing, patches, Kconfig restoration, OnBuild, and scheduling. Keep orchestration in `pkg/pipeline`; compilation/linking/install belong in `pkg/build`.
- `OnRequire` runs first for eager discovery without option values, then again through `FilterDeps` after `OnConfig`/`OnApply`. Direct `Bool`/`String`/`Int` reads fail during discovery; in `FilterDeps`, this package's entry option values are readable (global values are not merged). Prefer discovery-aware `When`/`If`/`Select` for conditional dependencies. Reachability is traversed from local roots, not every loaded remote package.
- `AddRequires` declares package resolution dependencies; targets must explicitly declare build edges with `AddDeps`. There is no automatic Require-to-target wiring.
- `inspect`/`query`/`query targets`/`check-symbols`/`clean` and `test`'s no-tests preflight reuse `pipeline.Inspect` (the `clean --all` fallback scans package directories instead); target inspection (`inspect`, `query`, `query targets`, `check-symbols`, `test` preflight) uses `pipeline.DeclareTargets` for dry-run OnBuild. `DeclareTargets` intentionally omits `applyBuildContextConfig` to avoid duplicate defines; a nil toolchain preserves existing wiring. `InspectWithOptions(... FilterDeps: true)` runs `FilterDeps` before inspection so these commands see the same package set as `RunBuild`; plain `Inspect` keeps the eager graph. `query config` bypasses `Inspect` entirely.
- Inspection never materializes sources: an absent local SetGit tree declares an empty `BuildDir()`, while present packages receive their resolved `BuildDir()` and `inspect`/`query`/`test` preflight enable read-only `scriptfs` (`--allow-dry-run-writes` opts out). OnBuild file writes must guard on `p.DryRun()` (and `p.BuildDir() == ""` for reads/paths) and cannot assume the `src` link resolves.
- `build.PackageLayout` and `layoutFor` in `pkg/pipeline/layout.go` are the single source of truth for per-package build/install dirs and build keys; do not recompute them in callers. Keys cover toolchain identity, mode, options, global flags and the materialized source commit; remote keys also include source version and patch hash. Buildscript text is action-level: its commands, includes, defines and file lists are captured by action signatures, so a comment-only script edit does not rotate `BuildDir`. Gen rules record the declared input in the generated header's first line (a hashed marker); other files a script writes into `BuildDir()` outside declared actions are untracked and must not be guarded by bare existence checks. A local SetGit patch-only change restores its tree but does not rotate its `BuildDir`; hash any new output-affecting input.
- Local and remote packages share scheduling, compilation, and linking. Keep source materialization, directory selection, and patch handling at their existing boundaries.
- Remote buildscripts and extension code are gated by `~/.vmake/trust.json` records keyed `kind:name@commit`; a missing or empty resolved commit is never trusted. Extensions additionally require a clean worktree (ignored `.go` files count as dirty). Loading holds the shared `~/.vmake/_locks/{repo,ext}_<hash>.lock`; extension locks stay held until the process exits so build-time toolchain installs read the checked worktree. `flock` is not reentrant, so trust decisions must not call a command that takes the exclusive lock on the same repository.

## Interpreted scripts and generated symbols

- Buildscripts and plugins are reinterpreted each invocation: no `.so`, `-buildmode=plugin`, or module generation. Their usable binary imports are registered in `pkg/buildscript/yaegi_loader.go` and `pkg/plugin/loader.go`.
- Script-facing package-level exports in `pkg/api` are an explicit allowlist in `pkg/api/yaegi_symbols.go`; add new symbols deliberately rather than exporting every Go API. `TestYaegiSymbolsComplete` checks exported symbols are registered there; native runtime binding and target snapshots are explicitly excluded and must remain unavailable to scripts. `pkg/toolchain` and `pkg/plugin` maintain their own symbol tables.
- Cobra/pflag bindings under `internal/yaegisym` are generated. After Cobra/pflag upgrades, run `go generate ./internal/yaegisym/` with `yaegi` (from `github.com/traefik/yaegi/cmd/yaegi`) on PATH, matching the version in `go.mod`; do not hand-edit generated bindings.
- `internal/gosrc.ListGoFiles` selects Go files using the running host's GOOS/GOARCH with CGO disabled, independently of the C/C++ target. It excludes `_test.go` and files importing `C`; the loaders merge selected files for interpretation.
- Pass `[]string` directly to `AddCFlags`, `AddDefines`, etc.; spreading it with `...` into their `...any` parameters fails under yaegi. The API flattens slices itself.
- Custom `GlobalOption`s are auto-exported to every package as `CONFIG_<NAME>` defines (choice values add `CONFIG_<NAME>_<UPPER_VALUE>`); plain package options become macros only through `GenerateConfigDefines`/`GenerateConfigHeader`. Options may only be declared during `OnConfig`.
- `internal/scriptfs` wraps common `os` operations, `filepath.Walk/WalkDir`, and `exec.Command` so relative paths resolve against `pkg.SourceDir()` when set, otherwise the build.go directory (plugins resolve against their entry directory). `os.Chdir` is rejected. `OnBuild`/`OnInstall`/`OnClean` callbacks run inside `os.Chdir(SourceDir)`, so unwrapped APIs such as `exec.CommandContext` and `template.ParseFiles` resolve relative paths there. `OnConfig` runs before source binding inside the loaded build.go directory (the registry wrapper checkout for registry packages); `OnRequire`/`OnPackage` and a later `TargetVoid` `SetBuildFunc` use the process cwd. Supply explicit absolute paths/command directories.

## Directories and incremental behavior

- `SourceDir()` is the package root; `SrcDir()` is the actual source tree and can differ after `SetGit`; `BuildDir()` is build-key-specific. `p.Run` and `p.Make` use BuildDir. For an in-source Makefile use `p.Make("-C", filepath.ToSlash(srcDir), ...)`; the source directory must be absolute. Make/CMake helpers inherit the invocation jobs ceiling.
- Prefer `CMakeConfigure`/`CMakeBuild`/`CMakeInstall`. Use their directory/configuration setters and `CMakeBuildDir()`/`CMakeInstallDir()` getters; defaults are `BuildDir()/cmake` and remote InstallDir or local `BuildDir()/staging`. Keep CMake artifacts separate from `SetPrebuilt` symlink destinations.
- `CMakeConfigure` fingerprints its inputs in `<CMakeBuildDir>/.vmake_configure.json` and skips unchanged runs; new files matched only by `file(GLOB)` do not invalidate it—use `CONFIGURE_DEPENDS` or clean the build tree.
- Third-party sources materialize as a single shallow working tree per package at `<project>/.vmake_deps/<repo>/<owner>/src[/<member>]` (local SetGit packages: `.vmake_deps/local/<pkg>/src`, linked from `<pkgDir>/src`). The selected commit's objects are fetched once into the shared content-addressed cache `~/.vmake/cache/git/<urlhash>.git` (one bare repo per upstream URL, anchored under `refs/vmake/<commit>`; `VMAKE_CACHE` overrides the default cache root and `VMAKE_GIT_CACHE=off` disables the cache and fetches depth-1 directly from the package's upstream URL), and a lock-pinned working tree at a cached commit is then fetched depth-1 from that cache without touching upstream; ref resolution (`git ls-remote`), HEAD materialization, cache misses, and the full-clone fallback for servers that reject SHA fetches still contact upstream. Same-commit work (patch-set changes) is restored in place with `checkout --force --detach`/`clean -fdx`; a different commit recreates the tree. Builds hold the exclusive project lock, and each tree is serialized by `tree_<hash>.lock` while each cache namespace is serialized by `cache_<urlhash[:16]>.lock`. Keep `_locks` outside guarded package directories and never unlink held lock files. `clean`/`clean --all` keep these working trees, and `distclean` keeps them unless `--purge-sources` is passed; ordinary cleaning does not reset source edits.
- Storage layout diagrams are living documentation: whenever paths, lock/state files, or cleanup semantics under `~/.vmake/`, `.vmake/`, or `.vmake_deps/` change, update the structure diagrams in `docs/VMAKE_HOME.md` and every other doc or bundled skill file that states those paths (for example `docs/ARCHITECTURE.md`, `docs/FIRMWARE_BUILD_DESIGN.md`, `cmd/vmake/skills/vmake/`) in the same change; do not leave stale paths.
- `.vmake/vmake.lock` pins remote versions/commits, versioned local SetGit versions/commits, the source kind (`registry`/`native`/`git`), and registry wrapper commits; `vmake lock update` explicitly re-resolves them. Preserve offline locked resolution rather than adding unconditional fetches.
- `.vmake/project.json` selects the active config file; if absent, `.vmake/config.json` is the default, but a present selection that is missing/invalid must error rather than fall back. All config files share `.vmake/vmake.lock`; `config list|use|copy|describe` do not run Require/Configure.
- Kconfig presets are make target names, not complete `.config` files. Generation belongs in `EnsureConfig`/TUI `ensureConfigCmd`; `restoreKConfigFiles` only restores saved content. Preserve unchanged config mtimes so incremental stamps remain valid.
- Custom post-link inputs/outputs require `AddPostLinkDeps`/`AddPostLinkOutputs`. Missing outputs or changed inputs trigger relinking and all post-link steps; command arguments do not imply outputs. Hex/Bin/Strip helpers declare their outputs automatically.
- `autoconf.h` is package-local; public headers must not include it. Cross-package config propagation uses exported/imported options and generated defines. `SetDefaultVisibilityHidden` is also package-local, not a dependency-wide policy.
- Native remote subpackages are lazily loaded and inherit the parent's version. Resolve the parent before referencing its subpackages. Registry wrappers and local nested buildscripts have different semantics; consult `docs/DESIGN_DECISIONS.md` before changing discovery.

## Cross-platform constraints

- Artifact names and linker/CMake behavior follow the project's `target_os`, not the host. Bare-metal uses `none`; the compiler triple is the project option `target_triple` / `Package.TargetTriple()`. Toolchain manifests may declare `target_os`/`target_triple` defaults that apply only while the project leaves them unset; `default_flags` stays rejected there. They use host-keyed `installations` with explicit `root_dir`; legacy `host`/`install` fields are rejected too.
- Configured tools resolve strictly: installation-relative tools stay under that installation's `bin`, with no PATH fallback. Preserve definition errors rather than selecting the host toolchain. Unset MAKE is resolved only when make is needed; explicitly configured MAKE is validated.
- Route git invocations through `internal/gitcmd.Args` to preserve LF content, long paths, and symlink settings. Use `filepath.ToSlash` for path arguments sent to MSYS `sh`/`make`/`tar`/`curl`; native `cmd.Dir` retains OS separators.
- Windows storage requires real symlinks (Developer Mode or elevation); do not substitute junctions. Git for Windows supplies Unix userland, not a C compiler or make. Diagnose a fixture with `../../vmake.exe doctor --toolchain NAME` from its project directory.
- Windows GNU compile/link/archive commands use response files; `compile_commands.json` must retain expanded arguments. VMake-built PE shared libraries link through `.dll.a` import libraries; never whole-archive these. ELF-only symbol policies must fail on PE targets.
- macOS hosts use the builtin `cc`/`c++` toolchain with no `objcopy`/`objdump`; do not require them there. Native links are Mach-O: dependency archives use `-force_load` (no GNU group/whole-archive), `TargetShared` links with `-dynamiclib` and an `@rpath/<name>` install name, binary consumers get `-rpath` entries for the dependency directories plus `@loader_path`/`@loader_path/../lib`, shared-library consumers get the dependency directories plus `@loader_path` only, and ELF-only symbol policies must fail on Mach-O targets. Only `target_os=darwin` selects this path; values are not aliased. Mach-O artifacts are out of scope for `vmake check-symbols`.
- Apple ships GNU Make 3.81; Kconfig/kernel/U-Boot builds need 3.82+ (`brew install make`, then point the toolchain's MAKE at `gmake`).
- `vmake test` executes native test binaries. Cross/bare-metal targets use `vmake build --tests`; test targets are excluded from ordinary builds and installation.

## Repository conventions

- Do not add comments unless explicitly requested. Use function-type callbacks and composition instead of introducing interfaces.
- API mutators are fluent: `SetX` sets a single value, `AddX` appends, `RemoveX` removes. Keep fields private where the surrounding API uses accessors.
- Imports are grouped stdlib → external → local. Logging uses `vlog "github.com/spock2300/vmake/pkg/log"`; available methods are Debug/Info/Error/Fatal, not Warn.
- `cmd/vmake/skills/vmake/` is embedded, test-checked user documentation. After CLI changes regenerate `references/cli.md` with `CGO_ENABLED=0 go test ./cmd/vmake -run '^TestSkillCLISnapshot$' -update-skill-ref`; keep the API/storage references in sync with code.
- API misuse raises `*BuildScriptError` through `fatalScript` (`pkg/api/errors.go`), recovered by script execution boundaries. New API validation must use that path rather than exiting with `vlog.Fatal`. Ordinary library errors are returned to callers.
- Preserve strict contracts: fix invalid callers rather than adding fallback chains or silent defaults. IDE integrations, distributed builds, and MSVC support are outside the current scope.

Detailed references: `docs/ARCHITECTURE.md` for execution flow, `docs/BUILD_SCRIPT_API.md` for script contracts, `docs/INSPECT.md` for the `vmake inspect` JSON schema, `docs/VMAKE_HOME.md` for storage layout, `docs/EXTENSION_PLUGIN.md` for plugins/toolchains, and `docs/DESIGN_DECISIONS.md` for intentional resolver limitations.

## Coding Guidelines

**Think before coding.** State assumptions explicitly. If multiple interpretations exist, present them. If a simpler approach exists, say so.

**Minimum code that solves the problem.** No speculative features, abstractions, or configurability. If 200 lines could be 50, rewrite it.

**Surgical changes.** Touch only what the task requires. Match existing style. Remove imports/variables made unused by your changes — don't touch pre-existing dead code.

**Goal-driven execution.** Convert tasks into verifiable outcomes: "Add validation" → "write tests, then make them pass". For multi-step tasks, state a plan with verify checks.
