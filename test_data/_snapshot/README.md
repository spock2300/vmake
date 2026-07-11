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

All test_data projects currently pass — no known pre-existing failures.

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
