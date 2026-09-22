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
- Static archives absorbed via `--whole-archive` export every global symbol,
  polluting the final binary's namespace.

The fix is layered: **default hidden → declare exports → link policy → audit**.

## The Five Layers

| Layer | Mechanism | Solves | API |
|-------|-----------|--------|-----|
| 1. Default hidden | `-fvisibility=hidden` + `-fvisibility-inlines-hidden` | This package's symbols default to non-exported | `ctx.SetDefaultVisibilityHidden()` |
| 2. Declare exports | version-script on shared libs | Declarative public API surface | `target.SetVersionScript("foo.map")` |
| 3. Link policy | `--exclude-libs`, `-Bsymbolic` | Static archive absorption; internal binding | `target.AddExcludeLibs(...)`, `target.SetSymbolBinding("static")` |
| 4. Audit | `nm -D` scan of all Shared/Binary outputs | Duplicate/mangled/reserved leaks, version-script violations | `vmake check-symbols [--strict]` |
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

For a cross-platform macro that also covers MSVC, put this in a single header:

```c
/* visibility.h */
#if defined(_WIN32)
  #if defined(FOO_BUILD_DLL)
    #define FOO_EXPORT __declspec(dllexport)
  #else
    #define FOO_EXPORT __declspec(dllimport)
  #endif
#else
  #define FOO_EXPORT __attribute__((visibility("default")))
#endif
```

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

`SetVersionScript` is only valid on `TargetShared` and `TargetBinary`. Calling
it on `TargetObject` is a fatal error — `cc -r` (partial link) does not
produce a dynamic symbol table, so the version script has no effect. For
object-level visibility control, use Layer 1 (compile-time visibility).

### Version Script Syntax Cheat Sheet

- `global: sym1; sym2;` — exported symbols (one per line, semicolon-terminated)
- `local: *;` — hide everything not listed in `global`
- `local: *foo_internal*;` — hide by pattern (wildcards supported)
- `extern "C" { ... }` — wrap C++ mangled names to declare them unmangled
- Multiple version nodes (`V_1_0 { ... }; V_2_0 { ... };`) for versioned ABI
- Comments: `/* block */` and `// line`

## Layer 3: Link Policy

### Strip symbols from absorbed static libraries

When `TargetShared` links a static `.a` (via `--whole-archive` by default),
every global symbol from that archive becomes exported. Use `AddExcludeLibs`
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

`vmake check-symbols` scans all built Shared/Binary outputs via `nm -D` — no
per-target declaration required — and reports:

- **Cross-target duplicate exports**: the same symbol exported by two targets
  in the build graph (collision risk at final link)
- **C++ mangled leaks**: `_Z*` symbols exported from C-facing libraries
- **Reserved-prefix leaks**: `__libc_*` and similar reserved prefixes
- **Version-script violations**: exports outside what a declared
  `SetVersionScript` allows
- **Missing version-script warnings**: shared libraries without any version
  script

```bash
vmake build
vmake check-symbols --strict
```

`--strict` exits non-zero on warn/error findings (info-level still passes) —
use in CI. Without it, the report is informational.

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
target kind (binary, shared, static, object). Use sparingly — prefer upstream
namespacing when possible, and remember the prefix changes the symbol names
your own code must reference.

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

- references/api.md — Target setters, ConfigContext methods
- examples/multi-target.md — Static + shared + binary in one package
- references/gotchas.md — Static library deps with unreferenced symbols
