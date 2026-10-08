# VMake Snapshot Tests

Golden snapshot tests for vmake build behavior. Each `test_data/*` project is
built from clean, and its `install/` tree + `build/compile_commands.json` are
hashed and compared against a baseline.

## Usage

```bash
# From this directory:
go test                                    # compare against baselines
go test -update                            # regenerate baselines
go test -run TestSnapshotsTestData/01_simple_c   # single project
VMAKE_SNAPSHOT_UPDATE=1 go test            # regenerate via env var

# From repo root:
go build -o vmake ./cmd/vmake              # rebuild vmake first
(cd test_data/_snapshot && go test)
```

Baselines are per host OS: `baseline/` (Linux), `baseline-windows/`, and
`baseline-darwin/`. Generate the macOS baselines on a Mac with `go test -update`;
until they exist, snapshot comparison on macOS fails with a missing-baseline
error.

## What is Snapshotted

Per project (`build --install`):
- `install/manifest.json` — with volatile fields stripped (`vmake`, `generated`,
  and for local packages also `version` and `ref`).
- `install/` tree — every file's SHA256, with absolute paths normalized
  (`<ROOT>` for repo root, `<HOME>` for user home).
- `build/compile_commands.json` — compile flag drift detector.

## What is NOT Snapshotted

- `build/<hash>/objects/*.o` and `*.o.d` — redundant with `compile_commands.json`.
- `manifest.json` itself is captured via the redacted field, not as a file.

## Excluded Projects

- `07_subbuild_codegen`, `08_with_package`, `09_with_curl` and `10_local_repo`
  are skipped by `skipProjects` (subgraph/remote-repo prerequisites).
- On macOS the ELF-only fixtures `12_rtos_simulate`, `22_version_script`,
  `23_link_strategy` and `24_symbol_prefix` are skipped, and the firmware
  snapshot (`TestSnapshotsTestLinux`) is not run, because the builtin host
  toolchain has no GNU ld/binutils and Apple's GNU Make 3.81 is too old for the
  kernel/U-Boot build. On Linux all discovered projects pass.

## Interpreting Failures

```
snapshot drift:
  [install] changed: bin/hello
    want f50ee70e4776
    got  abcd1234ef56
```

This means a build artifact differs from the baseline. Investigate:
1. Intended change (you modified build.go / sources) → run `go test -update`.
2. Unintended change (you refactored vmake internals) → fix the regression
   before committing.
3. Compile flag drift shows up as `compile_commands.json` change — inspect
   the actual flag difference with `diff` against the baseline content.
