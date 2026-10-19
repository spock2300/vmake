# vmake inspect

`vmake inspect` prints the resolved project model as one JSON document
(schemaVersion 1) on stdout. It runs dependency resolution, `OnConfig`, and
dry-run `OnBuild` target declaration only: it never compiles, links, installs,
or writes `build/`, `install/`, or `.vmake/config.json` itself. During dry-run
declaration it also disables interpreted-script file writes through the wrapped
`os` APIs (see **Read-only dry-run** below), so a callback that tries to write
gets a read-only error instead of creating or deleting a file. The model is a
declaration-time view, not a build plan: the target `files` field keeps the
declared patterns, and `cFlags`/`cxxFlags`/`ldFlags` keep the declared flag
strings without merged globals or action signatures. Project-wide
`AddGlobalCFlags`/`AddGlobalCxxFlags`/`AddGlobalLdFlags`/`AddGlobalLinks`
declarations from needed packages and auto-exported global-option macros are
reported once at the top level in `globalCFlags`/`globalCxxFlags`/
`globalLdFlags`/`globalLinks`/`globalDefines`. Inspect also warns about needed
packages whose source tree is not on disk, without fetching or materializing it,
and reports the install plan declared by `OnBuild` without reading or creating
`install/`. Presence fields report whether the compile database, build report
and test report currently exist, and each needed package carries its active
build directory; they do not verify action or output freshness.

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

## Read-only dry-run

From before resolution until the command returns, inspect enables a
process-wide read-only mode for interpreted-script file IO and restores it on
every return path. The wrapped `os` symbols `WriteFile`, `Create`, `CreateTemp`,
`Mkdir`, `MkdirAll`, `MkdirTemp`, `Remove`, `RemoveAll`, `Rename`, and `Symlink`
fail without touching the filesystem, as does `OpenFile` when its flags contain
`O_WRONLY`, `O_RDWR`, `O_CREATE`, `O_TRUNC`, or `O_APPEND`. The error is:

```
read-only inspection: interpreted script write to <resolved absolute path> is not allowed
```

Reads, stats, directory listings, and `Walk`/`WalkDir` keep working, and normal
`build`/`rebuild` never enables the mode. A rejected write does not truncate,
rename, or delete an existing file, so inspecting after a build leaves generated
files in place. Any dry-run `OnBuild` write attempt fails that package's target
declaration: its targets are omitted and it gets a `VMAKE_W_TARGET_DECLARE`
warning carrying the read-only message. This also applies when the callback
ignores the returned error (`_ = os.WriteFile(...)`); inspect records the attempt
and treats it as a declaration failure. Prefer declarative `WriteBuildFile`,
`GenerateFile`, `Generate`, and `BuildPath` in `OnBuild`, or guard procedural
writes on `p.DryRun()`. The guard covers the wrapped symbols only.

## `data` fields

| Field | Meaning |
|-------|---------|
| `root` | Absolute project root (the directory containing `.vmake/`) |
| `configFile`, `configSha256` | Active config file name and SHA-256; hash of empty content when absent |
| `mode`, `toolchain` | Effective build mode and project toolchain name |
| `toolchainInfo` | `{name, targetOs?, targetTriple?, installPath?, cc?, cxx?, ar?, ld?}` |
| `global` | Effective global option values, including `mode` and `toolchain` |
| `globalOptionDefs` | Declared global options `{name, type, default?, description?, values?, effective?}`, sorted by name; omitted when none |
| `globalCFlags`, `globalCxxFlags`, `globalLdFlags`, `globalLinks` | Global `AddGlobal*` declarations from needed packages in package/declaration order; duplicates preserved; omitted when empty |
| `globalDefines` | Auto-exported global-option macros as `NAME=value`, sorted, without `-D`; omitted when empty |
| `overrides` | Typed `--set` values keyed `[package/]option`; omitted when unused |
| `roots` | Needed local packages that no resolved package depends on, sorted |
| `packages` | Resolved package entries, sorted by name |
| `graph` | Package dependency edges `{from, to}`, sorted |
| `compileDatabase` | `<root>/build/compile_commands.json` (expected path, even before a build) |
| `compileDatabasePresent` | Whether that file currently exists (presence only) |
| `buildReport` | `<root>/build/vmake-build.json` (expected path, even before a build) |
| `buildReportPresent` | Whether that file currently exists |
| `testReport` | `<root>/build/vmake-test.json` |
| `testReportPresent` | Whether that file currently exists |
| `installPrefix` | `<root>/install` (default `--prefix`, even before an install) |
| `installManifest` | `<root>/install/manifest.json` (expected default manifest path, even before an install) |

### Global flags

`globalCFlags`, `globalCxxFlags`, `globalLdFlags`, `globalLinks` and
`globalDefines` are additive schemaVersion 1 arrays and are omitted when empty.
The four flag arrays concatenate the matching `AddGlobal*` calls from needed
packages in package resolution order; within one package, `OnConfig`
declarations precede its `OnApply` declarations. Values are copied verbatim and
in declaration order: not sorted, shell-split, variable-expanded or
de-duplicated, so a flag declared twice appears twice. Packages pruned by
`FilterDeps` contribute nothing, matching the build.

`globalDefines` lists the macros auto-exported from global options
(`CONFIG_<NAME>...` or a `SetMacroName` replacement) as `NAME=value` without the
`-D` prefix. It is sorted, de-duplicated by macro name, and project-wide: every
global option declaration contributes regardless of which package declared it.
The build appends each entry as `-D<entry>` to both the C and C++ project flags
after the package-declared globals; a `false` boolean emits no define.

Each `packages` entry has `name`, `kind` (`local`/`native`/`registry`),
`needed`, `sourcePresent`, `deps`, optional remote
`repo`/`version`/`commit`/`url`, local `path` and absolute `sourceDir`,
`description`, `toolchain`, `options` (effective values), `optionDefs`
(declared option schemas and effective values), and `targets` (needed packages
that declare targets). `sourcePresent` is false when the expected source tree is
missing or empty; that package also gets a `VMAKE_W_SOURCE_MISSING` warning when
it is needed.

Needed packages also carry `buildDir`, the absolute build-key directory from
the same layout the next build would use; it may not exist yet (before a first
build or after `clean`). Local packages also carry `buildKey` when the key is
the last path element; remote packages omit it because their `buildDir` ends in
`build`.

Each `targets` entry has `name`, `kind` (`binary`/`static`/`shared`/`object`/
`void`), `default`, `test`, declared `files`/`excludedFiles`/`includes`/
`publicIncludes`/`defines`/`publicDefines`/`cFlags`/`cxxFlags`/`ldFlags`/
`publicCFlags`/`publicCxxFlags`/`publicLdFlags`/`links`/`publicLinks`/
`linkerScript`/`keepGlobalSymbols`/`deps`/`inputs`/`outputs`/`postLinkOutputs`,
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
targets.

Targets are listed only for needed packages; a package pruned by `FilterDeps`
has none. A needed package whose source tree is missing also has no targets,
because inspect skips its dry-run `OnBuild` declaration and warns only with
`VMAKE_W_SOURCE_MISSING`. Inspect does not build or install, so `output`,
`installDir`, and `installPath` are predictions, not evidence that an artifact
exists.

## `--set` overrides

`--set` is repeatable and evaluates a hypothetical configuration for this run
only; nothing is persisted. Unknown packages/options, wrong types, and invalid
choices exit 1 with `VMAKE_E_OPTION`.

The value is coerced with the option's declared type during the single
resolution pass once `OnConfig` declarations are collected, matching
`vmake config --set`:

- bool: `true`/`false`, `on`/`off`, `1`/`0` (case-insensitive)
- int: numeric text with an integral value (`42`, `42.0`, `1e3`); `quick` and
  `1.5` are rejected
- string: raw text, so `42`, `true`, and `on` remain strings
- choice: raw text validated against the declared choices

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
configuration, overrides, and source trees: consecutive runs are byte-identical,
`globalDefines` is sorted, and `cFlags`/`cxxFlags`/`ldFlags` keep the
buildscript's declaration order while `globalCFlags`/`globalCxxFlags`/
`globalLdFlags`/`globalLinks` keep package and declaration order with duplicates
preserved. Inspect stats the compile database and report files, but never writes
any of those files through vmake's own operations. During resolution and dry-run
declaration it also rejects interpreted-script writes through the wrapped `os`
APIs; unwrapped stdlib APIs and subprocesses are outside that guard.

This reference is self-contained. The full repository documentation set
(including `docs/INSPECT.md`) is not installed with the skill.
