# Configuration Options

Demonstrates the option system: defining build-time options and using conditional expressions to adapt compilation flags.

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

		ctx.Option("optimization").
			SetType(api.OptionChoice).
			SetDefault("O2").
			SetValues("O0", "O1", "O2", "O3", "Os").
			SetDescription("Optimization level").
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
			AddDefines("OPT_LEVEL=\"" + ctx.String("optimization") + "\"").
			AddCFlags(ctx.Select("optimization", map[string]string{
				"O0": "-O0", "O1": "-O1", "O2": "-O2", "O3": "-O3", "Os": "-Os",
			})).
			AddLinks(ctx.If("ssl", "ssl", "crypto"))
	})
}
```

## What This Demonstrates

- **`p.OnConfig`** - Config phase hook (Phase 2 in the lifecycle)
- **`ctx.Option(name)`** - Define an option with fluent API
- **`SetType(api.OptionBool/Choice/String/Int)`** - Option type
- **`SetDefault(value)`** - Default value
- **`SetDescription(text)`** - Help text
- **`SetGroup(name)`** - Grouping for TUI
- **`ctx.If("option", "value_true"...)`** - Conditional (returns values if bool is true)
- **`ctx.IfNot("option", "value")`** - Inverse conditional
- **`ctx.Select("option", map)`** - Map option value to flag
- **`ctx.String("option")`** - Read option as string

## Running with Options

```bash
# Use TUI to configure
vmake config

# Or override via command line (if toolchain supports)
vmake build --mode debug
```

## Key Points

- Options are typed: Bool, String, Int, Choice
- Conditional expressions spread into variadic methods (`...`)
- `ctx.If("debug", "-g", "-O0")` returns slice - use `...` to unpack
- Choice options require `SetValues(...)` with allowed values

## See Also

- references/api.md - Complete Option API, ConfigAccessor
- SKILL.md - Option & Conditional section