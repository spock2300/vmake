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
| 1. Default hidden | `-fvisibility=hidden` + `-fvisibility-inlines-hidden` | 90% of leaks — all symbols default to non-exported | `ctx.SetDefaultVisibilityHidden()` |
| 2. Declare exports | version-script on shared libs | Declarative public API surface | `target.SetVersionScript("foo.map")` |
| 3. Link policy | `--exclude-libs`, `-Bsymbolic` | Static archive absorption; internal binding | `target.SetExcludeLibs(...)`, `target.SetSymbolBinding("static")` |
| 4. Audit | scan `.dynsym` against expectations | Catch unexpected exports / conflicts | `target.SetExpectedExports(...)`, `vmake check-symbols` |
| 5. Prefix isolation | `objcopy --prefix-symbols=` | Force namespace onto third-party C code | `target.SetSymbolPrefix("vendor_")` |

Layer 1 is the foundation. Without default-hidden visibility, version-scripts
only redefine which of the (many) exported symbols remain exported — weak
protection. Always start with Layer 1.

## Layer 1: Default Hidden Visibility

Compile every source file with hidden default visibility. Only symbols
explicitly marked become public. This is what glibc, libc++, Boost, and most
professional C/C++ libraries do.

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

This adds `-fvisibility=hidden` to C and C++ compiler flags, plus
`-fvisibility-inlines-hidden` to C++ only (that flag is invalid for C).

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
every global symbol from that archive becomes exported. Use `SetExcludeLibs`
to strip them:

```go
ctx.Target("libfoo").
    SetKind(api.TargetShared).
    AddFiles("src/*.c").
    AddDeps("vendor:helper_lib").   /* static lib from another package */
    SetExcludeLibs("helper_lib")    /* don't re-export its symbols */
```

This passes `-Wl,--exclude-libs=helper_lib` to the linker. Use `ALL` to strip
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

Declare expected exports per target, then run `vmake check-symbols` to verify
the build actually produces them — and nothing else.

```go
ctx.Target("libfoo").
    SetKind(api.TargetShared).
    AddFiles("src/*.c").
    SetVersionScript("export.map").
    SetExpectedExports("foo_api", "foo_init", "foo_shutdown")
```

```bash
vmake build
vmake check-symbols --strict
```

Output flags:

- **Unexpected exports**: a symbol in `.dynsym` not in the expected list
- **Missing exports**: an expected symbol not found in `.dynsym`
- **Cross-library conflicts**: the same symbol exported by two different
  shared libraries in the build graph

`--strict` exits non-zero on any discrepancy (for CI). Without it, the report
is informational.

`SetExpectedExports` is purely an audit assertion — it does NOT affect the
build itself. The version script remains the source of truth for what's
actually exported.

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

A mature library package uses Layers 1+2+4 together:

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
            SetSymbolBinding("static").            /* Layer 3: bind */
            SetExpectedExports("foo_api", "foo_init", "foo_shutdown")  /* Layer 4 */
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
