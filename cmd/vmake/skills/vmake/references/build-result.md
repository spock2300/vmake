# Build Results and `--json`

`vmake build` (including bare `vmake`), `vmake rebuild`, and `vmake test`
atomically publish `build/vmake-build.json` in the project root. The file
records the latest invocation only and uses schemaVersion 2. It is separate from
the installation manifest (`install/manifest.json`). `clean`, `distclean`, and
`pkg clean` invalidate it.

## Report fields

| Field | Meaning |
|-------|---------|
| `schemaVersion`, `buildId`, `root` | Report schema (2), unique invocation ID, absolute project root |
| `command` | `build` for a build, `rebuild` for `vmake rebuild`, `test` for `vmake test` |
| `status` | `running`, `succeeded`, `failed`, or `cancelled`; for test, `succeeded` only when every executed test passed |
| `started`, `finished`, `durationMs` | UTC timestamps and wall-clock duration; `durationMs` is 0 while running |
| `error` | Concise one-line failure summary (about 300 characters at most); empty on success |
| `diagnostics` | Always an array. Success entries are tool warnings; failure entries are errors followed by warnings. Each has `severity`, `code`, `message`, and optional `file`, `line`, `column`, `package`, `target`. Codes: `VMAKE_E_COMPILE`, `VMAKE_E_LINK`, `VMAKE_E_BUILD`, `VMAKE_E_SCRIPT` for errors, `VMAKE_W_TOOL` for warnings |
| `summary` | Per-invocation counts (see below); present on success, on a failure whose scheduler produced a partial result, and for a test failure after the build phase. Omitted when the failure happened before any target was scheduled |
| `configFile`, `configSha256` | Active configuration name and SHA-256 of the exact configuration bytes used |
| `global`, `packages` | Effective global values and per-package values for the packages that participated |
| `toolchain`, `mode` | Effective toolchain and mode |
| `compileDatabase` | Absolute path to the compilation database, or empty |
| `targets` | Executed default targets: `package`, `target`, `kind`, `outputs`, `status`, `durationMs`. On failure only targets that completed before the failure are listed |
| `tests` | Always an array. For `vmake test`, one entry per executed test binary with `name`, `package`, `target`, `passed`, `elapsedMs`, `exitCode`, and `output` |
| `artifacts` | Files installed by this invocation only: `package`, `target`, `buildPath`, `installPath`, `size`, `sha256` |

A target's `status` is `built` when it compiled or linked something in this
invocation, or when it is a `void` target (its build callback always runs);
otherwise it is `up-to-date`. `durationMs` is nonnegative and 0 when too fast to
measure. Non-default targets are never executed or recorded. Test targets appear
only for `vmake build --tests` and `vmake test`.

## Summary counts

`summary` repeats `durationMs` and carries `targetsTotal`, `targetsBuilt`,
`targetsUpToDate`, `targetsFailed`, `testsFound`, `testsPassed`, `testsFailed`,
`filesCompiled`, `filesLinked`, `actionsUpToDate`, `artifactsInstalled`, and
`changedFiles`, plus `callbacks` (executed `built` void targets) and `noOp`.

On a failed build or build phase, `summary` describes the work attempted before
the failure: `targetsFailed` counts distinct failed target names from the
diagnostics (or 1 when a target failure is known without a name),
`artifactsInstalled` is 0, and no installation runs. A failure before any target
is scheduled (for example a buildscript error) omits `summary` entirely.

Test counts are 0 for build/rebuild. Compile/link counts cover compile and link
actions only, not post-link steps, void callbacks, or installation. A no-op
build therefore has `filesCompiled: 0`, `filesLinked: 0`, `changedFiles: []`,
and `noOp: true`; it may still have run callbacks and installed artifacts.
`changedFiles` is the sorted, unique list of compiled source paths written as
`package:relative/path` (slash-separated; already absolute or qualified paths
keep their form), and `[]` when nothing compiled.
Counts describe this invocation's work, not repository state.

## Failure diagnostics

Compiler output is parsed from the GNU `file:line[:column]: severity: message`
form; GNU ld's `file:line: undefined reference to ...` / `multiple definition of
...` lines are also recognized. Each diagnostic is attributed to the failing
target and stage. `error` repeats the first parsed error. A command failure with
no parseable location produces one fallback entry: the last
`undefined reference`/`multiple definition` line, then the last `error:` line,
then the trimmed output tail (up to 1000 characters). Buildscript/API errors use
`VMAKE_E_SCRIPT` and carry the package name. Raw tool output and the full failing
command stay on stderr; they are not stored in the report.

## Success warnings

A succeeded build or test run collects the warnings streamed to stderr during
the invocation into `diagnostics`. GNU `file:line[:column]: warning: message`
lines become located entries with `file`/`line`/`column`; generic
`warning: message` and `tool: warning: message` lines become unlocated entries.
Every warning uses `severity: "warning"` and `code: "VMAKE_W_TOOL"`, is
deduplicated by file/line/column/message, and the list is capped at 50 entries.
Warnings never change `status` or `error`, and they are not repeated in stderr;
the raw warning still streams exactly once. A clean run reports an empty
`diagnostics` array.

On failure, `diagnostics` contains the parsed errors first and warnings after
them (warnings parsed from the failing command keep their `package`/`target`);
`error` still summarizes the first error.

## `--json`

`vmake build --json`, `vmake rebuild --json`, and `vmake test --json` write
exactly one JSON document to stdout. Human progress, buildscript prints, child
stdout, and raw diagnostics move to stderr, and no human summary line is
printed.

```json
{
  "schemaVersion": 1,
  "command": "build",
  "status": "ok",
  "reportPath": "/abs/project/build/vmake-build.json",
  "installManifest": "/abs/project/install/manifest.json",
  "invocation": "build",
  "data": {
    "schemaVersion": 2,
    "buildId": "...",
    "status": "succeeded",
    "...": "..."
  },
  "diagnostics": []
}
```

The envelope's `status` is `ok` when `data.status` is `succeeded`; otherwise
`error`. `command` and `invocation` are `build`, `rebuild`, or `test` and match
`data.command`. `reportPath` is the absolute `<root>/build/vmake-build.json` path
this invocation wrote, and `installManifest` is the absolute manifest path for
the invocation's install prefix (`<root>/install/manifest.json` by default, or
`<--prefix>/manifest.json`); both are envelope-only and are not added to the
on-disk report. `diagnostics` is a copy of `data.diagnostics` and is always an
array, so success warnings survive `-q --json`. `data` is the complete report
identical to `build/vmake-build.json`. `--json` does not change the report file,
artifacts, or exit codes; a failed run still exits non-zero after emitting
`status: "error"`. `-q`, `-v`, and `-V` keep controlling stderr verbosity.

## Test reports

`vmake test` writes `command: "test"`: it builds test targets, then runs every
target that is `IsTest()`, `IsDefault()`, and `TargetBinary`. `artifacts` is
empty because test runs never install.

- `status` is `succeeded` only when every executed test passed; `tests` holds
  one entry per executed binary.
- Each `tests[]` entry carries `exitCode` (0 on success, the process exit code
  on failure, 1 when unavailable) and `output`: the combined stdout/stderr tail
  captured for that test, trimmed and bounded to 4096 bytes. Longer output starts
  with `...[truncated]`. `output` is omitted when empty, and entries never
  include vmake's own `PASS`/`FAIL` progress lines. `PASS`/`FAIL` and child
  output keep streaming to the console exactly as before.
- A failing test makes `status` `failed` with `error: "N test(s) failed"` and a
  non-zero exit; the report keeps `summary` test counts because the build phase
  completed.
- When no runnable test target exists, the run fails with `error: "no test
  targets found"`, `status: "failed"`, and `summary.testsFound: 0`.
- `vmake build --tests` is still a build: zero test targets are not an error
  there and test counts stay 0.
- If the build phase itself fails, the report keeps the completed build targets
  and partial `summary` (unless the failure happened before scheduling) along
  with configuration identity and `compileDatabase`. `artifacts` stays empty.
- A running record replaces the previous report before work starts; later runs
  overwrite it with a new `buildId`.

Cross/bare-metal targets cannot be executed by `vmake test`; build them with
`vmake build --tests` instead.

## Clean and stale reports

`vmake clean` removes `build/vmake-build.json` and the stale
`build/compile_commands.json`. When build-key directories for other
configurations remain, it prints one note with their count and suggests
`vmake clean --all` or `vmake distclean`, which remove them. `distclean` and
`pkg clean` also invalidate the report.

Until a later command overwrites it or clean removes it, an existing file is
only the last report, not proof of a current successful build. Consumers should
require `status: "succeeded"`, check the project root and `configSha256`, and
verify artifact SHA-256 values before executing installed files.
