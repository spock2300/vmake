# Conditional Compilation

Comprehensive demonstration of conditional expressions: bool toggles, platform selection, and conditional defines.

## build.go

```go
package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("debug").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable debug mode").
			SetGroup("General")

		ctx.Option("verbose").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable verbose output").
			SetGroup("General")

		ctx.Option("feature_a").
			SetType(api.OptionBool).
			SetDefault(true).
			SetDescription("Enable feature A").
			SetGroup("Features")

		ctx.Option("feature_b").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable feature B").
			SetGroup("Features")

		ctx.Option("platform").
			SetType(api.OptionChoice).
			SetDefault("linux").
			SetValues("linux", "macos", "windows").
			SetDescription("Target platform").
			SetGroup("Platform")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("conditional_app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDefines(ctx.If("debug", "DEBUG_MODE")).
			AddDefines(ctx.If("verbose", "VERBOSE")).
			AddDefines(ctx.If("feature_a", "FEATURE_A")).
			AddDefines(ctx.If("feature_b", "FEATURE_B")).
			AddDefines("PLATFORM=\"" + ctx.String("platform") + "\"").
			AddCFlags(ctx.If("debug", "-g", "-O0")).
			AddCFlags(ctx.IfNot("debug", "-O2")).
			AddCFlags(ctx.Select("platform", map[string]string{
				"linux":   "-DLINUX",
				"macos":   "-DMACOS",
				"windows": "-DWINDOWS",
			}))
	})
}
```

## What This Demonstrates

- **`ctx.If`** - Returns values if bool option is true, empty slice otherwise
- **`ctx.IfNot`** - Returns values if bool is false
- **`ctx.Select`** - Map option value to different flags
- **`ctx.String`** - Read string option value
- **`ctx.Bool`** - Read bool option value
- **`SetGroup`** - Organize options in TUI

## Usage

```bash
vmake config
# Enable debug, set platform to windows

vmake build
# Compiles with -g -O0 -DWINDOWS
```

## Conditional Patterns

| Method | Use Case |
|--------|----------|
| `ctx.If("debug", "-g", "-O0")...` | Toggle flags |
| `ctx.IfNot("debug", "-O2")...` | Inverse conditional |
| `ctx.Select("platform", {...})` | Platform-specific flags |
| `ctx.If("debug", "DEBUG")...` | Conditional defines |

## Key Points

- Conditional methods return slices - spread with `...`
- `ctx.If("opt", "a", "b")` returns `["a", "b"]`
- `ctx.If("opt", "value")` at start, then other flags after
- Multiple conditionals can stack

## See Also

- references/api.md - ConfigAccessor methods
- examples/complete.md - Full API demo