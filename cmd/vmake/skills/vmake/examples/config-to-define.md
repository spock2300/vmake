# Config Options → C Compiler Defines

There are **three mechanisms** for mapping config options to `-D` compiler flags, plus `GenerateConfigHeader()` when you want the configuration in an `autoconf.h` header instead.
Pick the right one based on **macro naming** and **scope**.

## Prerequisites

- Go 1.26+ and a vmake binary built from the repository root: `CGO_ENABLED=0 go build -o vmake ./cmd/vmake`
- A working C compiler for the `host` toolchain
- Run commands from the project directory that contains `build.go` (local package name = directory name)

## Decision Table

| Mechanism | Scope | Macro naming | When |
|---|---|---|---|
| `GenerateConfigDefines()` | All targets in this package | Auto: `-DCONFIG_<NAME>=<value>` (bool true → `=1`, string → quoted; disabled bool → no define) | You control both option names and C code (`#if CONFIG_FOO`) |
| `OnBuild` + `ctx.Bool()` + `AddDefines` | One target (in this package) | Manual: any name | Third-party library expects specific names (e.g., lwIP wants `LWIP_PERF`, not `CONFIG_LWIP_PERF`) |
| `SetOnApply` + `AddGlobalCFlags` | Global (all packages) | Manual: any name | Architecture-wide flags all packages need (e.g., `-DAIC8800M40`) |

For the same automatic `CONFIG_*` macros as `GenerateConfigDefines` but from imported packages, see `examples/config-propagate.md`.

## Mechanism 1: GenerateConfigDefines — Automatic CONFIG_ Prefix

Simplest. Register options in `OnConfig`, call `ctx.GenerateConfigDefines()` in `OnBuild`. The defines are applied to every target of this package after `OnBuild` returns, so call position doesn't matter. Macro names are `CONFIG_<OPTION_NAME>` (uppercased, `-` → `_`); disabled bools emit no define (the generated header writes `/* #undef */` instead), and Choice options emit a second `CONFIG_X_<VALUE>=1` macro where the value is uppercased with `-` replaced by `_`.

The primary Choice macro is a **quoted string**: the argv element `-DCONFIG_PLATFORM="linux"` literally contains the quote characters (no shell is involved), and `compile_commands.json` stores it as `"-DCONFIG_PLATFORM=\"linux\""`. `SetValues` entries containing anything other than letters, digits, `_` or `-` (spaces, dots, `/`) produce invalid secondary macro names — keep choice values identifier-safe.

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("debug").SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable debug mode")
		ctx.Option("tick_hz").SetType(api.OptionInt).
			SetDefault(1000).
			SetDescription("Tick rate in Hz")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.GenerateConfigDefines()   // debug defaults to false → no define; emits -DCONFIG_TICK_HZ=1000

		ctx.Target("app").SetKind(api.TargetBinary).
			AddFiles("src/*.c")
	})
}
```

C code: `#if CONFIG_DEBUG`, configure with `vmake config`.

`SyncConfigDefines("a", "b")` is shorthand for `GenerateConfigDefines()` + `ImportConfig("a", "b")` (see `examples/config-propagate.md`).

**Limitation**: macro name is always `CONFIG_<OPTION_NAME>` (uppercased, `-` → `_`); disabled bools emit no `-D` at all. If your C code expects a different name (e.g., `LWIP_STATS`), use Mechanism 2.

## GenerateConfigHeader: generated/autoconf.h

`ctx.GenerateConfigHeader()` in `OnBuild` writes `generated/autoconf.h` under the package's build directory (`build/<buildKey>/generated/` for local packages) and adds that directory to the include path of **this package's targets only** — dependents do not see it, and it is not installed. Include it from the package's own C sources as `#include "autoconf.h"`.

Content rules (same option collection as `GenerateConfigDefines`, driven by `ConfigToHeader`):

- Bool true → `#define CONFIG_X 1`; bool false → `/* #undef CONFIG_X */`
- Int → `#define CONFIG_X 42`; String/Choice → `#define CONFIG_X "value"`
- Choice additionally emits `#define CONFIG_X_VALUE 1` (uppercase, `-` → `_`)
- An option with neither a configured value nor a `SetDefault` is skipped entirely (not even an `#undef` comment)
- `Group("Global")` options (including `mode`/`toolchain` and `ctx.GlobalOption`) are excluded

### Macro table

| Option kind | Value | `-D` form (defines) | `autoconf.h` form |
|---|---|---|---|
| Bool | true | `-DCONFIG_X=1` | `#define CONFIG_X 1` |
| Bool | false | (none) | `/* #undef CONFIG_X */` |
| Int | 42 | `-DCONFIG_X=42` | `#define CONFIG_X 42` |
| String | `"uart0"` | `-DCONFIG_X="uart0"` | `#define CONFIG_X "uart0"` |
| Choice | `"fast"` | `-DCONFIG_X="fast"` + `-DCONFIG_X_FAST=1` | `#define CONFIG_X "fast"` + `#define CONFIG_X_FAST 1` |
| any | unset, no default | (none) | (none) |

```go
p.OnBuild(func(ctx *api.BuildContext) {
	ctx.GenerateConfigHeader()   // writes generated/autoconf.h, adds it to this package's include path

	ctx.Target("app").SetKind(api.TargetBinary).
		AddFiles("src/*.c")
})
```

Both mechanisms can be used together; `autoconf.h` never propagates, so public headers must not include it. `test_data/18_config_header` is the reference fixture.

## Mechanism 2: AddDefines with Manual Naming — Per-Target Control

Register options normally. In `OnBuild`, read values with `ctx.Bool()`/`ctx.Int()`/`ctx.String()` and build a define list manually. Pass to `target.AddDefines()`. Macro names are completely under your control.

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("perf").SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable performance counters")
		ctx.Option("stats").SetType(api.OptionBool).
			SetDefault(true).
			SetDescription("Enable statistics collection")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		var defines []string
		if ctx.Bool("perf") {
			defines = append(defines, "LWIP_PERF=1")
		}
		if !ctx.Bool("stats") {
			defines = append(defines, "LWIP_STATS=0")
		}

		ctx.Target("lwip").SetKind(api.TargetStatic).
			AddDefines(defines). // []string flattens into ...any — no "..." spread
			AddFiles("src/*.c")
	})
}
```

**Key points**:
- `AddDefines("LWIP_PERF=1")` produces `-DLWIP_PERF=1` on the compiler command line
- `AddDefines("KEY")` (no value) produces `-DKEY`
- Boolean options do NOT auto-convert to 1/0 — you must construct the string yourself
- The accessor must match `SetType`: `ctx.Bool` for `OptionBool`, `ctx.String` for `OptionString`/`OptionChoice` — a mismatch is a build error
- Use `ctx.BoolStr("name")` to get `"ON"` / `"OFF"` strings instead of `true`/`false`

**Also works with `AddCFlags` directly** — useful for flags that aren't pure defines:

```go
ctx.Target("app").SetKind(api.TargetBinary).
	AddCFlags(ctx.If("debug", "-DDEBUG=1")).
	AddCFlags(ctx.If("perf", "-DLWIP_PERF=1"))
```

The `debug` build mode already injects `-O0 -g`, so don't repeat optimization/debug flags in `ctx.If`; `AddDefines` is clearer when the purpose is purely `-D` defines — see `SKILL.md - Global Flags & Mode Flags`.

## Mechanism 3: SetOnApply + AddGlobalCFlags — Global Flags

When a flag must be visible to **all packages** in the build, use `SetOnApply` with `AddGlobalCFlags`. The callback fires when config is resolved; you receive the resolved value and inject global compiler flags.

```go
p.OnConfig(func(ctx *api.ConfigContext) {
	ctx.Option("cpu_clock_hz").SetType(api.OptionChoice).
		SetDefault("160000000").
		SetValues("240000000", "160000000", "80000000").
		SetDescription("CPU clock frequency").
		SetOnApply(func(ctx *api.ConfigContext, val any) {
			ctx.AddGlobalCFlags("-DCONFIG_CPU_CLOCK_HZ=" + val.(string))
		})
})
```

Global flags apply to ALL targets in ALL packages. They are appended in declaration order (duplicates are preserved — no deduplication), buffered per package (flags from packages pruned by `FilterDeps` never leak), and their changes rebuild artifacts (global flags are part of the BuildKey). Use sparingly — prefer Mechanism 1 or 2 unless the flag truly needs cross-package visibility.

**Note**: `val` inside `SetOnApply` is already typed to the declared option type (`string` for Choice) — no `float64` conversion needed for Int options either.

## Reading Config Values

All accessors (`Bool`, `Int`, `String`, `BoolStr`, `When`, `If`, `Select`) exist on a shared `ConfigAccessor`, but **which values they see depends on the phase**:

| Phase | Values visible |
|---|---|
| `OnConfig` | Declared `SetDefault` values plus earlier `SetConfigValue` results only. Values saved in the active configuration (`.vmake/config.json` by default) are NOT loaded yet |
| `SetOnApply` | The resolved value for that option (saved config value, else default), normalized to the declared type |
| `OnBuild` / `OnInstall` / `OnClean` | Full resolved values: saved config + defaults + built-in/global options |
| `OnRequire` | None during discovery; only `When`/`If`/`Select` are allowed, direct reads are build errors |

```go
ctx.Bool("debug")           // bool → bool (OptionBool only — accessor must match SetType)
ctx.Int("tick_hz")          // int → int (OptionInt only)
ctx.String("platform")      // string → string (OptionString/OptionChoice only)
ctx.BoolStr("debug")        // bool → "ON" / "OFF"
ctx.When("x", "val")        // true iff option "x" == "val" (works in OnRequire too)
```

In `OnConfig`, options must already be declared (`ctx.Option(...)`) in that same callback before they can be read. Do not branch on user configuration there — move that logic to `OnBuild`, `OnInstall`, `OnClean`, or `SetOnApply`.

Newly registered options that haven't been written to the active configuration yet (first build after adding an option) will use their `SetDefault` value in `OnBuild`. No need to run `vmake config` first.

## Running / Verifying

Minimal project (the local root package name is the directory name, here `myapp`):

`myapp/build.go`:

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("debug").SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable debug output")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.GenerateConfigDefines()

		ctx.Target("app").SetKind(api.TargetBinary).
			AddFiles("src/*.c")
	})
}
```

`myapp/src/main.c`:

```c
#include <stdio.h>

int main(void) {
#ifdef CONFIG_DEBUG
    printf("debug build\n");
#else
    printf("release build\n");
#endif
    return 0;
}
```

```bash
cd myapp
vmake config --set myapp/debug=true   # local option: <package>/<option>; global options use --set debug=true (no prefix)
vmake build --install
./install/bin/app                     # expected: debug build
```

Without the `--set`, `build/compile_commands.json` has no `-DCONFIG_DEBUG` and the binary prints `release build`. With it, the entry for `src/main.c` contains `-DCONFIG_DEBUG=1`. `test_data/19_config_defines` is the reference fixture for the defines path.

## Common Mistakes

**Using `GenerateConfigDefines` but C code expects non-CONFIG_ names:**
```go
// WRONG: generates -DCONFIG_LWIP_PERF=1, but lwIP checks #if LWIP_PERF
ctx.GenerateConfigDefines()
```
Fix: use Mechanism 2 with `AddDefines("LWIP_PERF=1")`.

**Expecting `AddDefines("LWIP_PERF")` to auto-set value from bool option:**
```go
// WRONG: produces -DLWIP_PERF (no value), lwIP expects #if LWIP_PERF=1
AddDefines("LWIP_PERF")
```
Fix: construct the full `"KEY=VALUE"` string: `AddDefines("LWIP_PERF=1")`.

**Reading saved config in `OnConfig`:**
```go
// WRONG: sees SetDefault/SetConfigValue only, never saved configuration values
p.OnConfig(func(ctx *api.ConfigContext) {
    if ctx.Bool("debug") { /* never the user's configured value */ }
})
```
Fix: read resolved values in `OnBuild`/`OnInstall`/`OnClean`, or react with `SetOnApply`.

**Confusing scope of `GenerateConfigDefines`**:
- `GenerateConfigDefines()` emits `-D` flags for the current package's targets only. It does NOT propagate to dependent packages automatically. For that, the dependent package must call `ImportConfig` then `GenerateConfigDefines` (or `SyncConfigDefines`), and `autoconf.h` never crosses package boundaries. Global options are excluded from generated defines entirely.

## See Also

- **examples/config.md** — Options basics (ctx.If, ctx.Select, SetGroup)
- **examples/conditional.md** — Bool toggles, platform selection, conditional compilation
- **examples/config-propagate.md** — Cross-package config propagation with ImportConfig/SyncConfigDefines
- Option types, SetOnApply, accessor rules — `SKILL.md - Option & Conditional`
- The Choice primary + secondary macro pair — `SKILL.md - OptionChoice Generates Dual Macros`
- Frequent build.go errors — `SKILL.md - Common Mistakes`
