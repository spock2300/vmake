# vmake inspect

`vmake inspect` prints the resolved project model as a single JSON document on
stdout. It is the machine-readable counterpart of `vmake query`: it performs
resolution and `OnConfig`/`OnBuild` declaration only, so it never compiles,
links, installs, or writes `build/`, `install/`, or `.vmake/config.json` itself.
With `--no-targets`, dry-run `OnBuild` target declaration is skipped entirely,
so the package, option, graph, source-presence and directory model is available
without executing build callbacks.
Dry-run `OnBuild` callbacks run under a read-only interpreted-script file API
(see [Read-only dry-run](#read-only-dry-run)), so wrapped file writes fail
instead of touching the project; APIs outside the wrapped set remain a script's
responsibility. It also reports needed packages whose source tree is not on disk
yet, without fetching or materializing them, and the install plan declared by
`OnBuild` (extra `AddInstalls` entries and predicted prefix-relative target
paths) without reading or creating `install/`. The model is a declaration-time
view, not a build plan: the target `files` field keeps the declared patterns,
and `cFlags`/`cxxFlags`/`ldFlags` keep the declared flag strings without merged
globals or action signatures (see [`targets` entries](#targets-entries)).
Project-wide flags declared by needed packages are reported once in the
top-level `globalCFlags`/`globalCxxFlags`/`globalLdFlags`/`globalLinks` arrays,
with auto-exported global-option macros in `globalDefines` (see
[Global flags](#global-flags)).

The model also reports whether `build/compile_commands.json` and the last
`build/vmake-build.json` and `build/vmake-test.json` currently exist, along with
each needed package's active build-key directory. Inspect still does not verify
that actions or outputs are fresh, does not read action records, and does not
predict which targets are up to date. It never writes the compile database or
either report.

```
vmake inspect [--no-targets] [--set [package/]option=value ...] [-C <dir>]
```

`-C`/`--project` is the global project-root override: it resolves against the
process cwd, must be an existing directory with `.vmake/`, `build.go`, or an
immediate child containing `build.go`, and is used as the project root without
changing the process cwd. An invalid selection is a usage error (exit 2).

`--set` may be repeated. Each override applies to this run only: the in-memory
configuration is evaluated as if the project had those values, but nothing is
persisted. The effective overrides are echoed in `data.overrides`, using the
option's natural JSON type.

Values are coerced with the option's declared type during the single resolution
pass once `OnConfig` declarations are collected, matching `vmake config --set`:

- `OptionBool`: `true`/`false`, `on`/`off`, and `1`/`0` (case-insensitive).
- `OptionInt`: numeric text whose value is integral; `42`, `42.0`, and `1e3`
  are accepted, `quick` and `1.5` are rejected.
- `OptionString`: the raw text, so `42`, `true`, and `on` stay strings.
- `OptionChoice`: the raw text, validated against the declared choice set.

Invalid overrides:

- unknown package or unknown option,
- a package-prefixed global option,
- a value whose type does not match the declared option type,
- an `OptionChoice` value outside the declared choice set,

produce a `VMAKE_E_OPTION` diagnostic and exit status 1.

## `--no-targets`

`--no-targets` skips dry-run `OnBuild` target declaration for every package.
`data.packages[].targets` is omitted, and no `VMAKE_W_TARGET_DECLARE`
diagnostics are produced. Source-presence warnings, package metadata, options,
the dependency graph, build directories and report presence are still reported.
This mode does not execute `OnBuild` at all and is intended for machine consumers
that only need the package/dependency model.

## Output isolation

Buildscript `fmt.Println`/`os.Stdout` writes cannot corrupt the JSON document.
For the whole resolution and target-declaration phase (`Require`, `Configure`,
inspection, and dry-run `OnBuild`), `inspect` points `os.Stdout` at the null
device and restores the real stdout before writing the envelope or warnings. The
same restoration applies to error paths, so a failing run still emits exactly
one JSON document followed by the one-line `Error:` on stderr. Buildscript
stdout is discarded, not forwarded. Stderr is temporarily suppressed while
`Require`/`Configure` run (so interpreter panic traces cannot corrupt the human
error line) and is then restored for the documented `Warning:`/`Error:` lines;
it is not a general buildscript log for `inspect`.

## Read-only dry-run

Inspect enables a process-wide read-only mode for interpreted-script file IO
before it starts resolution and restores it with a deferred reset on every
return path. While the mode is enabled, these wrapped `os` symbols fail without
touching the filesystem, returning

```
read-only inspection: interpreted script write to <resolved absolute path> is not allowed
```

- `WriteFile`, `Create`, `CreateTemp`, `MkdirTemp`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Symlink`;
- `Rename`, whose error names both the old and the new path;
- `OpenFile` when the flags contain any of `O_WRONLY`, `O_RDWR`, `O_CREATE`,
  `O_TRUNC`, `O_APPEND`. `O_RDONLY` opens still work.

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
succeed. Prefer declarative `WriteBuildFile`, `GenerateFile`, `Generate`, and
`BuildPath` in `OnBuild` (they defer file creation to scheduler execution), or
guard procedural writes on `p.DryRun()`. Any package whose target declaration
fails is omitted and warned, whatever the cause. This guard covers the wrapped
symbols only: a script that reaches unwrapped stdlib APIs or a subprocess is not
sandboxed. Normal `build`/`rebuild` never enables the mode, so scripts that
generate files in a real build are unaffected.

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
packages. `vmake query targets` lists every needed package
with a materialized source tree, including local and remote packages; the
dependency tree labels those same packages by their primary target.

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
| `globalOptionDefs` | array? | Declared global options `{name, type, default?, description?, values?, effective?}`, sorted by name; omitted when none |
| `globalCFlags`, `globalCxxFlags`, `globalLdFlags`, `globalLinks` | array? | Global `AddGlobal*` declarations from needed packages in package resolution then declaration order; duplicates preserved (see [Global flags](#global-flags)) |
| `globalDefines` | array? | Auto-exported global-option macros as `NAME=value`, sorted, without the `-D` prefix (see [Global flags](#global-flags)) |
| `overrides` | object | Coerced `--set` values keyed `[package/]option`; omitted when no override was given |
| `roots` | array | Needed local packages that no resolved package depends on, sorted |
| `packages` | array | One entry per resolved package, sorted by name |
| `graph` | array | Package dependency edges `{from, to}`, sorted by `from` then `to` |
| `compileDatabase` | string | `<root>/build/compile_commands.json` (the expected path, even before a build) |
| `compileDatabasePresent` | bool | Whether `<root>/build/compile_commands.json` currently exists; presence only, not freshness |
| `buildReport` | string | `<root>/build/vmake-build.json` (see `docs/BUILD_RESULT.md`) |
| `buildReportPresent` | bool | Whether `<root>/build/vmake-build.json` currently exists |
| `testReport` | string | `<root>/build/vmake-test.json` |
| `testReportPresent` | bool | Whether `<root>/build/vmake-test.json` currently exists |
| `installPrefix` | string | `<root>/install` (the default `--prefix`, even before an install) |
| `installManifest` | string | `<root>/install/manifest.json` (the expected default manifest path, even before an install) |

## Global flags

`globalCFlags`, `globalCxxFlags`, `globalLdFlags`, `globalLinks` and
`globalDefines` are additive schemaVersion 1 arrays: they expose the
project-wide settings the build merges in, which target `cFlags`/`cxxFlags`/
`ldFlags` deliberately do not repeat. All five are omitted when empty.

The four flag arrays concatenate every `AddGlobalCFlags`,
`AddGlobalCxxFlags`, `AddGlobalLdFlags` and `AddGlobalLinks` declaration from
the needed packages. Packages appear in resolution order, and within one
package all `OnConfig` declarations come before its `OnApply` declarations.
Values are copied verbatim: not sorted, shell-split, variable-expanded or
de-duplicated, so a flag declared twice appears twice. Packages pruned by
`FilterDeps` contribute nothing, matching the build. The arrays exclude mode
flags, toolchain or extension global flags, builtin default build flags, and
per-target flags. They are declarations merged across packages, not the final
compile or link command line, and they are not action signatures.

`globalDefines` lists the macros auto-exported from global options
(`CONFIG_<NAME>...`, or a `SetMacroName` replacement) as `NAME=value` entries
without the `-D` prefix. Unlike the flag arrays it is sorted and de-duplicated
by macro name, and it is project-wide: every global option declaration
contributes regardless of which package declared it. The build appends each
entry as `-D<entry>` to both the C and C++ project flags after the
package-declared global flags. A `false` boolean emits no define.

`compileDatabasePresent`, `buildReportPresent` and `testReportPresent` are
stat-only checks. A clean tree reports all three `false`; a successful build
normally leaves the database and build report behind; a subsequent `vmake test`
adds the test report without changing the build report. `clean` removes both
reports and the database again. Inspect never creates, updates, or repairs any
of these files.

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
| `optionDefs` | array? | Declared package options `{name, type, default?, description?, values?, effective?}`, sorted by name; omitted when none |
| `deps` | array | Package dependency names, sorted; always present |
| `targets` | array? | Declared targets; present only for needed packages and omitted when the package declares none, its source tree is missing, or inspect runs with `--no-targets` |

## `targets` entries

Targets are sorted by name. Argument lists such as `files`, `deps`, and the
flag arrays keep the order the buildscript declared.

| Field | Type | Meaning |
|-------|------|---------|
| `name`, `kind` | string | Target name and kind (`binary`, `static`, `shared`, `object`, `void`) |
| `default`, `test` | bool | Default build membership and test-target flag |
| `files`, `excludedFiles`, `includes`, `publicIncludes`, `defines`, `publicDefines` | array? | Declared inputs and compile settings; `files`/`excludedFiles` keep the declared glob patterns |
| `cFlags`, `cxxFlags`, `ldFlags`, `publicCFlags`, `publicCxxFlags`, `publicLdFlags` | array? | Declared target compiler and linker flags and public exported flags, raw strings in declaration order; omitted when empty |
| `links`, `publicLinks` | array? | Declared link libraries (`-l`) and public exported link libraries |
| `linkerScript` | string? | Declared linker script path (`SetLinkerScript`); omitted when empty |
| `keepGlobalSymbols` | array? | Symbols preserved by `AddKeepGlobalSymbols`; omitted when empty |
| `deps` | array? | Declared target dependencies (`pkg:target` or `:target`) |
| `inputs`, `outputs` | array? | Declared action inputs and outputs |
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
`InstallContext.SetPrefix` are not resolved.

Targets are listed only for needed packages; a package pruned by `FilterDeps`
has no `targets`, even though it remains in `packages` and the graph. A needed
package whose source tree is missing also has no `targets`: inspect does not run
its dry-run `OnBuild` declaration, and the only warning is
`VMAKE_W_SOURCE_MISSING`. With `--no-targets`, no package has `targets`, and no
target-declaration warning is produced. Inspect does not build or install, so
`output`, `installDir`, and `installPath` are predictions, not evidence that an
artifact exists.

## Determinism

Output is deterministic for the same project, configuration, overrides, and
source trees: packages and roots are sorted by name, graph edges by `(from, to)`,
target `cFlags`/`cxxFlags`/`ldFlags` keep the buildscript's declaration order,
`globalCFlags`/`globalCxxFlags`/`globalLdFlags`/`globalLinks` keep package and
declaration order (duplicates preserved), `globalDefines` is sorted,
target-declaration diagnostics keep package-name order, and JSON object keys
follow Go's `encoding/json` ordering. Consecutive runs are byte-identical.

## Exit status and stderr

| Situation | stdout | exit | stderr |
|-----------|--------|------|--------|
| Model produced | `status: "ok"` envelope | 0 | empty |
| Model produced with warnings | `status: "ok"` envelope with warning diagnostics | 0 | one `Warning: ...` line per warning |
| `Require`/`Configure`/inspection failure | `status: "error"`, no `data` | 1 | one `Error: ...` line |
| Invalid `--set` | `status: "error"`, `code: VMAKE_E_OPTION` | 1 | one `Error: ...` line |

Warnings cover target-declaration failures (`VMAKE_W_TARGET_DECLARE`, including
a dry-run write rejected by the read-only guard) and missing source trees
(`VMAKE_W_SOURCE_MISSING`). Buildscript stdout is always discarded, including on
failure; stdout contains one JSON document in every row.

## Side effects

`inspect` acquires the project lock and performs dependency resolution like
`query`. The missing-source check only reads directories: it does not run git,
touch the network, create `<package>/src` links, or write `.vmake_deps`.
Resolution of native remote packages still follows its normal rules and may
materialize that package's tree. `inspect` does not read or create `install/`
and does not resolve the install prefix: `installPrefix` and `installManifest`
are the expected default paths, while `installs` and `installPath` come from
dry-run `OnBuild` declarations. Inspect stats `build/compile_commands.json`,
`build/vmake-build.json`, and `build/vmake-test.json` to report their presence.
It never writes, updates, or deletes the compile database, reports, or sources
through vmake's own file operations, and it does not inspect action records or
outputs. During resolution and dry-run declaration it also rejects
interpreted-script writes through the wrapped `os` APIs described in
[Read-only dry-run](#read-only-dry-run); a rejected call does not create,
truncate, rename, or delete anything. It also does not modify the active
configuration. The lock/state files it may create under `.vmake/` are the same
ones every read-only command creates.
