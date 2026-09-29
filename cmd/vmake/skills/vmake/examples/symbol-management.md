# Symbol Management

Control which symbols a library exports to prevent conflicts, leaks, and ABI
coupling across a complex dependency graph. This is critical when multiple
shared/static libraries are linked into the same binary and their internal
helpers might collide (e.g. two libraries both defining `parser_init`).

## Why It Matters

Without symbol management:

- Library A's internal `parse_init` and library B's internal `parse_init`
  collide at final link — "multiple definition" errors or, worse, silent
  override.
- Internal helpers leak into a `.so`'s export table, becoming de-facto public
  ABI. Refactoring then breaks downstream consumers.
- Binary links absorb static archives through `--whole-archive`, contributing
  every global symbol from them into the final binary's namespace.

The fix is layered: **default hidden → declare exports → link policy → audit**.

## Prerequisites

- An ELF target for the link-policy layers and the audit; GNU-compatible
  toolchain with `nm` (and `objcopy` for Layer 5).
- A version-script file committed at a stable path (e.g. `export.map` in the
  package root) when using Layer 2.
- A build that succeeds: `vmake check-symbols` inspects built artifacts and
  reports `missing-artifact` errors for declared targets without output.

## The Five Layers

| Layer | Mechanism | Solves | API |
|-------|-----------|--------|-----|
| 1. Default hidden | `-fvisibility=hidden` + `-fvisibility-inlines-hidden` | This package's symbols default to non-exported | `ctx.SetDefaultVisibilityHidden()` |
| 2. Declare exports | version-script on Shared/Binary | Declarative public API surface | `target.SetVersionScript("foo.map")` |
| 3. Link policy | `--exclude-libs`, `-Bsymbolic` | Static archive absorption; internal binding | `target.AddExcludeLibs(...)`, `target.SetSymbolBinding("static")` |
| 4. Audit | `nm -D` scan of built local default outputs | Duplicate/mangled/reserved leaks, version-script violations | `vmake check-symbols [--strict]` |
| 5. Prefix isolation | `objcopy --prefix-symbols=` | Force namespace onto third-party C code | `target.SetSymbolPrefix("vendor_")` |

Use Layer 1 in packages whose source declares their public exports. Dependency
packages keep their own export rules, including third-party libraries that rely
on default visibility. Enable it separately in each of your packages that needs
hidden defaults.

## Layer 1: Default Hidden Visibility

Compile every source file in the declaring package with hidden default
visibility. Only symbols explicitly marked become public.

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.SetDefaultVisibilityHidden()
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("libfoo").SetKind(api.TargetShared).AddFiles("src/*.c")
    })
}
```

This adds `-fvisibility=hidden` to this package's C and C++ compiler flags, plus
`-fvisibility-inlines-hidden` to C++ only (that flag is invalid for C). The
setting applies to every target in this package and is idempotent in `OnConfig`
or an option's `OnApply`. It does not alter dependencies or force
`-fvisibility=default` on them. This behavior is the same for local, remote,
Registry, and Native packages.

Native compilation, `p.MergedCFlags()`/`p.MergedCxxFlags()`, and
`p.CMakeConfigure()` share these defaults. The defaults precede global and
explicit extra flags; explicit CMake flags replace the automatic value. Use
`MergedCFlags(extra...)` or `MergedCxxFlags(extra...)` to retain it in an
override. `p.DefaultVisibilityHidden()` and `p.VisibilityFlags()` expose the
policy, and changing it changes only this package's build key.

`AddGlobalCFlags("-fvisibility=hidden")` and
`AddGlobalCxxFlags("-fvisibility=hidden")` still apply to all packages when that
behavior is explicitly required.

Then annotate exported symbols in source:

```c
/* foo.h */
#define FOO_EXPORT __attribute__((visibility("default")))

FOO_EXPORT int foo_api(int x);
```

```c
/* foo_api.c */
#include "foo.h"
int foo_api(int x) { return helper(x) + 1; }   /* exported */

/* helper stays internal — not annotated, hidden by default */
static int helper(int x) { return x * 2; }
```

MSVC is out of scope for VMake — it targets GNU-compatible toolchains, where the
GNU visibility attribute above is the export mechanism. On PE targets only the
ELF-specific layers are rejected at link time (version scripts, `--exclude-libs`,
`-Bsymbolic`), and `vmake check-symbols` reports the artifacts as not-applicable.

## Layer 2: Version Script (Declarative Exports)

A version script declares exactly which symbols a shared library exports.
Everything else is hidden — even if the source forgets the visibility
attribute. This is the strongest guarantee and is source-agnostic.

```go
ctx.Target("libfoo").
    SetKind(api.TargetShared).
    AddFiles("src/*.c").
    SetVersionScript("export.map")
```

`export.map`:

```
V_1_0 {
    global:
        foo_api;
        foo_init;
        foo_shutdown;
    local:
        *;
};
```

`SetVersionScript` is only valid on `TargetShared` and `TargetBinary`. Any other
kind fails at build time with a scheduler error ("only valid on
TargetShared/TargetBinary") — it is not a declaration-time `fatalScript`. `cc -r`
(partial link) produces no dynamic symbol table, and static archives never link
one, so the version script would have no effect. For object-level visibility
control, use Layer 1 (compile-time visibility).

### Version Script Syntax Cheat Sheet

- `global: sym1; sym2;` — exported symbols (one per line, semicolon-terminated)
- `local: *;` — hide everything not listed in `global`
- `local: *foo_internal*;` — hide by pattern (wildcards supported)
- `extern "C" { ... }` — wrap C++ mangled names to declare them unmangled
- Multiple version nodes (`V_1_0 { ... }; V_2_0 { ... };`) for versioned ABI
- Comments: `/* block */` and `#`. GNU ld does **not** accept `//` line
  comments; VMake's audit parser tolerates `//` but does not skip `#` lines
  (they are collected as declared symbols), so `vmake check-symbols` will NOT
  catch a `//` mistake — prefer `/* */`

Audit-parser limitations: `global: *;` and `extern "C++" { ... }` are not
modeled by the version-script checker. `global: *;` yields an empty declared
set and `extern "C++"` contributes literal `extern`/`C++` tokens, so either
form makes every real export a `version-script-violation` false positive.

### Version Script Path Resolution

`SetVersionScript` paths are resolved against the package's `SourceDir()` at
build time (an absolute path is still joined under `SourceDir()`, so always pass
a SourceDir-relative path). `vmake check-symbols` resolves the same literal
against the current working directory first, then the built artifact's
directory. If the audit cannot open the file it emits a warn-level
`version-script-violation` with `could not parse <path>` under `--strict`. Keep
one consistent SourceDir-relative path (e.g. `"linker/export.map"`) and run the
audit from the project root, or from the directory where that path resolves.

## Layer 3: Link Policy

### Strip symbols from absorbed static libraries

By default a `TargetShared` that links a static `.a` through `AddDeps` passes
the archive as a plain positional input — unlike a binary link, the shared
command does **not** wrap dependency archives in `--whole-archive`. Only the
members the shared code references get included, but their global symbols still
appear in the `.so`'s dynamic export table unless hidden. Use `AddExcludeLibs`
to strip them:

```go
ctx.Target("libfoo").
    SetKind(api.TargetShared).
    AddFiles("src/*.c").
    AddDeps("helper").              /* static lib from another package */
    AddExcludeLibs("libhelper")     /* don't re-export its symbols */
```

This passes `-Wl,--exclude-libs=libhelper` to the linker. Use `ALL` to strip
every static archive's symbols.

**GNU ld quirk**: when the archive is linked by file path (not `-l`),
`--exclude-libs` matches the full archive basename minus `.a` — so a target
named `helper` (which vmake emits as `libhelper.a`) must be referenced as
`libhelper`, not `helper`. Pass the form with the `lib` prefix.

On `TargetBinary`, VMake places `.a`/`.so` dependency artifacts inside
`-Wl,--start-group` / `-Wl,--whole-archive` / `-Wl,--no-whole-archive`. The full
linker semantics and the unresolved-symbol case are covered in
`references/gotchas.md` (`Static Library Deps with Symbols Not Referenced by
Your Code`).

All three `SetVersionScript`, `AddExcludeLibs` and `SetSymbolBinding` settings are
validated as ELF-only on shared/binary links: on a `target_os=windows`
`TargetShared`/`TargetBinary` the build fails with "not supported for PE targets:
version scripts, --exclude-libs and -Bsymbolic are ELF-only". Static/object links
never build a link policy, so `AddExcludeLibs`/`SetSymbolBinding` there are
silently ignored (and `SetVersionScript` fails earlier on the kind check).

### Bind internal references statically

By default, shared library code references its own symbols through the PLT,
allowing interposition (LD_PRELOAD-style override). `-Bsymbolic` makes
internal references bind directly to the library's own definitions — faster
and prevents accidental override:

```go
ctx.Target("libfoo").
    SetKind(api.TargetShared).
    AddFiles("src/*.c").
    SetSymbolBinding("static")   /* -Bsymbolic */
```

Modes: `"static"` (`-Bsymbolic`), `"static-functions"` (`-Bsymbolic-functions`),
or empty (default).

## Layer 4: Audit

`vmake check-symbols` inspects the built default `TargetShared`/`TargetBinary`
outputs of **local** packages via `nm -D` — no per-target declaration required —
and reports:

- **Cross-target duplicate exports**: the same symbol exported by two targets
  in the build graph (collision risk at final link)
- **C++ mangled leaks**: `_Z*` symbols exported from C-facing libraries
- **Reserved-prefix leaks**: `__libc_*` and similar reserved prefixes
- **Version-script violations**: exports outside what a declared
  `SetVersionScript` allows
- **Missing version-script notices**: shared libraries without any version
  script (severity `info`, so `--strict` still passes)

```bash
vmake build
vmake check-symbols            # informational report
vmake check-symbols --strict   # exits 1 on any warn/error finding
```

Scope and platform rules:

- The command runs on any host but analyzes **ELF only**. PE/COFF artifacts and
  ELF files without a dynamic symbol table (`SHT_DYNSYM`) are reported as
  `not-applicable` with `info` severity and never fail `--strict`.
- Only local packages are scanned; remote packages are skipped. Targets with
  `SetDefault(false)` are excluded.
- A declared target whose built artifact is missing is a `missing-artifact`
  `error` and fails `--strict`. Run `vmake build` first.
- `--strict` exits non-zero when any `warn` or `error` finding exists; without
  it the report is informational.

## Layer 5: Prefix Isolation (Third-Party C Code)

When vendoring third-party C code with no namespacing, rename every symbol
to add a project-specific prefix:

```go
ctx.Target("liblinenoise").
    SetKind(api.TargetStatic).
    AddFiles("vendor/linenoise/*.c").
    SetSymbolPrefix("ln_")   /* linenoise_complete -> ln_linenoise_complete */
```

Implemented as a post-link `objcopy --prefix-symbols=ln_` step. Works on any
target kind (binary, shared, static, object); on a `SetPrebuilt` target the
post-link step also forces a copy instead of a symlink. Use sparingly — prefer
upstream namespacing when possible, and remember the prefix changes the symbol
names your own code must reference.

## Running / Verifying

```bash
vmake build                 # Layer 1+2 in place: hidden defaults + export.map
vmake check-symbols         # report; info-level not-applicable entries are fine
vmake check-symbols --strict  # exit code 0 when no warn/error findings remain
```

Fixture `test_data/22_version_script` combines Layers 1+2 (`SetDefaultVisibilityHidden`
plus `SetVersionScript`) and `test_data/23_link_strategy` combines Layers 3
(`AddExcludeLibs`, `SetSymbolBinding`) on shared libraries; `test_data/24_symbol_prefix`
covers Layer 5.

## Putting It All Together

A mature library package uses Layers 1+2+3 together, then audits in CI:

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.SetDefaultVisibilityHidden()   /* Layer 1: default */
    })

    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("libfoo").
            SetKind(api.TargetShared).
            AddFiles("src/*.c").
            AddPublicIncludes("include").
            SetVersionScript("export.map").        /* Layer 2: declare */
            SetSymbolBinding("static")             /* Layer 3: bind */
    })
}
```

Audit in CI:

```bash
vmake build && vmake check-symbols --strict
```

## See Also

- SKILL.md - Symbol Management
- references/api.md — Target setters, ConfigContext methods
- references/gotchas.md — static library deps and `--start-group`/`--whole-archive` semantics
- examples/multi-target.md — static + binary + test targets in one package (no shared library)
- examples/prebuilt.md — shipping a prebuilt `.a`/`.so` instead of compiling
