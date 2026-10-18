# vmake inspect

`vmake inspect` prints the resolved project model as a single JSON document on
stdout. It is the machine-readable counterpart of `vmake query`: it performs
resolution and `OnConfig`/`OnBuild` declaration only, so it never compiles,
links, installs, or writes `build/`, `install/`, or `.vmake/config.json`.

```
vmake inspect [--set [package/]option=value ...]
```

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
| `buildReport` | string | `<root>/build/vmake-build.json` (see `docs/BUILD_RESULT.md`) |

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
| `needed` | bool | Whether the package is reachable from the build roots |
| `description` | string? | `SetDescription` value |
| `toolchain` | string? | Per-package toolchain, present for needed packages |
| `options` | object? | Declared options of this package with effective values (global declarations included); omitted when none |
| `deps` | array | Package dependency names, sorted; always present |
| `targets` | array? | Declared targets; present only for needed packages and omitted when the package declares none |

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

## Determinism

Output is deterministic for the same project, configuration, and overrides:
packages and roots are sorted by name, graph edges by `(from, to)`, and JSON
object keys by Go's `encoding/json` ordering. Consecutive runs are
byte-identical.

## Exit status and stderr

| Situation | stdout | exit | stderr |
|-----------|--------|------|--------|
| Model produced | `status: "ok"` envelope | 0 | empty |
| Model produced with target warnings | `status: "ok"` envelope with warning diagnostics | 0 | one `Warning: ...` line per warning |
| `Require`/`Configure`/inspection failure | `status: "error"`, no `data` | 1 | one `Error: ...` line |
| Invalid `--set` | `status: "error"`, `code: VMAKE_E_OPTION` | 1 | one `Error: ...` line |

Buildscript stdout is always discarded, including on failure; stdout contains
one JSON document in every row.

## Side effects

`inspect` acquires the project lock and may lazily resolve sources exactly like
`query`; it does not modify `build/`, `install/`, or the active configuration.
With `--set`, resolution and `OnConfig`/`OnApply` run twice as described above.
The lock/state files it may create under `.vmake/` are the same ones every
read-only command creates.
