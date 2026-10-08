# Configuration Options

Demonstrates the option system: defining build-time options and using conditional expressions to adapt compilation flags.

## Prerequisites

- VMake installed with a host C toolchain (gcc or clang); run from the project directory (VMake searches upward for `build.go` or `.vmake/`).

## build.go

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("debug").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable debug mode").
			SetGroup("General")

		ctx.Option("features").
			SetType(api.OptionChoice).
			SetDefault("standard").
			SetValues("minimal", "standard", "full").
			SetDescription("Feature set").
			SetGroup("General")

		ctx.Option("ssl").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable SSL support").
			SetGroup("SSL")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("config_app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDefines(ctx.If("ssl", "USE_SSL")).
			AddDefines(ctx.If("debug", "DEBUG=1")).
			AddDefines(ctx.Select("features", map[string]string{
				"minimal":  "FEATURES_MINIMAL",
				"standard": "FEATURES_STANDARD",
				"full":     "FEATURES_FULL",
			})).
			AddDefines("FEATURE_SET=\"" + ctx.String("features") + "\"").
			AddLinks(ctx.If("ssl", "ssl", "crypto"))
	})
}
```

## Project Structure

```
myproject/
├── build.go
└── src/
    └── main.c
```

```c
#include <stdio.h>

int main(void) {
#ifdef USE_SSL
    puts("ssl enabled");
#endif
    return 0;
}
```

## What This Demonstrates

- **`p.OnConfig`** - Config phase hook (Phase 2 in the lifecycle)
- **`ctx.Option(name)`** - Define an option with fluent API
- **`SetType(api.OptionBool/Choice/String/Int)`** - Option type
- **`SetDefault(value)`** - Default value
- **`SetDescription(text)`** - Help text
- **`SetGroup(name)`** - Grouping for TUI
- **`ctx.If("option", vals...)`** - Conditional for **bool** options (returns the chosen strings when true, `nil` otherwise; pass the slice to Add* methods directly, no `...` spread)
- **`ctx.Select("option", map)`** - Map a String/Choice option value to a flag or define
- **`ctx.String("option")`** - Read a String/Choice option

## Running with Options

```bash
# Use TUI to configure
vmake config

# Or set option values non-interactively (format: [pkg/]option=value).
# The package prefix is the build.go directory name and is required for non-global options.
vmake config --set myproject/debug=true --set myproject/features=full

# Show effective option values and generated -DCONFIG_* defines
vmake query config myproject

# The global build mode (debug/release/size) can also be overridden per build
vmake build
vmake build --mode debug
vmake build --mode size
```

## Switching Configuration Files

```bash
vmake config copy config-minimal.json
vmake config describe "minimal feature set, no SSL"
vmake config use config-minimal.json
vmake config --set myproject/features=minimal
vmake build
vmake config list
```

The selection is stored in `.vmake/project.json` as
`{"config":"config-minimal.json"}`. Each file uses the existing configuration
format, including package versions, options and KConfig data, plus an optional
top-level `description` line shown by `config list` and by completion. `copy`
leaves the selection unchanged and rejects existing destinations; `describe`
prints or sets the active description; `use` switches it without running build
scripts. TUI and `--set` save only the selected file. If there was no saved
default configuration before copying, use `vmake config copy config.json`
to create it from the active configuration before switching back, since `use`
requires an existing file. In the TUI the description is the fixed `Description`
row at the top of the options panel, edited with `D` or a click.

A configuration file in full (a remote package's `version` field is a pin; edit the file directly to set it):

```json
{
  "version": "1",
  "description": "Board A debug build",
  "global": { "toolchain": "host", "mode": "debug" },
  "entries": {
    "myproject": { "options": { "debug": true } },
    "official/zlib": { "version": "1.3.1" }
  }
}
```

All configurations share `.vmake/vmake.lock` and the existing BuildKey rules.
Use distinct installation `--prefix` directories to keep multiple installed builds.
See `SKILL.md - Multiple Project Configurations` for compatibility and error handling.

## Key Points

- Options are typed: Bool, String, Int, Choice
- `AddDefines(ctx.If("ssl", "USE_SSL"))` passes the `[]string` directly — `Add*` methods accept `...any` and flatten slices. Do NOT spread with `...` (Go cannot spread `[]string` into `[]any`; `flattenAny` expands the slice for you)
- `ctx.If` only accepts `OptionBool`; `ctx.Select` and `ctx.String` work for String/Choice options, and `ctx.When`/`ctx.Int`/`ctx.Bool` cover the rest (a mismatched accessor is a build error)
- Options can only be declared in `OnConfig`; `ctx.Option(...)` after the config phase is a build error
- Choice options declare their allowed values with `SetValues(...)`; a non-empty `SetDefault` is validated against them after `OnConfig` (validation is skipped when the default is empty or `SetValues` was never called). `vmake config --set` rejects invalid choices
- Don't map options to `-O*`: build mode flags are appended after target flags, so the mode's `-O2`/`-O0` wins for GCC — see `SKILL.md - Global Flags & Mode Flags`
- Turn options into `-DCONFIG_*` defines or a generated `autoconf.h` with `ctx.GenerateConfigDefines()` / `ctx.GenerateConfigHeader()` in `OnBuild` — see `examples/config-to-define.md`
- Reading an option with a mismatched accessor (e.g. `ctx.Bool` on an OptionChoice) is a build error — use the accessor matching `SetType`: `ctx.Bool`, `ctx.String`, `ctx.Int` (there is no Choice accessor; OptionChoice is read with `ctx.String`)

## See Also

- references/api.md - Complete Option API, ConfigAccessor
- examples/config-to-define.md - Option → `-DCONFIG_*` / `autoconf.h` generation
- SKILL.md - Option & Conditional
