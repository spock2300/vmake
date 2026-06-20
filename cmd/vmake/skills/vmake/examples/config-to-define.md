# Config Options → C Compiler Defines

There are **three mechanisms** for mapping config options to `-D` compiler flags.
Pick the right one based on **macro naming** and **scope**.

## Decision Table

| Mechanism | Scope | Macro naming | When |
|---|---|---|---|
| `GenerateConfigDefines()` | All targets in this package | Auto: `-DCONFIG_<NAME>=<value>` | You control both option names and C code (`#if CONFIG_FOO`) |
| `OnBuild` + `ctx.Bool()` + `AddDefines` | One target (in this package) | Manual: any name | Third-party library expects specific names (e.g., lwIP wants `LWIP_PERF`, not `CONFIG_LWIP_PERF`) |
| `SetOnApply` + `AddGlobalCFlags` | Global (all packages) | Manual: any name | Architecture-wide flags all packages need (e.g., `-DAIC8800M40`) |

## Mechanism 1: GenerateConfigDefines — Automatic CONFIG_ Prefix

Simplest. Register options in `OnConfig`, call `ctx.GenerateConfigDefines()` in `OnBuild` before any targets. Every option becomes `-DCONFIG_<NAME>=<value>`.

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
		ctx.GenerateConfigDefines()   // emits -DCONFIG_DEBUG=1 -DCONFIG_TICK_HZ=1000

		ctx.Target("app").SetKind(api.TargetBinary).
			AddFiles("src/*.c")
	})
}
```

C code: `#if CONFIG_DEBUG`, configure with `vmake config`.

**Limitation**: macro name is always `CONFIG_<OPTION_NAME>`. If your C code expects a different name (e.g., `LWIP_STATS`), use Mechanism 2.

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
		var defines []any
		if ctx.Bool("perf") {
			defines = append(defines, "LWIP_PERF=1")
		}
		if !ctx.Bool("stats") {
			defines = append(defines, "LWIP_STATS=0")
		}

		ctx.Target("lwip").SetKind(api.TargetStatic).
			AddDefines(defines...).
			AddFiles("src/*.c")
	})
}
```

**Key points**:
- `AddDefines("LWIP_PERF=1")` produces `-DLWIP_PERF=1` on the compiler command line
- `AddDefines("KEY")` (no value) produces `-DKEY`
- Boolean options do NOT auto-convert to 1/0 — you must construct the string yourself
- `ctx.Bool()` works for both `OptionBool` and `OptionChoice` (returns true if choice value matches string name)
- Use `ctx.BoolStr("name")` to get `"ON"` / `"OFF"` strings instead of `true`/`false`

**Also works with `AddCFlags` directly** — useful for flags that aren't pure defines:

```go
ctx.Target("app").SetKind(api.TargetBinary).
	AddCFlags(ctx.If("debug", "-g", "-O0")...).
	AddCFlags(ctx.If("perf", "-DLWIP_PERF=1")...)
```

But `AddDefines` is clearer when the purpose is purely `-D` defines.

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

Global flags apply to ALL targets in ALL packages. They are deduplicated. Use sparingly — prefer Mechanism 1 or 2 unless the flag truly needs cross-package visibility.

**Important**: `ctx.Select()` returns `""` during vmake's internal discoverAll phase. If you call `AddGlobalCFlags` in `SetOnApply` with a value derived from `ctx.Select()`, guard with `if flag != ""`.

## Reading Config Values in OnBuild

All option values are available in `OnBuild` via the ConfigAccessor:

```go
ctx.Bool("debug")           // bool → bool
ctx.Int("tick_hz")          // int → int
ctx.String("platform")      // string → string
ctx.BoolStr("debug")        // bool → "ON" / "OFF"
ctx.When("x", "val")        // true iff option "x" == "val"
ctx.Equal("x", "val", "dep") // returns "dep" iff option "x" == "val", else ""
```

These work anywhere `ConfigAccessor` is available: `OnBuild`, `OnRequire`, `OnInstall`.

Newly registered options that haven't been written to `.vmake/config.json` yet (first build after adding an option) will use their `SetDefault` value. No need to run `vmake config` first.

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

**Confusing scope of `GenerateConfigDefines`**:
- `GenerateConfigDefines()` emits `-D` flags for the current package's targets only. It does NOT propagate to dependent packages automatically. For that, the dependent package must call `ImportConfig` then `GenerateConfigDefines`.

## See Also

- **examples/config.md** — Options basics (ctx.If, ctx.Select, SetGroup)
- **examples/conditional.md** — Bool toggles, platform selection, conditional compilation
- **examples/config-propagate.md** — Cross-package config propagation with ExportConfig/ImportConfig
- **SKILL.md** — Option & Conditional section
