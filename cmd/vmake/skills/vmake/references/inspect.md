# vmake inspect

`vmake inspect` prints the resolved project model as one JSON document
(schemaVersion 1) on stdout. It runs dependency resolution, `OnConfig`, and
dry-run `OnBuild` target declaration only: it never compiles, links, installs,
or writes `build/`, `install/`, or `.vmake/config.json` itself. During dry-run
declaration it also disables interpreted-script file writes through the wrapped
`os` APIs (see **Read-only dry-run** below), so a callback that tries to write
gets a read-only error instead of creating or deleting a file. The model is a
declaration-time view, not a build plan: the target `files` field keeps the
declared patterns, while `resolvedFiles` adds a bounded resolution of them (see
the `targets` entries below), and `cFlags`/`cxxFlags`/`ldFlags` keep the declared
flag strings without merged globals or action signatures. Inspect also warns
about needed packages whose source tree
is not on disk, without fetching or materializing it, and reports the install
plan declared by `OnBuild` without reading or creating `install/`. State fields
report whether the compile database, build report and test report currently
exist, each report's identity and configuration match (`configMatches`, plus
override-aware `effectiveConfigMatches`), and each needed package's active build
directory; they do not verify action or output freshness. A `freshness` object
adds a heuristic that compares the build report's finish time with the declared
source files of needed packages. The build and test reports are separate
invocations and are never merged.

```
vmake inspect [--set [package/]option=value ...] [-C <dir>]
```

`-C`/`--project` is the global project-root override. It is resolved against
the process cwd, must be an existing project directory (`.vmake/`, `build.go`,
or an immediate child with `build.go`), and does not change the process cwd. An
invalid selection exits 2.

Buildscript stdout is discarded while the model is resolved, so stdout always
contains exactly one JSON document even when `build.go` prints. On success
stderr is empty except for one `Warning: ...` line per warning diagnostic; on
failure it carries one `Error: ...` line.

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

Failure (no `data`, exit 1):

```json
{
  "schemaVersion": 1,
  "command": "inspect",
  "status": "error",
  "diagnostics": [
    {
      "severity": "error",
      "code": "VMAKE_E_OPTION",
      "message": "proj/flag: invalid bool value: \"maybe\" (use true/false, on/off, or 1/0)"
    }
  ]
}
```

`diagnostics` is always an array. Error codes:

- `VMAKE_E_INSPECT` — the model could not be produced (load, `Require`,
  `Configure`, or inspection failure).
- `VMAKE_E_OPTION` — invalid `--set` key, unknown package/option, wrong value
  type, or an invalid choice.
- `VMAKE_W_TARGET_DECLARE` — warning: one package's `OnBuild` failed while
  targets were declared. Its `targets` are omitted, `status` stays `"ok"`, exit
  is 0, and a `fix` field is present. A message containing
  `read-only inspection: interpreted script write to ... is not allowed` means
  the dry-run callback attempted a file write (see **Read-only dry-run**).
  Declaration is skipped when the package's source tree is missing, so that
  state reports only `VMAKE_W_SOURCE_MISSING` instead of a misleading
  declaration failure.
- `VMAKE_W_SOURCE_MISSING` — warning: a needed package's expected source tree
  is missing or empty. `message` names the package and the URL/version to fetch
  when known; `package` carries the package name. `status` stays `"ok"` and exit
  is 0. Inspect checks the tree without fetching, linking, or writing
  `.vmake_deps`: local `SetGit` packages use `<package>/src`, remote packages
  `.vmake_deps/<repo>/<owner>/src[/<member>]`, and plain local packages their
  package directory. `vmake query` and `vmake query targets` print the same
  `Warning: ...` lines to stderr for missing needed packages while their tree
  or target list stays unchanged on stdout and they exit 0. Query also leaves
  such packages untargeted without running their dry-run `OnBuild`; a
  target-declaration failure in a package whose source is present prints one
  `Warning: cannot declare targets for <package>: <error>` and does not stop the
  command.
- `VMAKE_W_REPORT_UNREADABLE` — warning: `build/vmake-build.json` or
  `build/vmake-test.json` exists but cannot be read or parsed. The matching
  `buildReportPresent`/`testReportPresent` stays `true`, that state object is
  omitted, `status` stays `"ok"`, exit is 0, and `fix` tells the user to rerun
  the command that writes the document. Inspect never rewrites either file.

## Read-only dry-run

From before resolution until the command returns, inspect enables a
process-wide read-only mode for interpreted-script file IO (`--allow-dry-run-writes` disables the guard for callbacks that must materialize inputs; it may modify the build tree) and restores it on
every return path. The wrapped `os` symbols `WriteFile`, `Create`, `CreateTemp`,
`Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, and `Rename` fail without touching
the filesystem, as does `OpenFile` when its flags contain `O_WRONLY`, `O_RDWR`,
`O_CREATE`, `O_TRUNC`, or `O_APPEND`. The error is:

```
read-only inspection: interpreted script write to <resolved absolute path> is not allowed
```

Reads, stats, directory listings, and `Walk`/`WalkDir` keep working, and normal
`build`/`rebuild` never enables the mode. A rejected write does not truncate,
rename, or delete an existing file, so inspecting after a build leaves generated
files in place. Any dry-run `OnBuild` write attempt fails that package's target
declaration: its targets are omitted and it gets a `VMAKE_W_TARGET_DECLARE`
warning carrying the read-only message. When one or more packages are omitted this way, inspect appends one deterministic `VMAKE_W_READ_ONLY_TARGETS` aggregate naming the count and fix. This also applies when the callback
ignores the returned error (`_ = os.WriteFile(...)`); inspect records the attempt
and treats it as a declaration failure. The guard covers the wrapped symbols
only: unwrapped stdlib APIs such as `os.MkdirTemp`, `ioutil.*`, or subprocesses
are not sandboxed. Writes attempted during `Require`/`Configure` are rejected
too but are not attached to a target-declaration warning.

## `data` fields

| Field | Meaning |
|-------|---------|
| `root` | Absolute project root (the directory containing `.vmake/`) |
| `configFile`, `configSha256` | Active config file name and SHA-256; hash of empty content when absent |
| `mode`, `toolchain` | Effective build mode and project toolchain name |
| `toolchainInfo` | `{name, targetOs?, targetTriple?, installPath?, cc?, cxx?, ar?, ld?}` |
| `global` | Effective global option values, including `mode` and `toolchain` |
| `overrides` | Typed `--set` values keyed `[package/]option`; omitted when unused |
| `roots` | Needed local packages that no resolved package depends on, sorted |
| `packages` | Resolved package entries, sorted by name |
| `graph` | Package dependency edges `{from, to}`, sorted |
| `compileDatabase` | `<root>/build/compile_commands.json` (expected path, even before a build) |
| `compileDatabasePresent` | Whether that file currently exists (presence only) |
| `buildReport` | `<root>/build/vmake-build.json` (expected path, even before a build) |
| `buildReportPresent` | Whether that file currently exists |
| `buildReportState` | Report identity/result when the file exists and parses; omitted when absent or unreadable |
| `freshness` | Source-freshness heuristic from the build report finish time and declared target files; additive to schemaVersion 1 |
| `testReport` | `<root>/build/vmake-test.json` |
| `testReportPresent` | Whether that file currently exists |
| `testReportState` | Test report identity/result with the same shape as `buildReportState`; omitted when absent or unreadable |
| `installPrefix` | `<root>/install` (default `--prefix`, even before an install) |
| `installManifest` | `<root>/install/manifest.json` (expected default manifest path, even before an install) |

`buildReportState` and `testReportState` copy `status`, `buildId`, `command`,
`started`, `finished`, `mode`, `toolchain`, `summary` and `error` from their
report when present, plus `modeMatches` and `toolchainMatches`.
`configMatches` is true only when that report's `configSha256` equals
`data.configSha256` and no `--set` override was supplied, so a normal
`inspect --set` reports `false` for both. `effectiveConfigMatches` is the
override-aware counterpart: it is true when the digest matches, the recorded
mode/toolchain match the inspected ones, and every `--set` override equals the
value already in effect (the config value or the declared default, compared
after option-type normalization), and false when an override changes an
effective value, the digest differs, or the report was produced with another
mode/toolchain; a missing effective value is not a no-op. Both match fields are
additive to schemaVersion 1: `configMatches` keeps its strict file-level
meaning, and consumers comparing inspect documents across versions should ignore
unknown data keys. These fields
are snapshots of the reported build and test runs, not evidence that actions or
outputs are current: inspect does not read action records or predict up-to-date
targets. A project can therefore show `buildReportPresent: true` with a
succeeded build state and `testReportPresent: true` with a failed or no-tests
test state at the same time.

### `freshness`

`freshness` is an additive schemaVersion 1 field: a heuristic source-staleness
signal, not an up-to-date check or build plan. Consumers that compare inspect
documents across versions should ignore unknown data keys rather than reject
the document.

```json
"freshness": {
  "known": true,
  "reportStatus": "succeeded",
  "reportFinished": "2026-10-19T02:00:00.123456789Z",
  "configMatches": true,
  "effectiveConfigMatches": true,
  "sourceChanged": true,
  "changedSources": ["firmware:src/main.c"],
  "truncated": false
}
```

- `known` is false without a build report, when the report is unreadable, or
  when its `finished` value is empty or not RFC 3339. A parsable report still
  reports `reportStatus`, `configMatches` and `effectiveConfigMatches`;
  `reportFinished` appears only for a parseable timestamp. `sourceChanged`,
  `changedSources`, and `truncated` are omitted while `known` is false.
- The heuristic expands the declared `files` of targets in needed packages with
  the build's glob and exclusion rules (the same expansion used for
  `resolvedFiles`), then skips paths under the active `buildDir`, stats each
  remaining existing match, and compares its modification time with
  `reportFinished` (contents are not read or hashed). Missing declared files are
  skipped; absent source trees are covered by `VMAKE_W_SOURCE_MISSING` instead.
- `changedSources` holds de-duplicated, sorted `pkg:path` entries (path relative
  to the package source directory, matching `summary.changedFiles` style) for
  files strictly newer than the finish time, capped at 50; it is omitted when
  empty. `truncated` is true when more changed; `sourceChanged` is true even
  when the extra files are not listed.
- `reportStatus` and `reportFinished` copy the build report's status and finish
  time; a failed report with a parseable finish is still `known: true`.
- `configMatches` mirrors `buildReportState.configMatches`, so it is false
  after a config change or any `--set`. The mismatch alone requires a rebuild;
  `sourceChanged` is still computed and reported.
- `effectiveConfigMatches` mirrors `buildReportState.effectiveConfigMatches`:
  true when the digest matches and every `--set` restates the value already in
  effect, so probing a current value keeps this bit true while `configMatches`
  stays false. It is additive to schemaVersion 1.
- Limitations: toolchain or buildscript/action changes, generated inputs,
  undeclared headers/files, deleted files, backdated mtimes, and clock skew are
  invisible to this heuristic. Declared target flags are reported separately in
  `cFlags`/`cxxFlags`/`ldFlags`, so an `AddCFlags` edit is visible in the
  document while `sourceChanged` can still be false. Always run `vmake build`
  when correctness matters; a false `sourceChanged` does not prove a no-op
  build.

Each `packages` entry has `name`, `kind` (`local`/`native`/`registry`),
`needed`, `sourcePresent`, `deps`, optional remote
`repo`/`version`/`commit`/`url`, local `path` and absolute `sourceDir`,
`description`, `toolchain`, `options` (effective values), and `targets` (needed
packages that declare targets). `sourcePresent` is false when the expected
source tree is missing or empty; that package also gets a
`VMAKE_W_SOURCE_MISSING` warning when it is needed.

Needed packages also carry `buildDir`, the absolute build-key directory from
the same layout the next build would use; it may not exist yet (before a first
build or after `clean`). Local packages also carry `buildKey` when the key is
the last path element; remote packages omit it because their `buildDir` ends in
`build`.

Each `targets` entry has `name`, `kind` (`binary`/`static`/`shared`/`object`/
`void`), `default`, `test`, declared `files`/`excludedFiles`/`includes`/
`publicIncludes`/`defines`/`cFlags`/`cxxFlags`/`ldFlags`/`languages`/`links`/
`deps`/`postLinkOutputs`, `resolvedFiles`/`resolvedFileCount`/
`resolvedFilesTruncated`, `output` (artifact name; empty for `void`),
`installDir`, `noInstall`, `installs`, and `installPath`.

`cFlags`, `cxxFlags` and `ldFlags` are the target's own `AddCFlags`/
`AddCxxFlags`/`AddLdFlags` values, copied verbatim in declaration order (not
sorted, de-duplicated, shell-split, or variable-expanded) and omitted when
empty. They exclude package and global flags (`AddGlobalCFlags`, `OnApply`
flags, toolchain defaults), flags inherited from dependencies, and action
signatures: they are the target's declaration, not the compiler or linker
command line. An `AddCFlags` edit therefore now changes the inspect document,
so a JSON diff shows the changed declaration even though `freshness` is
mtime-only and still cannot plan the rebuild; run `vmake build` when
correctness matters.

`resolvedFiles` is a declaration-time, bounded expansion of the declared
`files`: each pattern is globbed against the package source directory, matches
selected by `excludedFiles` are dropped, and the remaining existing regular
files are listed as sorted, de-duplicated, slash-separated paths relative to the
package source directory. Inspect only globs and stats these files; it does not
read contents or write anything. The list is capped at 200 entries.
`resolvedFileCount` reports the full count before the cap, and
`resolvedFilesTruncated` (present only when true) flags that the cap applied.
All three fields are omitted when the target declares no `files` or nothing
matches, and when the package source tree is missing (no targets are declared at
all). A declared generated file (for example one under the active `buildDir`)
matches only after a build has created it, so it can appear in a later inspect
run. The list is additive to schemaVersion 1: `files` keeps the declared
patterns.

`installs` lists the package-level `AddInstalls` entries `{source, dest}`
declared by the dry-run `OnBuild`; every target of that package carries the
same entries, because the API does not associate extra install items with a
target. `source` is as declared (usually relative to `SourceDir()`) and `dest`
is relative to the install prefix. `OnInstall` does not run during inspection,
so entries declared there are absent.

`installPath` is the predicted prefix-relative slash-separated artifact path
using `installDir` or the kind default (`bin`/`lib`), for example `bin/app` or
`lib/libfoo.a`; it is omitted for `void`, `object`, `noInstall`, and test
targets. It assumes the default install prefix: `--prefix` and per-package
`SetPrefix` are not resolved. Post-link outputs are installed beside the
artifact and are not repeated. Static libraries are installed only with
`--install-type sdk`. These install fields are additive to schemaVersion 1;
existing fields are unchanged.

Targets are listed only for needed packages; a package pruned by `FilterDeps`
has none. A needed package whose source tree is missing also has no targets,
because inspect skips its dry-run `OnBuild` declaration and warns only with
`VMAKE_W_SOURCE_MISSING`, so no resolved file fields appear. Inspect does not
build or install, so `output`, `installDir`, and `installPath` are predictions,
not evidence that an artifact exists.

## `--set` overrides

`--set` is repeatable and evaluates a hypothetical configuration for this run
only; nothing is persisted. Unknown packages/options, wrong types, and invalid
choices exit 1 with `VMAKE_E_OPTION`.

A `--set` that restates the value already in effect is a no-op for
`effectiveConfigMatches` (on both report states and `freshness`), while
`configMatches` remains false for any `--set`; an override that changes an
effective value makes both false. Values that are missing from the file and
have no declared default are not treated as no-ops.

The value is coerced with the option's declared type, matching
`vmake config --set`:

- bool: `true`/`false`, `on`/`off`, `1`/`0` (case-insensitive)
- int: numeric text with an integral value (`42`, `42.0`, `1e3`); `quick` and
  `1.5` are rejected
- string: raw text, so `42`, `true`, and `on` remain strings
- choice: raw text validated against the declared choices

Typed coercion needs the declarations, so `--set` resolves twice: once without
overrides to learn the option types, then a fresh context applies the typed
values before `Configure`. This roughly doubles resolution time and repeats
`OnConfig`/`OnApply` side effects; plain `inspect` stays single-pass. If an
option is only declared after an override changes dependency selection, its raw
text is stored and post-`Configure` validation reports any type mismatch.

Examples:

```sh
vmake inspect
vmake inspect --set mode=release --set proj/retries=5
vmake inspect --set proj/label=42          # string option keeps "42"
vmake inspect --set proj/with_dep=on       # bool alias becomes true
vmake inspect --set dep/dep_mode=slow      # choice validated
```

## Exit status and determinism

Exit 0 means a model was produced (including models with warning diagnostics);
exit 1 means it was not. Output is deterministic for the same project,
configuration, overrides, build report, and source trees: consecutive runs are
byte-identical, target `resolvedFiles` are de-duplicated and sorted, and
`cFlags`/`cxxFlags`/`ldFlags` keep the buildscript's declaration order.
Inspect stats the compile database and declared sources, expands and stats
declared file patterns, and reads both reports, but never writes any of those
files through vmake's own operations. During resolution and dry-run declaration
it also rejects interpreted-script writes through the wrapped `os` APIs;
unwrapped stdlib APIs and subprocesses are outside that guard.

This reference is self-contained. The full repository documentation set
(including `docs/INSPECT.md`) is not installed with the skill.
