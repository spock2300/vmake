# Build Results and `--json`

`vmake build` (including bare `vmake`) and `vmake rebuild` atomically publish
`build/vmake-build.json` in the project root. `vmake test` publishes its own
`build/vmake-test.json` with `command: "test"` and the same schemaVersion 2
shape. Each file records the latest invocation of its own command only, so a
test run never rewrites the build report and vice versa. They are separate from
the installation manifest (`install/manifest.json`). `clean`, `distclean`, and
`pkg clean` remove both reports.

## Report fields

| Field | Meaning |
|-------|---------|
| `schemaVersion`, `buildId`, `root` | Report schema (2), unique invocation ID, absolute project root |
| `command` | `build` for a build, `rebuild` for `vmake rebuild`, `test` for `vmake test` (the test report lives in `build/vmake-test.json`) |
| `status` | `running`, `succeeded`, `failed`, or `cancelled`; for test, `succeeded` when every executed test passed or (by default) when no test target was declared |
| `started`, `finished`, `durationMs` | UTC timestamps and wall-clock duration; `durationMs` is 0 while running |
| `error` | Concise one-line failure summary (about 300 characters at most); empty on success |
| `diagnostics` | Always an array. Success entries are build-phase tool warnings; failure entries are errors followed by warnings. Each has `severity`, `code`, `message`, and optional `file`, `line`, `column`, `package`, `target`. Codes: `VMAKE_E_COMPILE`, `VMAKE_E_LINK`, `VMAKE_E_BUILD`, `VMAKE_E_SCRIPT`, `VMAKE_E_TEST` for errors, `VMAKE_W_TOOL` for warnings |
| `summary` | Per-invocation counts (see below); present on success, on a failure whose scheduler produced a partial result, and for a test failure after the build phase. Omitted when the failure happened before any target was scheduled |
| `configFile`, `configSha256` | Active configuration name and SHA-256 of the exact configuration bytes used |
| `global`, `packages` | Effective global values and per-package values for the packages that participated |
| `toolchain`, `mode` | Effective toolchain and mode |
| `vmakeVersion`, `vmakeCommit` | Version and abbreviated VCS commit embedded in the running vmake binary (`dev`/`unknown` when unavailable); captured when the report begins and preserved by finish/partial writes |
| `projectRevision`, `projectDirty` | Full git HEAD and whether the project tree had uncommitted changes when the report began. Both are omitted outside a supported git worktree. The dirty scan excludes vmake's own `build/`, `install/`, `.vmake/_locks/`, `.vmake/layout`, `.vmake/vmake.lock`, `.vmake_deps/`, and `vmake_deps/` paths but counts any other untracked file, including source. Recorded observations, not security attestations |
| `compileDatabase` | Absolute path to the compilation database, or empty |
| `targets` | Executed default targets: `package`, `target`, `kind`, `outputs`, `status`, `durationMs`. On failure only targets that completed before the failure are listed. A failure outside target execution (for example a package-level `OnBuild` CMake configure failure) has no target attribution: `diagnostics[].target` stays empty and `targets[]` may stay empty |
| `tests` | Always an array. For `vmake test`, one entry per discovered test target: an executed test has `name`, `package`, `target`, `passed`, `elapsedMs`, `exitCode`, optional `signal` (set when a signal terminated the process), and `output`. After a `-k` build failure, a declared target that did not run has `notRun: true`, `skipReason` (`build failed`, `target not built`, `binary missing`, or `unsupported platform`), `passed: false`, and no output or signal. Partial mode executes only targets recorded `built` or `up-to-date` in this invocation's `targets`; a stale binary from an earlier build is recorded as `target not built` and never run |
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
`changedFiles`, plus `callbacks` (executed `built` void targets), `externalStages` (executed CMake/Make helper stages) and `noOp`.

`targetsBuilt` counts target records with status `built`, including `void` targets whose callbacks always run; `noOp` can be true while `targetsBuilt` is nonzero (for example eight callback targets) but requires `externalStages == 0`; CMake/Make helper work makes it false.

On a failed build or build phase, `summary` describes the work attempted before
the failure: `targetsFailed` counts distinct failed target names from the
diagnostics (or 1 when a target failure is known without a name),
`artifactsInstalled` is 0, and no installation runs. A failed report never says
`noOp: true`: when a failure would otherwise look like no work (for example a
failed `void` callback or CMake configure error before any compilation),
`noOp` is false even when `targetsFailed` is 0 because no target name is
known. A failure before any target is scheduled (for example a buildscript
error) omits `summary` entirely, so it makes no no-op claim either.

Test counts are 0 for build/rebuild. `testsFound` counts discovered test
targets (executed plus not run), while `testsPassed` and `testsFailed` count
executed tests only; not-run entries are not failures. Compile/link counts cover
compile and link actions only, not post-link steps, void callbacks, or
installation. A no-op
build therefore has `filesCompiled: 0`, `filesLinked: 0`, `changedFiles: []`,
and `noOp: true` (only when `externalStages == 0`); it may still have run callbacks and installed artifacts.
`changedFiles` is the sorted, unique list of compiled source paths written as
`package:relative/path` (slash-separated; already absolute or qualified paths
keep their form), and `[]` when nothing compiled.
Counts describe this invocation's work, not repository state.

## Failure diagnostics

Compiler output is parsed from the GNU `file:line[:column]: severity: message`
form; GNU ld's `file:line: undefined reference to ...` / `multiple definition of
...` lines are also recognized. Each diagnostic is attributed to the failing
target and stage; a package-scoped failure outside target execution has an
empty `target`. Located CMake headers (`CMake Warning`,
`CMake Deprecation Warning`, `CMake Dev Warning`, `CMake Author Warning`,
`CMake Warning (dev)`, and `CMake Error` at `file:line`) are recognized as
well: each collects its contiguous indented detail lines (stopping at a blank
or non-indented line) into one message with `file` and `line`, bounded to 1000
characters, and every warning variant has severity `warning` with code
`VMAKE_W_TOOL`. `error` repeats the first parsed error. A command failure with
no parseable location produces one fallback entry: the last
`undefined reference`/`multiple definition` line, then the last `error:` line,
then the trimmed output tail (up to 1000 characters). Buildscript/API errors use
`VMAKE_E_SCRIPT` and carry the package name. A `vmake test` failure that has no
build diagnostic (a failed test, `--require-tests` with no runnable test target,
or a test preflight refusal) gets exactly one `VMAKE_E_TEST` error entry whose
message is the same concise summary carried by `error`; per-test details stay in
`tests[]`. Raw tool output and the full failing command stay on stderr; they are
not stored in the report.

## Success warnings

A succeeded build or test run collects the warnings streamed to stderr during
the build phase into `diagnostics`. The capture is cut before `vmake test` runs
test binaries, so test program stdout/stderr (including the stdout redirect used
by `--json`) never becomes a `VMAKE_W_TOOL` entry; build-phase warnings and a
failure before test execution keep theirs. GNU `file:line[:column]: warning: message`
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

## Console output

The default console view is action lines plus the final summary. Per-target
`[package:target]` headers are verbose detail: they print only with `-v`, while
compile/link/generate/install actions and buildscript prints still appear at
default. The report and `summary.changedFiles` carry package/target context.

A successful build/rebuild ends with exactly one `Build succeeded: ...` summary
line; the separate `Build succeeded!` progress line no longer exists.
`vmake build --install` still prints `Install succeeded!` after the build phase.

`Downloading package sources...` appears only when a needed source actually needs materialization or a remote resolve; no-op builds omit the heading. CMake helper output is captured. `CMakeConfigure` prints one
`cmake configure: done` line (or `cmake configure (cached): <dir>` when its
fingerprint is unchanged); `CMakeBuild`/`CMakeInstall` print step and
installed/up-to-date counts. CMake `-- ...` configure scaffolding, Ninja
progress and `-- Up-to-date` install lines are hidden from default output
(Ninja steps and installed counts are summarized), and the full captured log is
printed with `-v`. CMake warning/error lines, including compiler
`file:line: warning:` lines from the build stage, stream to stderr instead of
stdout, so the warning capture can report them as `VMAKE_W_TOOL` diagnostics.
With `-q`, a successful configure prints nothing on stdout; warnings and errors
still stream to stderr. A CMake stage running longer than 10 seconds prints
`  cmake <stage>: still running (Ns)` to stderr every 10 seconds, including
under `-q`; fast stages print no heartbeat.

## Build check (plan mode)

`vmake build --check` (alias `--plan`) resolves the project like a build but
evaluates action signatures/records without compiling, linking, running `void`
callbacks or post-link steps, writing action records, installing, or writing
`build/vmake-build.json`/`compile_commands.json`. Generator rules and
config-header generation are skipped, so sources produced by generators may be
undercounted; check is a plan, not a guarantee. A default target with generator
rules or in a package with a generated config header counts once in
`plannedGenerators`, even with several rules, and the human plan line ends with
`; N generated-input target(s) not checked` in both pending and up-to-date
forms. `workPending` means `plannedCompiles + plannedLinks > 0`;
`plannedCallbacks` counts default `void` targets whose callbacks would run.
Exit is 0 when planning succeeds even with pending work, 1 on planning errors,
2 for `--check` with `--install`/`--tests`. Human output is one line; `--json`
emits `{schemaVersion:1,command:"check",status,data:{workPending,
plannedCompiles,plannedLinks,plannedCallbacks,plannedGenerators,caveats,
upToDate,targetsTotal,vmakeVersion,vmakeCommit},diagnostics}`;
`plannedGenerators`/`caveats` are omitted when zero/empty, and
`vmakeVersion`/`vmakeCommit` identify the running binary on every check
document (check writes no build report).

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
`data.command`. `reportPath` is the absolute report path this invocation wrote:
`<root>/build/vmake-build.json` for build/rebuild and
`<root>/build/vmake-test.json` for test. `installManifest` is the absolute
manifest path for the invocation's install prefix
(`<root>/install/manifest.json` by default, or `<--prefix>/manifest.json`);
both are envelope-only and are not added to the on-disk report. `diagnostics`
is a copy of `data.diagnostics` and is always an array, so success warnings
survive `-q --json`. `data` is the complete report identical to the file named
by `reportPath`. `--json` does not change the report file,
artifacts, or exit codes; a failed run still exits non-zero after emitting
`status: "error"`. `-q`, `-v`, and `-V` keep controlling stderr verbosity.

## Test report

`vmake test` writes `build/vmake-test.json` with `command: "test"`: it builds
test targets, then runs every target that is `IsTest()`, `IsDefault()`, and
`TargetBinary`. `artifacts` is empty because test runs never install. The build
report from an earlier `vmake build` is left byte-for-byte intact for every test
outcome.

- `status` is `succeeded` when every executed test passed; a host project with
  no runnable test target also succeeds unless `--require-tests` is given. A
  `-k` build-phase failure stays `failed` even when surviving test binaries
  execute and pass; the build error remains `error`.
- Each `tests[]` entry carries `exitCode` (0 on success, the process exit code
  on failure, -1 when a signal terminated it, 1 when unavailable), `signal`
  (omitted unless a signal terminated the process; platform wording such as
  `segmentation fault` or `killed`), and `output`: the combined stdout/stderr
  tail captured for that test, trimmed and bounded to 4096 bytes. Longer output
  starts with `...[truncated]`. `output` is omitted when empty, and entries never
  include vmake's own `PASS`/`FAIL` progress lines. `PASS`/`FAIL` and child
  output keep streaming to the console exactly as before, but test program
  output is never parsed as a build warning: the stderr warning capture is cut
  before tests run.
- Test binaries execute with their working directory set to the package's
  `BuildDir()` (the directory containing the test executable), so relative paths
  in a test program resolve there.
- The report is the only overall test summary line; there is no separate
  `N/M test(s) passed` stdout line. Per-test `PASS`/`FAIL` lines remain.
- A failing test makes `status` `failed` with `error: "N test(s) failed"` and a
  non-zero exit; the report keeps `summary` test counts because the build phase
  completed. A test-stage failure with no build diagnostic (failed test,
  `--require-tests` no-tests, or preflight refusal) also records one
  `VMAKE_E_TEST` error diagnostic, so the `--json` envelope's `diagnostics` is
  never empty when `status` is `error`.
- A project with no runnable test target is detected before the build phase:
  it reports `status: "succeeded"`, `summary.testsFound: 0`, prints
  `Tests succeeded: no test targets`, and exits 0 without compiling anything.
  Pass `--require-tests` to make this case fail with
  `error: "no test targets found"`, `status: "failed"`, and exit 1 without
  building (useful in CI). If target declaration cannot be trusted (missing
  sources or a script error), vmake falls back to the normal build path.
- When runnable tests exist, preflight checks the effective root platform. If it
  cannot execute tests (`target_os` is `none`, differs from the host, or
  `target_triple` is non-empty), the run exits 1 with the
  `cannot run tests for project target ... use 'vmake build --tests'` message,
  writes a failed test report, and does not compile anything or create the
  compilation database. `vmake build --tests` is still a build: zero test
  targets are not an error there and test counts stay 0.
- If the build phase itself fails, the report keeps the completed build targets
  and partial `summary` (unless the failure happened before scheduling) along
  with configuration identity and `compileDatabase`. `artifacts` stays empty.
- With `-k`, a build-phase failure still runs the test binaries this invocation
  built successfully: only targets recorded as `built` or `up-to-date` in the
  report's `targets` are executed, and a stale binary left by an earlier build
  gets `skipReason: "target not built"` instead of running. Executed entries
  keep their normal pass/exit/signal/output fields; declared targets that did
  not run get `notRun: true` and `skipReason` and do not count as failures. The report stays `failed` with the build error as `error`, and the
  command exits 1. Without `-k`, a build-phase failure records no test entries.
- A running test record replaces the previous test report before work starts;
  later test runs overwrite it with a new `buildId`. The build report keeps its
  own build ID.
- `vmake inspect` exposes the two documents separately: `buildReport`/
  `buildReportPresent`/`buildReportState` read `build/vmake-build.json`, and
  `testReport`/`testReportPresent`/`testReportState` read
  `build/vmake-test.json` (see `references/inspect.md`).

Cross/bare-metal targets cannot be executed by `vmake test`; build them with
`vmake build --tests` instead.

## Clean and stale reports

`vmake clean` removes `build/vmake-build.json`, `build/vmake-test.json`, and the
stale `build/compile_commands.json`. When build-key directories for other
configurations remain, it prints one note with their count and suggests
`vmake clean --all` or `vmake distclean`, which remove them. `distclean` and
`pkg clean` also remove both reports.

Until a later command overwrites it or clean removes it, an existing file is
only the last report, not proof of a current successful build. Consumers should
require `status: "succeeded"`, check the project root and `configSha256`, and
verify artifact SHA-256 values before executing installed files.
