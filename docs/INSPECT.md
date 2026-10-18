# vmake inspect

`vmake inspect` prints the resolved project model as a single JSON document on
stdout. It is the machine-readable counterpart of `vmake query`: it performs
resolution and `OnConfig`/`OnBuild` declaration only, so it never compiles,
links, installs, or writes `build/`, `install/`, or `.vmake/config.json` itself.
Dry-run `OnBuild` callbacks run under a read-only interpreted-script file API
(see [Read-only dry-run](#read-only-dry-run)), so wrapped file writes fail
instead of touching the project; APIs outside the wrapped set remain a script's
responsibility. It also reports needed packages whose source tree is not on disk
yet, without fetching or materializing them, and the install plan declared by
`OnBuild` (extra `AddInstalls` entries and predicted prefix-relative target
paths) without reading or creating `install/`. The model is a declaration-time
view, not a build plan: the target `files` field keeps the declared patterns
while `resolvedFiles` adds a bounded resolution of those patterns, and
`cFlags`/`cxxFlags`/`ldFlags` keep the declared flag strings without merged
globals or action signatures (see [`targets` entries](#targets-entries)).

The model also reports whether `build/compile_commands.json` and the last
`build/vmake-build.json` and `build/vmake-test.json` currently exist, each
report's identity, whether its configuration digest matches the active
configuration file, and whether the effective configuration (the file plus any
no-op `--set` overrides) matches. It also reports each needed package's active
build-key directory. The build report and test report are written by separate
invocations; inspect reads both without merging them. A `freshness` object adds
a source-staleness heuristic computed from the build report's finish time and
the declared files of needed packages. These fields describe files, resolved
layout, declared source modification times, and declaration-time resolved file
sets: inspect still does not verify that actions or outputs are fresh, does not
read action records, and does not predict which targets are up to date. It
never writes the compile database or either report.

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

Each report state's `configMatches` stays false for any `--set` run, because the
report records only the configuration-file digest. The additive
`effectiveConfigMatches` fields answer the effective question: they are true
when that digest matches **and** every override equals the value already in
effect (the file's value or, when the file has none, the declared default).
Probing an option with the value it already has therefore keeps the effective
match bit while the strict file-level bit remains false.

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

## Read-only dry-run

Inspect enables a process-wide read-only mode for interpreted-script file IO
before it starts resolution and restores it with a deferred reset on every
return path. While the mode is enabled, these wrapped `os` symbols fail without
touching the filesystem, returning

```
read-only inspection: interpreted script write to <resolved absolute path> is not allowed
```

- `WriteFile`, `Create`, `CreateTemp`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`;
- `Rename`, whose error names both the old and the new path;
- `OpenFile` when the flags contain any of `O_WRONLY`, `O_RDWR`, `O_CREATE`,
  `O_TRUNC`, `O_APPEND`. `O_RDONLY` opens still work.

`--allow-dry-run-writes` disables this guard for projects whose callbacks must materialize generated inputs before declaring targets. It may modify the build tree; use it only when those writes are expected, and do not rely on the resulting generated files as build inputs.

Reads, `Stat`/`Lstat`, `ReadDir`, and `Walk`/`WalkDir` continue to work. Writes
performed by vmake itself (locks, plugin state, or the JSON document) do not go
through the script API and are unaffected. Existing files are not truncated,
renamed, or deleted by a rejected call, so an inspect run after a build leaves
build-generated files in place.

A dry-run callback that attempts a wrapped write fails its package's target
declaration: the package's targets are omitted and one
`VMAKE_W_TARGET_DECLARE` warning carries the read-only message (see below).
This happens even when the callback ignores the returned error and continues;
inspect records the attempt and turns it into a declaration failure, so an
optimistic `_ = os.WriteFile(...)` in `OnBuild` cannot silently appear to
succeed. The callback may keep running, but the write did not happen. When one
or more packages are omitted this way, inspect appends one additional aggregate
`VMAKE_W_READ_ONLY_TARGETS` warning after the per-package warnings; the
per-package warnings are retained and the aggregate reports the omitted-package
count. This guard covers the wrapped symbols only: a script that reaches
unwrapped stdlib APIs such as `os.MkdirTemp`, `ioutil.*`, or a subprocess is not
sandboxed, and a script that uses them can still write during inspection. The
mode is also active while `Require`/`Configure` run, so wrapped writes there are
rejected too, but only dry-run `OnBuild` attempts are attached to a
target-declaration warning.
Normal `build`/`rebuild` never enables the mode, so scripts that generate files
in a real build are unaffected.

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
source-missing warning is reported for such a package. A declaration warning
that mentions `read-only inspection: interpreted script write to ... is not
allowed` means the callback tried to write a file while inspect held the
read-only guard; the file was not created or removed.

The aggregate warning is additional to the per-package diagnostics:

```json
{
  "severity": "warning",
  "code": "VMAKE_W_READ_ONLY_TARGETS",
  "message": "omitted targets for 2 package(s) because dry-run callbacks attempted writes",
  "fix": "Guard generated-file work with p.DryRun(), or run vmake inspect --allow-dry-run-writes when those writes are expected."
}
```

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
packages. The warning is emitted once per package. When one or more of those
declarations failed because the read-only guard rejected a script write, both
`query` and `query targets` print one aggregate summary line to stderr after
the target listing, exit 0, and keep the per-package warnings:

```
Warning: omitted targets for 2 package(s) because dry-run callbacks attempted writes; fix: Guard generated-file work with p.DryRun(), or run vmake inspect --allow-dry-run-writes when those writes are expected.
```

The count is the number of packages whose targets were omitted, and the summary
line is printed once, after everything on stdout. It is absent when no
read-only declaration failure occurred.

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
| `mode`, `toolchain` | string? | The report's recorded build mode and toolchain; omitted when the report lacks them |
| `modeMatches`, `toolchainMatches` | bool | Whether the report's recorded mode/toolchain equals the inspected one (missing values count as matching) |
| `configMatches` | bool | Whether the report's `configSha256` equals `data.configSha256` and no `--set` override was supplied |
| `effectiveConfigMatches` | bool | Whether the report's `configSha256` equals `data.configSha256`, the recorded mode/toolchain match, and every `--set` override is a no-op against the current effective value |
| `summary` | object? | The report's `summary` object copied verbatim; omitted when the report has none |
| `error` | string? | The report's concise error; omitted when empty |

`configMatches` is deliberately conservative: an existing report from the same
configuration file reports `true`, while any `--set` run reports `false` because
the overrides model a hypothetical configuration whose effective values may
differ from the file hash recorded by the build. A false value means "do not
assume this report describes the configuration being inspected"; it does not by
itself mean the build is stale.

`effectiveConfigMatches` is the override-aware counterpart. It is true only
when the digest matches, the recorded mode/toolchain match the inspected ones,
and every `--set` override equals the value already in effect, so a probe such
as `--set firmware/diagnostics=true` on a configuration that already enables
`diagnostics` leaves it true. It is false when an override changes an effective
value, when the digest differs, or when the report was produced with another
mode/toolchain. Values are compared after option-type normalization against the
configuration file's value or, when the file has none, the declared default; a
missing effective value is not a no-op.
`effectiveConfigMatches` is additive to schemaVersion 1: `configMatches` keeps
its strict file-level meaning, and consumers that compare inspect documents
across versions should ignore unknown data keys rather than reject the document.

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
  "effectiveConfigMatches": true,
  "sourceChanged": true,
  "changedSources": ["firmware:src/main.c", "firmware:src/uart.c"],
  "truncated": false
}
```

It is computed from the build report's `finished` timestamp and the declared
`files` of targets in needed packages whose source tree is present. Those
patterns are expanded with the build's glob and exclusion rules (the same
expansion that produces target `resolvedFiles`), paths under the active
`buildDir` are skipped, and each remaining existing matched file is stat-ed and
compared with the report finish time; file contents are not read or hashed.
Missing declared files are skipped silently; a missing source tree is covered by
`VMAKE_W_SOURCE_MISSING` instead.

- `known` is false when there is no build report, the report is unreadable, or
  its `finished` field is empty or not an RFC 3339 timestamp. When a report
  parses, `reportStatus`, `configMatches` and `effectiveConfigMatches` are still
  reported and `reportFinished` is present only for a parseable timestamp;
  `sourceChanged`, `changedSources` and `truncated` are omitted.
- `reportStatus` and `reportFinished` copy the build report's status and finish
  time; a failed report with a parseable finish is still `known: true`.
- `configMatches` mirrors `buildReportState.configMatches`: false after a
  configuration change or any `--set` run. A false value still leaves
  `sourceChanged` computed, but the configuration mismatch alone requires a
  rebuild, so do not read `sourceChanged: false` as "no build needed".
- `effectiveConfigMatches` mirrors `buildReportState.effectiveConfigMatches`:
  true when the digest matches and every `--set` override restates the value
  already in effect, so probing an option with its current value keeps this bit
  true while `configMatches` stays false. It is additive to schemaVersion 1.
- `changedSources` lists `pkg:path` entries, where `path` is relative to that
  package's source directory and slash-separated, matching the
  `summary.changedFiles` style. It contains declared files whose modification
  time is strictly newer than the report finish, de-duplicated, sorted, and
  limited to 50 entries; it is omitted when empty. `truncated` is true when more
  files changed.
- `sourceChanged` is true when any declared file is newer, including files
  beyond the reported limit.

This is a heuristic, not an up-to-date check and not a build plan. It compares
modification times only: it does not capture toolchain or buildscript/action
changes, generated inputs, headers or other files a target did not declare,
deleted files, content edits that preserve or backdate a modification time, or
clock/file-system skew. Declared target flags are reported separately in
`cFlags`/`cxxFlags`/`ldFlags`, so an `AddCFlags` edit is visible in the document
even though this heuristic still reports `sourceChanged: false`. `vmake build`
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

Targets are sorted by name. Argument lists such as `files`, `deps`, and the
flag arrays keep the order the buildscript declared.

| Field | Type | Meaning |
|-------|------|---------|
| `name`, `kind` | string | Target name and kind (`binary`, `static`, `shared`, `object`, `void`) |
| `default`, `test` | bool | Default build membership and test-target flag |
| `files`, `excludedFiles`, `includes`, `publicIncludes`, `defines` | array? | Declared inputs and compile settings; `files`/`excludedFiles` keep the declared glob patterns |
| `cFlags`, `cxxFlags`, `ldFlags` | array? | Declared target compiler and linker flags from `AddCFlags`/`AddCxxFlags`/`AddLdFlags`, raw strings in declaration order; omitted when the target declares none |
| `resolvedFiles` | array? | Declaration-time expansion of `files` minus `excludedFiles`, package-source-dir-relative slash paths, sorted, de-duplicated, capped at 200; omitted when no file matches |
| `resolvedFileCount` | int? | Full number of matched files before the 200-entry cap; omitted when no file matches |
| `resolvedFilesTruncated` | bool? | True when more than 200 files matched and `resolvedFiles` was capped; omitted otherwise |
| `languages`, `links` | array? | Declared language overrides and link libraries |
| `deps` | array? | Declared target dependencies (`pkg:target` or `:target`) |
| `postLinkOutputs` | array? | Declared post-link output patterns |
| `output` | string? | Artifact name from `api.TargetFilename(kind, name, target_os)`; empty and omitted for `void` |
| `installDir` | string? | Per-target install subdirectory override |
| `noInstall` | bool? | True when the target opts out of installation |
| `installs` | array? | Package-level `AddInstalls` entries `{source, dest}` declared by the dry-run `OnBuild`; omitted when none |
| `installPath` | string? | Predicted prefix-relative, slash-separated install path (`bin/app`, `lib/libfoo.a`); omitted for `void`, `object`, `noInstall`, and test targets |

`cFlags`, `cxxFlags` and `ldFlags` are the target's own declared values: each
list is copied verbatim and in declaration order, so flag order is preserved and
the entries are not sorted, de-duplicated, shell-split, or variable-expanded.
They exclude package and global flags (`AddGlobalCFlags`, `OnApply` flags,
toolchain default flags) and flags inherited from dependencies; they are a
declaration, not the compile or link command line. They are also not action
signatures: inspect does not read or hash action records. An `AddCFlags` (or
`AddCxxFlags`/`AddLdFlags`) edit therefore changes this document, making the
change visible in a JSON diff, but `buildDir`, `freshness`, and the report match
bits still do not prove the next build is a no-op. Run `vmake build` when
correctness matters. The flag fields are additive to schemaVersion 1 and are
omitted, like the other optional array fields, when empty.

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

`resolvedFiles` uses the same expansion as the build: every pattern in `files`
is globbed against the package `sourceDir`, each match is dropped when
`excludedFiles` matches it, and the remaining existing regular files are made
relative to the package source directory, slash-separated, de-duplicated, and
sorted. The list is declaration-time only: it performs glob expansion and
`stat`, never reads file contents and never writes. It is capped at 200 entries
to bound output size; `resolvedFileCount` always reports the full before-cap
count and `resolvedFilesTruncated` is true when the cap applied. All three
fields are omitted when the target declares no `files` or nothing matches (an
empty directory or a pattern that matches nothing). A declared generated file,
for example one under the active `buildDir`, matches only after a build has
created it, so it can appear in a later inspect run; declaration order is not
preserved because the list is sorted. Unlike the freshness heuristic,
`resolvedFiles` does not skip paths under `buildDir`: a generated file that a
target actually compiles is a resolved source once it exists.

`resolvedFiles` is additive to schemaVersion 1: `files` still reports the
declared patterns unchanged, and consumers that compare inspect documents across
versions should ignore unknown data keys rather than reject the document.
Targets are listed only for needed packages; a package pruned by `FilterDeps`
(for example a conditional or test-only package that is disabled in the current
configuration) has no `targets`, even though it remains in `packages` and the
graph. A needed package whose source tree is missing also has no `targets`:
inspect does not run its dry-run `OnBuild` declaration, and the only warning is
`VMAKE_W_SOURCE_MISSING`, so no resolved file fields appear. Inspect does not
build or install, so `output`, `installDir`, and `installPath` are predictions,
not evidence that an artifact exists.

## Determinism

Output is deterministic for the same project, configuration, overrides, build
report, and source trees: packages and roots are sorted by name, graph edges by
`(from, to)`, freshness sources are de-duplicated and sorted, target
`resolvedFiles` are de-duplicated and sorted, target
`cFlags`/`cxxFlags`/`ldFlags` keep the buildscript's declaration order,
target-declaration diagnostics keep package-name order with the
`VMAKE_W_READ_ONLY_TARGETS` aggregate last, and JSON object keys follow Go's
`encoding/json` ordering. Consecutive runs are byte-identical. Build- and
test-state fields are snapshots of the report and compile database files, so
they stay byte-identical until one of those files changes. The freshness
heuristic changes when the build report or a declared source's modification time
changes; resolved file lists also change when matching files are added, removed,
or renamed.

## Exit status and stderr

| Situation | stdout | exit | stderr |
|-----------|--------|------|--------|
| Model produced | `status: "ok"` envelope | 0 | empty |
| Model produced with warnings | `status: "ok"` envelope with warning diagnostics | 0 | one `Warning: ...` line per warning |
| `Require`/`Configure`/inspection failure | `status: "error"`, no `data` | 1 | one `Error: ...` line |
| Invalid `--set` | `status: "error"`, `code: VMAKE_E_OPTION` | 1 | one `Error: ...` line |

Warnings cover target-declaration failures (`VMAKE_W_TARGET_DECLARE`, including
a dry-run write rejected by the read-only guard), the read-only omission
aggregate (`VMAKE_W_READ_ONLY_TARGETS`), missing source trees
(`VMAKE_W_SOURCE_MISSING`), and unreadable build or test reports
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
state, and expands and stats declared source files for the freshness heuristic
and `resolvedFiles`. It never writes, updates, or deletes the compile database,
reports, or sources through vmake's own file operations, and it does not inspect
action records or outputs. During resolution and dry-run declaration it also
rejects interpreted-script writes through the wrapped `os` APIs described in
[Read-only dry-run](#read-only-dry-run); a rejected call does not create,
truncate, rename, or delete anything. Unwrapped stdlib APIs and subprocesses are
not sandboxed, so a buildscript that uses them remains responsible for guarding
its own writes on `p.DryRun()`; during inspection `BuildDir()` is the resolved
active build-key directory even before it exists, so an empty `BuildDir()` is
not a sufficient guard. It also does not modify
the active configuration. With `--set`, resolution and
`OnConfig`/`OnApply` run twice as described above. The lock/state files it may
create under `.vmake/` are the same ones every read-only command creates.
