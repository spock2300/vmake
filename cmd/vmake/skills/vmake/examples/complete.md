# Complete API Demo

The most comprehensive example covering nearly the entire VMake API.

## Prerequisites

- VMake installed with a host C/C++ toolchain (the example sources are C++).
- Run commands from the project directory; VMake finds `build.go` by searching upward.

## build.go

```go
package main

import (
	"strconv"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.GlobalMode()

		ctx.GlobalOption("customer").
			SetType(api.OptionString).
			SetDefault("default").
			SetDescription("Customer name for branding")

		ctx.GlobalOption("product").
			SetType(api.OptionChoice).
			SetDefault("standard").
			SetValues("lite", "standard", "professional", "enterprise").
			SetDescription("Product edition")

		ctx.Option("debug").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable debug mode").
			SetGroup("General")

		ctx.Option("verbose").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Show compiler driver invocations (-v)").
			SetGroup("General")

		ctx.Option("ssl").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable SSL support").
			SetGroup("SSL")

		ctx.Option("ssl_version").
			SetType(api.OptionString).
			SetDefault("1.1.1").
			SetDescription("SSL library version").
			SetGroup("SSL").
			SetShowIf(func(c *api.ConfigContext) bool {
				return c.Bool("ssl")
			})

		ctx.Option("thread_count").
			SetType(api.OptionInt).
			SetDefault(4).
			SetDescription("Number of worker threads").
			SetGroup("Performance")

		ctx.Option("shared_lib").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Build as shared library").
			SetGroup("Build")

		ctx.Option("custom_prefix").
			SetType(api.OptionString).
			SetDefault("/usr/local").
			SetDescription("Installation prefix").
			SetGroup("Installation")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		threads := ctx.Int("thread_count")
		prefix := ctx.String("custom_prefix")
		sslVersion := ctx.String("ssl_version")

		ctx.Target("core_obj").
			SetKind(api.TargetObject).
			AddFiles("src/core.cpp").
			AddIncludes("include").
			AddDefines(ctx.Select("product", map[string]string{
				"lite":         "PRODUCT_LITE",
				"standard":     "PRODUCT_STANDARD",
				"professional": "PRODUCT_PROFESSIONAL",
				"enterprise":   "PRODUCT_ENTERPRISE",
			})).
			AddCxxFlags(ctx.If("debug", "-g", "-DDEBUG"))

		ctx.Target("utils_obj").
			SetKind(api.TargetObject).
			AddFiles("src/utils.cpp").
			AddIncludes("include")

		if ctx.When("shared_lib", true) {
			ctx.Target("mylib").
				SetKind(api.TargetShared).
				AddFiles("src/library.cpp").
				AddIncludes("src/internal").
				AddPublicIncludes("include").
				AddDeps("core_obj", "utils_obj").
				AddDefines(ctx.If("ssl", "USE_SSL")).
				AddDefines(ctx.If("ssl", "SSL_VERSION=\""+sslVersion+"\"")).
				AddDefines("THREAD_COUNT=" + strconv.Itoa(threads)).
				AddDefines("PREFIX=\"" + prefix + "\"")
		} else {
			ctx.Target("mylib").
				SetKind(api.TargetStatic).
				AddFiles("src/library.cpp").
				AddIncludes("src/internal").
				AddPublicIncludes("include").
				AddDeps("core_obj", "utils_obj").
				AddDefines(ctx.If("ssl", "USE_SSL")).
				AddDefines(ctx.If("ssl", "SSL_VERSION=\""+sslVersion+"\"")).
				AddDefines("THREAD_COUNT=" + strconv.Itoa(threads)).
				AddDefines("PREFIX=\"" + prefix + "\"")
		}

		ctx.Target("myapp").
			SetKind(api.TargetBinary).
			AddFiles("src/main.cpp").
			AddDeps("mylib").
			AddDefines("PREFIX=\"" + prefix + "\"").
			AddDefines(ctx.If("ssl", "USE_SSL")).
			AddDefines(ctx.If("ssl", "SSL_VERSION=\""+sslVersion+"\"")).
			AddCxxFlags(ctx.If("debug", "-g", "-fsanitize=address")).
			AddCxxFlags(ctx.If("verbose", "-v")).
			AddLinks(ctx.If("ssl", "ssl", "crypto")).
			AddLdFlags(ctx.If("debug", "-fsanitize=address"))

		ctx.Target("benchmark").
			SetKind(api.TargetBinary).
			AddFiles("src/benchmark.cpp").
			AddDeps("core_obj").
			AddCxxFlags("-DNDEBUG").
			SetTest(true)

		if ctx.Bool("verbose") {
			ctx.Target("debug_info").
				SetKind(api.TargetBinary).
				AddFiles("src/debug.cpp").
				AddDefines("VERBOSE_MODE").
				SetDefault(false) // [disabled]: skipped by vmake build and vmake test
		}
	})
}
```

## Project Structure

```
myproject/
├── build.go
├── include/
│   └── mylib.h
└── src/
    ├── core.cpp
    ├── utils.cpp
    ├── library.cpp
    ├── main.cpp
    ├── benchmark.cpp
    ├── debug.cpp
    └── internal/
        └── internal.h
```

## Running

```bash
# shared_lib is a package-local option: prefix it with the package name (the build.go directory)
vmake config --set myproject/shared_lib=true

vmake build
# -> build/<buildKey>/myapp
# -> build/<buildKey>/libmylib.a       (default)
# -> build/<buildKey>/libmylib.so      (shared_lib=true; libmylib.dll on Windows)
# Windows binary: build\<buildKey>\myapp.exe

vmake test
# builds and runs the benchmark test target (SetTest(true))
```

## What This Demonstrates

- **`ctx.GlobalMode()`** - Declare the built-in build-mode option (`mode`: debug/release/size) as global
- **`ctx.GlobalOption(name)`** - Package-wide options accessible to all packages
- **`api.OptionInt`** - Integer option type
- **`ctx.Int(name)`** - Read int option value
- **`SetShowIf(func)`** - Conditional option visibility
- **`api.TargetObject`** - Intermediate object file target
- **`api.TargetShared`** - Shared library (.so)
- **`ctx.When(option, value)`** - Returns bool for imperative conditionals
- **`ctx.Select(option, map)`** - Map a choice value to a define
- **Dynamic target creation** - Targets inside `if` blocks
- **`AddPublicIncludes`** - Propagates to dependents
- **`SetTest(true)`** - Test target: built and run by `vmake test`, skipped by ordinary builds

## API Coverage Summary

| Category | Methods Used |
|----------|-------------|
| Options | Bool, String, Int, Choice, ShowIf, GlobalMode/Option |
| Conditionals | If, Select, When, Bool, String, Int |
| Targets | Object, Static, Shared, Binary, SetTest(true), Default(false) |
| Flags | CxxFlags, LdFlags, Defines |
| Dependencies | AddDeps, AddLinks |
| Utilities | AddIncludes, AddPublicIncludes |

## Key Points

- Choose static vs shared based on option: `if ctx.When("shared_lib", true)`
- Global options apply across packages
- Integer options need `strconv.Itoa()` for defines
- Object targets useful for multi-stage builds
- Language is auto-detected from file extension (`.c` → C, `.cpp` → C++) — no need to set manually; `SetLanguages` only records the value and is not consumed by the scheduler
- Mode flags are appended after target flags (target → mode → global), so per-target `-O*` flags lose to the mode's `-O2`/`-O0` for GCC — select the mode instead (`vmake build --mode debug`); see `SKILL.md - Global Flags & Mode Flags`
- The builtin `host` toolchain already injects `-Wall -Wextra` and `-fPIC` when compiling and `-Wl,--as-needed` when linking (ELF targets), so the targets don't repeat them; see `SKILL.md - Default Build Flags`
- Global options may be declared by multiple packages, but each declaration must use the same `Type` and `Default` — only global options are cross-validated; see `SKILL.md - GlobalOption Cross-Package Consistency`
- `benchmark` uses `SetTest(true)`, so `vmake test` builds and runs it; `debug_info` keeps `SetDefault(false)` and is never built automatically (listed as `[disabled]`)

## See Also

- references/api.md - Full API reference
- references/gotchas.md - Linker groups, discovery reads, source patching
- SKILL.md - Target API at a Glance