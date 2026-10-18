# vmake inspect

`vmake inspect` prints the resolved project model as one JSON document
(schemaVersion 1) on stdout. It runs dependency resolution, `OnConfig`, and
dry-run `OnBuild` target declaration only: it never compiles, links, installs,
or writes `build/`, `install/`, or `.vmake/config.json`. It also warns about
needed packages whose source tree is not on disk, without fetching or
materializing it, and reports the install plan declared by `OnBuild` without
reading or creating `install/`. Build-state fields report whether the compile
database and build report currently exist, the last report's identity and
configuration match, and each needed package's active build directory; they do
not verify action or output freshness.

```
vmake inspect [--set [package/]option=value ...]
```

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
  is 0, and a `fix` field is present.
- `VMAKE_W_SOURCE_MISSING` — warning: a needed package's expected source tree
  is missing or empty. `message` names the package and the URL/version to fetch
  when known; `package` carries the package name. `status` stays `"ok"` and exit
  is 0. Inspect checks the tree without fetching, linking, or writing
  `.vmake_deps`: local `SetGit` packages use `<package>/src`, remote packages
  `.vmake_deps/<repo>/<owner>/src[/<member>]`, and plain local packages their
  package directory. `vmake query` and `vmake query targets` print the same
  `Warning: ...` lines to stderr for missing needed packages while their tree
  or target list stays unchanged on stdout and they exit 0.
- `VMAKE_W_REPORT_UNREADABLE` — warning: `build/vmake-build.json` exists but
  cannot be read or parsed. `buildReportPresent` stays `true`,
  `buildReportState` is omitted, `status` stays `"ok"`, exit is 0, and `fix`
  tells the user to rerun a build. Inspect never rewrites the file.

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
| `installPrefix` | `<root>/install` (default `--prefix`, even before an install) |
| `installManifest` | `<root>/install/manifest.json` (expected default manifest path, even before an install) |

`buildReportState` copies `status`, `buildId`, `command`, `started`,
`finished`, `summary` and `error` from the report when present. Its
`configMatches` is true only when the report's `configSha256` equals
`data.configSha256` and no `--set` override was supplied, so a normal
`inspect --set` reports `false`. These fields are a snapshot of the reported
build, not evidence that actions or outputs are current: inspect does not read
action records or predict up-to-date targets.

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
`publicIncludes`/`defines`/`languages`/`links`/`deps`/`postLinkOutputs`,
`output` (artifact name; empty for `void`), `installDir`, `noInstall`,
`installs`, and `installPath`.

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
has none. `files` keeps the declared globs; inspect does not expand them into
source lists. Inspect does not build or install, so `output`, `installDir`, and
`installPath` are predictions, not evidence that an artifact exists.

## `--set` overrides

`--set` is repeatable and evaluates a hypothetical configuration for this run
only; nothing is persisted. Unknown packages/options, wrong types, and invalid
choices exit 1 with `VMAKE_E_OPTION`.

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
configuration, overrides, and build-state files: consecutive runs are
byte-identical. Inspect stats the compile database and reads the build report
but never writes either file.

This reference is self-contained. The full repository documentation set
(including `docs/INSPECT.md`) is not installed with the skill.
