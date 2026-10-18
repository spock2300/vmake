# vmake inspect

`vmake inspect` prints the resolved project model as a single JSON document on
stdout. It is the machine-readable counterpart of `vmake query`: it performs
resolution and `OnConfig`/`OnBuild` declaration only, so it never compiles,
links, installs, or writes `build/`, `install/`, or `.vmake/config.json`. It also
reports needed packages whose source tree is not on disk yet, without fetching
or materializing them, and the install plan declared by `OnBuild` (extra
`AddInstalls` entries and predicted prefix-relative target paths) without
reading or creating `install/`.

The model also reports whether `build/compile_commands.json` and the last
`build/vmake-build.json` and `build/vmake-test.json` currently exist, each
report's identity and whether its configuration digest matches the active
configuration, and each needed package's active build-key directory. The build
report and test report are written by separate invocations; inspect reads both
without merging them. A `freshness` object adds a source-staleness heuristic
computed from the build report's finish time and the declared files of needed
packages. These fields describe files, resolved layout, and declared source
modification times only: inspect still does not verify that actions or outputs
are fresh, does not read action records, and does not predict which targets are
up to date. It never writes the compile database or either report.

```
vmake inspect [--set [package/]option=value ...] [-C <dir>]
```

`-C`/`--project` is the global project-root override: it resolves against the
process cwd, must be an existing directory with `.vmake/`, `build.go`, or an
immediate child containing `build.go`, and is used as the project root without
changing the process cwd. An invalid selection is a usage error (exit 2).

`--set` may be repeated. Each override applies to this run only: the in-memory
configuration is evaluated as if the project had those values, but nothing is
persisted. The effective overrides are echoed in `data.overrides`, using the
option's natural JSON type.

Values are coerced with the option's declared type, matching `vmake config
--set`:

- `OptionBool`: `true`/`false`, `on`/`off`, and `1`/`0` (case-insensitive).
- `OptionInt`: numeric text whose value is integral; `42`, `42.0`, and `1e3`
  are accepted, `quick` and `1.5` are rejected.
- `OptionString`: the raw text, so `42`, `true`, and `on` stay strings.
- `OptionChoice`: the raw text, validated against the declared choice set.

The declared type is discovered by a first resolution pass (`Require` +
`Configure` without overrides). A second, fresh context applies the typed
overrides before `Configure` and continues as usual. `inspect --set` therefore
resolves dependencies and runs `OnConfig`/`OnApply` twice (roughly 2x resolution)
and may repeat any side effects those callbacks have; plain `inspect` remains a
single pass. If an option is not declared in the first pass because the
hypothetical override changes dependency selection, its raw text is stored and
the post-`Configure` validation reports a type mismatch. Overrides are validated
after the second `Configure`:

- unknown package or unknown option,
- a package-prefixed global option,
- a value whose type does not match the declared option type,
- an `OptionChoice` value outside the declared choice set,

produce a `VMAKE_E_OPTION` diagnostic and exit status 1. The reference
implementation silently ignored unknown overrides; vmake rejects them.

## Output isolation

Buildscript `fmt.Println`/`os.Stdout` writes cannot corrupt the JSON document.
For the whole resolution and target-declaration phase (both `--set` passes,
`Require`, `Configure`, inspection, and dry-run `OnBuild`), `inspect` points
`os.Stdout` at the null device and restores the real stdout before writing the
envelope or warnings. The same restoration applies to error paths, so a failing
run still emits exactly one JSON document followed by the one-line `Error:` on
stderr. Buildscript stdout is discarded, not forwarded. Stderr is temporarily
suppressed while `Require`/`Configure` run (so interpreter panic traces cannot
corrupt the human error line) and is then restored for the documented
`Warning:`/`Error:` lines; it is not a general buildscript log for `inspect`.

## Envelope

Success:

```json
{
  "schemaVersion": 1,
  "command": "inspect",
  "status": "ok",
  "data": { "...": "..." },
  "diagnostics": []
}
```

Failure (no `data`; one human-readable line is written to stderr):

```json
{
  "schemaVersion": 1,
  "command": "inspect",
  "status": "error",
  "diagnostics": [
    {
      "severity": "error",
      "code": "VMAKE_E_INSPECT",
      "message": "load project: yaegi eval project: ..."
    }
  ]
}
```

`diagnostics` is always an array, never `null`. Exit status is 0 for a model
that could be produced (including models carrying warnings) and 1 when the
model cannot be produced.

A package whose `OnBuild` callback fails while targets are being declared does
not abort the model. That package's targets are omitted, the command exits 0,
and one warning is appended:

```json
{
  "severity": "warning",
  "code": "VMAKE_W_TARGET_DECLARE",
  "message": "cannot declare targets for <package>: <error>",
  "fix": "Fix the package OnBuild callback, then rerun vmake inspect."
}
```

Target declaration is skipped when a needed package's source tree is missing
(see `VMAKE_W_SOURCE_MISSING` below): the package's targets are omitted and no
`VMAKE_W_TARGET_DECLARE` warning is produced, because a dry-run `OnBuild`
failure in that state reflects the absent tree, not a broken callback. Only the
source-missing warning is reported for such a package.

A needed package whose expected source tree is missing or empty gets one
warning. Inspect checks the tree without running git, touching the network,
creating `<package>/src` links, or writing `.vmake_deps`; a missing tree never
fails the model. Expected paths:

- local `SetGit` packages: `<package>/src`,
- remote packages: `<root>/.vmake_deps/<repo>/<owner>/src[/<member>]`,
- plain local packages: the package directory.

```json
{
  "severity": "warning",
  "code": "VMAKE_W_SOURCE_MISSING",
  "message": "package hostap source is not materialized; the next build will fetch https://git.w1.fi/hostap.git (version 2.12)",
  "package": "hostap"
}
```

The message includes the resolved URL and version when available. Warnings keep
`status: "ok"` and exit 0, and each diagnostic produces one `Warning:` line on
stderr.

The same non-fetching check backs `vmake query` and `vmake query targets`: each
prints one `Warning: ...` line per missing needed package to stderr, keeps the
tree or target list unchanged on stdout, exits 0, and does not fetch. Query also
skips dry-run target declaration for packages whose source tree is missing, so
they keep their untargeted tree label (or are omitted from `query targets`)
instead of surfacing an `OnBuild` panic caused only by the absent tree. For
packages whose source is present, a target-declaration failure does not abort
query either: it prints one
`Warning: cannot declare targets for <package>: <error>` line to stderr, omits
that package's targets or kind label, and continues with the remaining
packages. The warning is emitted once per package.

`build/vmake-build.json` or `build/vmake-test.json` may exist but be unreadable
or invalid JSON (for example a truncated file left by an interrupted writer, or
a hand-edited file). Inspect keeps the matching `buildReportPresent: true` /
`testReportPresent: true`, omits that document's state object, and appends one
warning per unreadable document; it never rewrites or removes either file:

```json
{
  "severity": "warning",
  "code": "VMAKE_W_REPORT_UNREADABLE",
  "message": "cannot read build report <root>/build/vmake-build.json: <parse error>",
  "fix": "Rerun vmake build to rewrite build/vmake-build.json."
}
```

A test report warning names `<root>/build/vmake-test.json` and suggests rerunning
`vmake test`. Valid reports that predate the current configuration still yield
their state objects; `configMatches` reports the mismatch independently. A
report is not evidence that the recorded build/test still matches the worktree:
inspect checks only the configuration digest for that, while the separate
freshness heuristic compares declared source modification times. Neither is an
action- or output-freshness check.

## `data` fields

| Field | Type | Meaning |
|-------|------|---------|
| `root` | string | Absolute project root (the directory containing `.vmake/`) |
| `configFile` | string | Active configuration file name under `.vmake/` |
| `configSha256` | string | SHA-256 of the active configuration file; the hash of empty content when absent |
| `mode` | string | Effective build mode (`debug`, `release`, `size`) |
| `toolchain` | string | Effective project toolchain name |
| `toolchainInfo` | object | `{name, targetOs?, targetTriple?, installPath?, cc?, cxx?, ar?, ld?}`; optional fields are omitted when empty |
| `global` | object | Effective global option values, including `mode` and `toolchain` |
| `overrides` | object | Coerced `--set` values keyed `[package/]option`; omitted when no override was given |
| `roots` | array | Needed local packages that no resolved package depends on, sorted |
| `packages` | array | One entry per resolved package, sorted by name |
| `graph` | array | Package dependency edges `{from, to}`, sorted by `from` then `to` |
| `compileDatabase` | string | `<root>/build/compile_commands.json` (the expected path, even before a build) |
| `compileDatabasePresent` | bool | Whether `<root>/build/compile_commands.json` currently exists; presence only, not freshness |
| `buildReport` | string | `<root>/build/vmake-build.json` (see `docs/BUILD_RESULT.md`) |
| `buildReportPresent` | bool | Whether `<root>/build/vmake-build.json` currently exists |
| `buildReportState` | object? | Identity and result recorded by the build report; omitted when the report is absent or unreadable (see below) |
| `freshness` | object | Source-freshness heuristic from the build report's finish time and declared target files (see below); additive to schemaVersion 1 |
| `testReport` | string | `<root>/build/vmake-test.json` |
| `testReportPresent` | bool | Whether `<root>/build/vmake-test.json` currently exists |
| `testReportState` | object? | Identity and result recorded by the test report; same shape as `buildReportState`, omitted when absent or unreadable |
| `installPrefix` | string | `<root>/install` (the default `--prefix`, even before an install) |
| `installManifest` | string | `<root>/install/manifest.json` (the expected default manifest path, even before an install) |

## Build and test state

`buildReportState` is present when `build/vmake-build.json` exists and parses as
a JSON object; `testReportState` is present for `build/vmake-test.json` under
the same rule. They are independent snapshots of separate invocations, and the
same object shape is used for both. Each copies the report's identity fields:

| Field | Type | Meaning |
|-------|------|---------|
| `status` | string | `succeeded`, `failed`, `cancelled`, or `running` as recorded by the report |
| `buildId` | string | Report build ID |
| `command` | string? | `build`/`rebuild` for the build report, `test` for the test report; omitted when the report lacks it |
| `started`, `finished` | string | RFC 3339 timestamps recorded in the report; `finished` may be empty while a build is running |
| `configMatches` | bool | Whether the report's `configSha256` equals `data.configSha256` and no `--set` override was supplied |
| `summary` | object? | The report's `summary` object copied verbatim; omitted when the report has none |
| `error` | string? | The report's concise error; omitted when empty |

`configMatches` is deliberately conservative: an existing report from the same
configuration file reports `true`, while any `--set` run reports `false` because
the overrides model a hypothetical configuration whose effective values may
differ from the file hash recorded by the build. A false value means "do not
assume this report describes the configuration being inspected"; it does not by
itself mean the build is stale.

`compileDatabasePresent`, `buildReportPresent` and `testReportPresent` are
stat-only checks. A clean tree reports all three `false`, omits both state
objects, and reports `freshness.known: false`; a successful build normally
leaves the database and build report
behind; a subsequent `vmake test` adds the test report without changing the
build report. `clean` removes both reports and the database again. Inspect never
creates, updates, or repairs any of these files. `vmake build --tests` records
test targets in the build report only, so it does not create a test report.

## Freshness heuristic

`data.freshness` is an additive field (schemaVersion 1, existing fields
unchanged) that gives a cheap source-staleness signal without running a build.
Consumers that compare inspect documents across versions should ignore unknown
data keys rather than reject the document:

```json
"freshness": {
  "known": true,
  "reportStatus": "succeeded",
  "reportFinished": "2026-10-19T02:00:00.123456789Z",
  "configMatches": true,
  "sourceChanged": true,
  "changedSources": ["firmware:src/main.c", "firmware:src/uart.c"],
  "truncated": false
}
```

It is computed from the build report's `finished` timestamp and the declared
`files` of targets in needed packages whose source tree is present. Those
patterns are expanded with the build's glob rules (including exclusions), and
each existing matched file is stat-ed and compared with the report finish time;
file contents are not read or hashed. Missing declared files are skipped
silently; a missing source tree is covered by `VMAKE_W_SOURCE_MISSING` instead.

- `known` is false when there is no build report, the report is unreadable, or
  its `finished` field is empty or not an RFC 3339 timestamp. When a report
  parses, `reportStatus` and `configMatches` are still reported and
  `reportFinished` is present only for a parseable timestamp; `sourceChanged`,
  `changedSources` and `truncated` are omitted.
- `reportStatus` and `reportFinished` copy the build report's status and finish
  time; a failed report with a parseable finish is still `known: true`.
- `configMatches` mirrors `buildReportState.configMatches`: false after a
  configuration change or any `--set` run. A false value still leaves
  `sourceChanged` computed, but the configuration mismatch alone requires a
  rebuild, so do not read `sourceChanged: false` as "no build needed".
- `changedSources` lists `pkg:path` entries, where `path` is relative to that
  package's source directory and slash-separated, matching the
  `summary.changedFiles` style. It contains declared files whose modification
  time is strictly newer than the report finish, de-duplicated, sorted, and
  limited to 50 entries; it is omitted when empty. `truncated` is true when more
  files changed.
- `sourceChanged` is true when any declared file is newer, including files
  beyond the reported limit.

This is a heuristic, not an up-to-date check and not a build plan. It does not
capture flag, toolchain, or buildscript changes, generated inputs, headers or
other files a target did not declare, deleted files, content edits that preserve
or backdate a modification time, or clock/file-system skew. `vmake build`
remains authoritative: `sourceChanged: false` does not prove the next build is a
no-op.

## `packages` entries

| Field | Type | Meaning |
|-------|------|---------|
| `name` | string | Package name |
| `kind` | string | `local`, `native`, or `registry` |
| `repo` | string? | Repository name for remote packages |
| `version` | string? | Selected version (config pin, lockfile, or native selection) |
| `commit` | string? | Resolved commit for remote packages |
| `url` | string? | Resolved source URL for remote packages |
| `path` | string? | Local packages: source directory relative to `root` (slash-separated) |
| `sourceDir` | string? | Absolute source directory |
| `sourcePresent` | bool | Whether the expected source tree exists and is non-empty (see `VMAKE_W_SOURCE_MISSING`) |
| `buildDir` | string? | Absolute active build-key directory from the resolved layout; present for needed packages with a layout, even before the directory exists |
| `buildKey` | string? | Build key (the last path element) for local packages; omitted for remote packages, whose `buildDir` ends in `build` |
| `needed` | bool | Whether the package is reachable from the build roots |
| `description` | string? | `SetDescription` value |
| `toolchain` | string? | Per-package toolchain, present for needed packages |
| `options` | object? | Declared options of this package with effective values (global declarations included); omitted when none |
| `deps` | array | Package dependency names, sorted; always present |
| `targets` | array? | Declared targets; present only for needed packages and omitted when the package declares none or its source tree is missing |

`buildDir` comes from the same layout used by `RunBuild`, so it names the
directory the next build would use for the current mode, options, flags,
toolchain and source identity. It is a prediction, not proof: before a first
build (and after `clean`) the directory may not exist. Inspect does not read or
verify its contents, does not expose action records, and does not claim any
target is up to date. `buildKey` is reported only when the key is unambiguously
the last element of `buildDir` (local packages); remote build directories are
`<root>/.vmake_deps/.../out/<owner>/<key>/build`, so their basename is `build`
and the field is omitted.

## `targets` entries

Targets are sorted by name. Argument lists such as `files` or `deps` keep the
order the buildscript declared.

| Field | Type | Meaning |
|-------|------|---------|
| `name`, `kind` | string | Target name and kind (`binary`, `static`, `shared`, `object`, `void`) |
| `default`, `test` | bool | Default build membership and test-target flag |
| `files`, `excludedFiles`, `includes`, `publicIncludes`, `defines` | array? | Declared inputs and compile settings |
| `languages`, `links` | array? | Declared language overrides and link libraries |
| `deps` | array? | Declared target dependencies (`pkg:target` or `:target`) |
| `postLinkOutputs` | array? | Declared post-link output patterns |
| `output` | string? | Artifact name from `api.TargetFilename(kind, name, target_os)`; empty and omitted for `void` |
| `installDir` | string? | Per-target install subdirectory override |
| `noInstall` | bool? | True when the target opts out of installation |
| `installs` | array? | Package-level `AddInstalls` entries `{source, dest}` declared by the dry-run `OnBuild`; omitted when none |
| `installPath` | string? | Predicted prefix-relative, slash-separated install path (`bin/app`, `lib/libfoo.a`); omitted for `void`, `object`, `noInstall`, and test targets |

`installs` is package-level: every target of a package that called
`AddInstalls` in `OnBuild` carries the same entries, because the API does not
associate extra install items with a target. `source` is exactly as declared
(usually relative to the package `SourceDir()`), and `dest` is relative to the
install prefix. `OnInstall` is not executed during inspection, so entries
declared there are absent.

`installPath` uses `target.InstallDir` when set and otherwise the kind's
default directory (`bin` for binaries, `lib` for static and shared libraries).
It is relative to the install prefix; `--prefix` and package
`InstallContext.SetPrefix` are not resolved. Post-link outputs are installed
beside the target artifact (for example `install/bin/firmware.hex` next to
`install/bin/firmware`) and are not repeated in `installPath`. Static libraries
are copied only with `--install-type sdk`; test targets are never installed.
`installPath` is a prediction, not evidence that the artifact exists. These
install fields are additive to schemaVersion 1; existing fields are unchanged.

Targets are listed only for needed packages. A package pruned by `FilterDeps`
(for example a conditional or test-only package that is disabled in the current
configuration) has no `targets`, even though it remains in `packages` and the
graph. A needed package whose source tree is missing also has no `targets`:
inspect does not run its dry-run `OnBuild` declaration, and the only warning is
`VMAKE_W_SOURCE_MISSING`. `files` keeps the declared globs; inspect does not
expand them to concrete files. Inspect does not build or install, so `output`,
`installDir`, and `installPath` are predictions, not evidence that an artifact
exists.

## Determinism

Output is deterministic for the same project, configuration, overrides, build
report, and source modification times: packages and roots are sorted by name,
graph edges by `(from, to)`, freshness sources are de-duplicated and sorted, and
JSON object keys follow Go's `encoding/json` ordering. Consecutive runs are
byte-identical. Build- and test-state fields are snapshots of the report and
compile database files, so they stay byte-identical until one of those files
changes. The freshness heuristic changes when the build report or a declared
source's modification time changes.

## Exit status and stderr

| Situation | stdout | exit | stderr |
|-----------|--------|------|--------|
| Model produced | `status: "ok"` envelope | 0 | empty |
| Model produced with warnings | `status: "ok"` envelope with warning diagnostics | 0 | one `Warning: ...` line per warning |
| `Require`/`Configure`/inspection failure | `status: "error"`, no `data` | 1 | one `Error: ...` line |
| Invalid `--set` | `status: "error"`, `code: VMAKE_E_OPTION` | 1 | one `Error: ...` line |

Warnings cover target-declaration failures (`VMAKE_W_TARGET_DECLARE`), missing
source trees (`VMAKE_W_SOURCE_MISSING`), and unreadable build or test reports
(`VMAKE_W_REPORT_UNREADABLE`). Buildscript stdout is always discarded, including
on failure; stdout contains one JSON document in every row.

## Side effects

`inspect` acquires the project lock and performs dependency resolution like
`query`. The missing-source check only reads directories: it does not run git,
touch the network, create `<package>/src` links, or write `.vmake_deps`.
Resolution of native remote packages still follows its normal rules and may
materialize that package's tree. `inspect` does not read or create `install/`
and does not resolve the install prefix: `installPrefix` and `installManifest`
are the expected default paths, while `installs` and `installPath` come from
dry-run `OnBuild` declarations. Inspect stats `build/compile_commands.json`,
reads `build/vmake-build.json` and `build/vmake-test.json` to report their
state, and expands and stats declared source files for the freshness heuristic.
It never writes, updates, or deletes the compile database, reports, or sources,
and it does not inspect action records or outputs. It also does not modify the
active configuration. With `--set`, resolution and
`OnConfig`/`OnApply` run twice as described above. The lock/state files it may
create under `.vmake/` are the same ones every read-only command creates.
