# Conditional Compilation

Comprehensive demonstration of conditional expressions: bool toggles, platform selection, and conditional defines.

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

		ctx.Option("level").
			SetType(api.OptionInt).
			SetDefault(1).
			SetDescription("Feature level").
			SetGroup("Features")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		target := ctx.Target("conditional_app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddDefines(ctx.If("debug", "DEBUG_MODE")).
			AddDefines(ctx.If("verbose", "VERBOSE")).
			AddDefines(ctx.If("feature_a", "FEATURE_A")).
			AddDefines(ctx.If("feature_b", "FEATURE_B")).
			AddDefines("PLATFORM=\"" + ctx.String("platform") + "\"").
			AddCFlags(ctx.If("debug", "-g", "-O0"))
		if !ctx.Bool("debug") {
			target.AddCFlags("-O2")
		}
		if ctx.When("level", 2) {
			target.AddDefines("LEVEL_2")
		}
		target.AddCFlags(ctx.Select("platform", map[string]string{
			"linux":   "-DLINUX",
			"macos":   "-DMACOS",
			"windows": "-DWINDOWS",
		}))
	})
}
```

## What This Demonstrates

- **`ctx.If`** - Returns the chosen values when a bool option is true, `nil` otherwise
- **`ctx.Select`** - Map option value to different flags
- **`ctx.When`** - Compare any option type imperatively (`ctx.When("level", 2)` returns a bool)
- **`ctx.String`** - Read string option value
- **`ctx.Bool`** - Read bool option value
- **`SetGroup`** - Organize options in TUI

## Usage

```bash
# TUI
vmake config
# Enable debug, set platform to windows

# Or non-interactively: the package prefix is the build.go directory name
# and is required for non-global options
vmake config --set myproject/debug=true --set myproject/platform=windows

vmake build --mode debug
# Compiles with -g -O0 -DDEBUG_MODE -DWINDOWS (mode flags are appended after target flags)
```

## Conditional Patterns

| Method | Use Case |
|--------|----------|
| `ctx.If("debug", "-g", "-O0")` | Toggle flags for a **bool** option (returns `[]string`, pass directly) |
| `ctx.Select("platform", {...})` | Platform-specific flags (returns `string`) |
| `ctx.When("level", 2)` | Compare an int option (returns `bool`) |
| `ctx.If("debug", "DEBUG")` | Conditional defines |

## Conditional Dependencies (OnRequire)

```go
p.OnConfig(func(ctx *api.ConfigContext) {
	ctx.Option("ssl").SetType(api.OptionBool).SetDefault(false)
})

p.OnRequire(func(ctx *api.RequireContext) {
	if ctx.When("ssl", true) {
		ctx.AddRequires("official/openssl")
	}
})
```

`OnRequire` runs twice. In pass 1 (discovery, nil config) the condition is unknown: `When` returns `true`, so the package is resolved eagerly. In pass 2 (`FilterDeps`, real values) a false condition drops the edge. The reverse is not allowed: pass 2 must not add a package that pass 1 did not declare. See `SKILL.md - OnRequire Two-Phase Execution`.

## Key Points

- `ctx.If` returns a `[]string` — pass it to `Add*` methods **directly** (`AddCFlags(ctx.If(...))`); `flattenAny` expands `[]string`/`[]any` items. Never spread with `...` — Go cannot spread a `[]string` into the `...any` parameter
- `ctx.If("opt", "a", "b")` returns `["a", "b"]` when true and `nil` when false; during discovery (nil config) it returns the values unconditionally
- `ctx.If` only accepts bool options; `ctx.When(option, value)` compares any type (in `OnBuild` and pass 2) and is the safe discovery-aware helper
- Multiple conditionals can stack
- In `OnRequire`, direct reads (`ctx.Bool/String/Int`) are fatal only on pass 1 (nil config); pass 2 (`FilterDeps`) reads real values. On pass 1, `When` → `true`, `If` → its values, `Select` → `""`
- Target `-O*` flags are appended before mode flags, so the mode's `-O2`/`-O0` wins for GCC — prefer `vmake build --mode debug` over per-target `-O` flags; see `SKILL.md - Global Flags & Mode Flags`

## See Also

- references/api.md - ConfigAccessor methods
- references/gotchas.md - Discovery reads and linker pitfalls
- examples/complete.md - Full API demo