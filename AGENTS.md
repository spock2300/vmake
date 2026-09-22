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
- Windows CI's setup and scoped checks are in `.github/workflows/windows.yml`; its unit command excludes `(?i)subgraph|TestDeclarePackageTargetsDryRunWiring`. Windows snapshots are currently disabled there.

### Build-behavior snapshots

`test_data/_snapshot` is a separate Go module. Rebuild the root `vmake` binary first; the tests execute it rather than compiling it themselves:

```bash
CGO_ENABLED=0 go build -o vmake ./cmd/vmake
(cd test_data/_snapshot && go test)
(cd test_data/_snapshot && go test -run '^TestSnapshotsTestData/01_simple_c$')
```

- Snapshots compare installed artifacts, a redacted manifest, and `build/compile_commands.json`. Use them for compilation/linking/install behavior changes.
- The harness deletes fixture-local build/install/dependency directories and `.vmake` configuration before and after each test. It sets an isolated `VMAKE_CACHE` and `VMAKE_TRUST_ALL=1`.
- Projects 07–10 are skipped by `skipProjects` in `snapshot_test.go`; the full suite also runs firmware 17 when present.
- Prerequisites: 06 uses C++; 11 needs `official/tinyexpr`; 13 needs the `test_build` native repo; 25 requires `sh test_data/25_subpackage/setup.sh` after building vmake. That setup registers/trusts the local `subtest` repo in `~/.vmake`.
- For intentional drift, run `go test -update` inside `test_data/_snapshot` (optionally with `-run`). Baselines are OS-specific: `baseline/` and `baseline-windows/`. Investigate drift before updating.
- CMake integration: `python3 test_windows/verify_cmake.py ./vmake` exercises real CMake/Ninja builds; `--arm` also requires the ARM toolchain. Windows ARM setup and verification commands live in the workflow above.

## Execution boundaries

- `cmd/vmake/main.go` initializes Git userland before plugin loading and Cobra execution. Built-in `ext` commands skip plugin execution so broken extensions remain removable/updatable.
- `cmd/vmake/root.go:resolveToConfig` calls `pipeline.Require` then `pipeline.Configure`. `pkg/pipeline/build_phase.go:RunBuild` owns filtering, source preparation, lockfile writing, patches, Kconfig restoration, OnBuild, and scheduling. Keep orchestration in `pkg/pipeline`; compilation/linking/install belong in `pkg/build`.
- `OnRequire` runs first for eager discovery without option values, then again through `FilterDeps` after `OnConfig`/`OnApply`. Use discovery-aware `When`/`If`/`Select` there; direct `Bool`/`String`/`Int` reads fail. Reachability is traversed from local roots, not every loaded remote package.
- `AddRequires` declares package resolution dependencies; targets must explicitly declare build edges with `AddDeps`. There is no automatic Require-to-target wiring.
- Query/check-symbols/clean reuse `pipeline.Inspect`; target inspection/install use `pipeline.DeclareTargets` for dry-run OnBuild. `DeclareTargets` intentionally omits `applyBuildContextConfig` to avoid duplicate defines; a nil toolchain preserves existing wiring.
- Local and remote packages share scheduling, compilation, and linking. Keep source materialization, directory selection, and patch handling at their existing boundaries.

## Interpreted scripts and generated symbols

- Buildscripts and plugins are reinterpreted each invocation: no `.so`, `-buildmode=plugin`, or module generation. Their usable binary imports are registered in `pkg/buildscript/yaegi_loader.go` and `pkg/plugin/loader.go`.
- New package-level exports in `pkg/api` must be registered in `pkg/api/yaegi_symbols.go`; `TestYaegiSymbolsComplete` checks this. `pkg/toolchain` and `pkg/plugin` maintain their own symbol tables.
- Cobra/pflag bindings under `internal/yaegisym` are generated. After Cobra/pflag upgrades, run `go generate ./internal/yaegisym/` with `yaegi` (from `github.com/traefik/yaegi/cmd/yaegi`) on PATH, matching the version in `go.mod`; do not hand-edit generated bindings.
- `internal/gosrc.ListGoFiles` selects Go files using the running host's GOOS/GOARCH with CGO disabled, independently of the C/C++ target. It excludes `_test.go` and files importing `C`; the loaders merge selected files for interpretation.
- Pass `[]string` directly to `AddCFlags`, `AddDefines`, etc.; spreading it with `...` into their `...any` parameters fails under yaegi. The API flattens slices itself.
- `internal/scriptfs` wraps common `os` operations, `filepath.Walk/WalkDir`, and `exec.Command` to use the script directory. `os.Chdir` is rejected. Unwrapped APIs such as `exec.CommandContext` and `template.ParseFiles` still use process cwd; supply explicit absolute paths/command directories.

## Directories and incremental behavior

- `SourceDir()` is the package root; `SrcDir()` is the actual source tree and can differ after `SetGit`; `BuildDir()` is build-key-specific. `p.Run` and `p.Make` use BuildDir. For an in-source Makefile use `p.RunIn(srcDir, "make", ...)`.
- Prefer `CMakeConfigure`/`CMakeBuild`/`CMakeInstall`. Use their directory/configuration setters and `CMakeBuildDir()`/`CMakeInstallDir()` getters; defaults are `BuildDir()/cmake` and remote InstallDir or local `BuildDir()/staging`. Keep CMake artifacts separate from `SetPrebuilt` symlink destinations.
- `vmake_deps/<repo>/<pkg>/{src,out}` links into `~/.vmake/cache/<repo>/<pkg>/<version>/`. Source checkouts are immutable; outputs are shared between projects. Keep `_locks` outside guarded package directories and never unlink held lock files. `distclean` retains the shared cache unless `--purge-cache` is requested.
- `.vmake/vmake.lock` pins remote versions/commits; `vmake lock update` explicitly re-resolves them. Preserve offline locked resolution rather than adding unconditional fetches.
- Kconfig presets are make target names, not complete `.config` files. Generation belongs in `EnsureConfig`/TUI `ensureConfigCmd`; `restoreKConfigFiles` only restores saved content. Preserve unchanged config mtimes so incremental stamps remain valid.
- Custom post-link inputs/outputs require `AddPostLinkDeps`/`AddPostLinkOutputs`. Missing outputs or changed inputs trigger relinking and all post-link steps; command arguments do not imply outputs. Hex/Bin/Strip helpers declare their outputs automatically.
- `autoconf.h` is package-local; public headers must not include it. Cross-package config propagation uses exported/imported options and generated defines. `SetDefaultVisibilityHidden` is also package-local, not a dependency-wide policy.
- Native remote subpackages are lazily loaded and inherit the parent's version. Resolve the parent before referencing its subpackages. Registry wrappers and local nested buildscripts have different semantics; consult `docs/DESIGN_DECISIONS.md` before changing discovery.

## Cross-platform constraints

- Artifact names and linker/CMake behavior follow the project's `target_os`, not the host. Bare-metal uses `none`; the compiler triple is the project option `target_triple` / `Package.TargetTriple()`. Toolchain manifests carry no target or default flags: `target_os`/`target_triple`/`default_flags` are rejected there. They use host-keyed `installations` with explicit `root_dir`; legacy `host`/`install` fields are rejected too.
- Configured tools resolve strictly: installation-relative tools stay under that installation's `bin`, with no PATH fallback. Preserve definition errors rather than selecting the host toolchain. Unset MAKE is resolved only when make is needed; explicitly configured MAKE is validated.
- Route git invocations through `internal/gitcmd.Args` to preserve LF content, long paths, and symlink settings. Use `filepath.ToSlash` for path arguments sent to MSYS `sh`/`make`/`tar`/`curl`; native `cmd.Dir` retains OS separators.
- Windows storage requires real symlinks (Developer Mode or elevation); do not substitute junctions. Git for Windows supplies Unix userland, not a C compiler or make. Diagnose a fixture with `../../vmake.exe doctor --toolchain NAME` from its project directory.
- Windows GNU compile/link/archive commands use response files; `compile_commands.json` must retain expanded arguments. VMake-built PE shared libraries link through `.dll.a` import libraries; never whole-archive these. ELF-only symbol policies must fail on PE targets.
- `vmake test` executes native test binaries. Cross/bare-metal targets use `vmake build --tests`; test targets are excluded from ordinary builds and installation.

## Repository conventions

- Do not add comments unless explicitly requested. Use function-type callbacks and composition instead of introducing interfaces.
- API mutators are fluent: `SetX` sets a single value, `AddX` appends, `RemoveX` removes. Keep fields private where the surrounding API uses accessors.
- Imports are grouped stdlib → external → local. Logging uses `vlog "github.com/spock2300/vmake/pkg/log"`; available methods are Debug/Info/Error/Fatal, not Warn.
- API misuse raises `*BuildScriptError` through `fatalScript` (`pkg/api/errors.go`), recovered by script execution boundaries. New API validation must use that path rather than exiting with `vlog.Fatal`. Ordinary library errors are returned to callers.
- Preserve strict contracts: fix invalid callers rather than adding fallback chains or silent defaults. IDE integrations, distributed builds, and MSVC support are outside the current scope.

Detailed references: `docs/BUILD_SCRIPT_API.md` for script contracts, `docs/EXTENSION_PLUGIN.md` for plugins/toolchains, and `docs/DESIGN_DECISIONS.md` for intentional resolver limitations.
