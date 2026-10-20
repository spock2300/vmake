# Simple C Project

The most minimal VMake build script - builds a single C binary from source files.

## Prerequisites

- `vmake` is installed. The built-in `host` toolchain uses `gcc`/`g++` on Linux and `cc`/`c++` on macOS; run `vmake doctor` to check it. Do not add compiler paths to `PATH`; select a `toolchain.json` for any other compiler.
- VMake searches upward from the current directory for `.vmake/`, `build.go`, or a `*/build.go`. The output commands below assume you are at the project root; from elsewhere use `vmake -C <project> build` and absolute output paths.

## build.go

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("hello").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c")
	})
}
```

## What This Demonstrates

- **`package main`** with `func Main(p *api.Package)` - required entry point
- **`p.OnBuild`** - Build phase hook (Phase 4)
- **`ctx.Target("hello")`** - Create a target named "hello"
- **`SetKind(api.TargetBinary)`** - Target produces an executable
- **`AddFiles("src/*.c")`** - Source files with glob pattern

## Project Structure

```
myproject/
├── build.go
└── src/
    └── main.c
```

Minimal `src/main.c`:

```c
#include <stdio.h>

int main(void) {
    puts("hello from vmake");
    return 0;
}
```

## Running

```bash
vmake doctor               # verify the host C toolchain once
vmake toolchain show host  # show resolved compiler paths
vmake build
./build/*/hello            # <buildKey> is the 64-hex SHA-256 build directory key
ls build/                  # list variants if the glob is ambiguous
# Windows: .\build\<buildKey>\hello.exe
```

## Key Points

- No `OnConfig` needed if no build options
- No `OnRequire` needed if no third-party dependencies
- Glob patterns (`src/*.c`) match multiple files and resolve against `SourceDir()` (the `build.go` directory), not the current working directory
- A glob that matches nothing is not an error: a binary target fails later at link time with no inputs, while a static target produces an empty archive
- For local packages the output binary goes to `build/<buildKey>/<target>`; remote packages build under `.vmake_deps/<repo>/<pkg>/out/<sha256(member)>/<buildKey>/build/`
- `<buildKey>` hashes the build format version, toolchain, mode, options, and the global-flags hash (plus a `default-visibility=hidden` marker when active); local `SetGit` packages add their source commit, remote packages add version/commit/patch hash

## See Also

- references/api.md - Target setters and TargetKind constants
- references/dirs.md - SourceDir vs SrcDir, BuildKey naming
- examples/config.md - Adding build options