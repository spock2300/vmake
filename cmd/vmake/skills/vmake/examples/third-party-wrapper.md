# Third-Party Package Wrapper

Wrapping an external C/C++ library (CMake, Autotools, etc.) as a vmake package using `TargetVoid` and `SetBuildFunc`. This is the pattern used for **registry repo** packages, and for local packages that wrap a downloaded library.

For CMake projects, prefer `CMakeConfigure`, `CMakeBuild`, and `CMakeInstall`.
They manage the toolchain, paths, build configuration, global flags, and
parallelism; build.go supplies project options and project-specific steps.
Call `cmake` directly only for operations these APIs cannot express.

## Prerequisites

- Go 1.26+ and a vmake binary built from the repository root: `CGO_ENABLED=0 go build -o vmake ./cmd/vmake`
- Network access for `SetGit` (or an existing package working tree under the project's `.vmake_deps/` plus the pinned commit in `.vmake/vmake.lock`), plus the upstream build tools (`cmake`/`make`) and a C/C++ compiler
- A consuming project that requires the wrapper with `AddRequires` + `AddDeps` (see `examples/with-package.md`)

## build.go

```go
package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://github.com/user/somelib.git").
			AddVersion("1.0.0", "v1.0.0").
			AddVersion("1.1.0", "v1.1.0").
			SetDescription("A C/C++ library for doing things").
			SetLicense("MIT")
	})

	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("somelib").
			SetKind(api.TargetVoid).
			AddProvidedLibs("somelib", "somelib_math").
			SetBuildFunc(func(p *api.Package) error {
				p.CMakeConfigure("-DBUILD_SHARED_LIBS=OFF", "-DBUILD_TESTS=OFF")
				p.CMakeBuild()
				p.CMakeInstall()
				return nil
			})
	})
}
```

## What This Demonstrates

- **`p.OnPackage`** — Package metadata phase (runs during buildscript extraction, before any lifecycle phases)
- **`SetGit(urls...)`** — Git repository URLs for source download
- **`AddVersion(version, ref)`** — Map human-readable version to git ref (tag/commit)
- **`AddProvidedLibs(libs...)`** — Library names that consumers will link against
- **`api.TargetVoid`** — Target that doesn't produce a compilation artifact
- **`SetBuildFunc(fn)`** — Custom build function; receives `*Package`, returns `error`

## How It Works

1. **OnPackage**: Declares metadata so the dependency resolver can download and version-match the source
2. **OnBuild**: `TargetVoid` with `SetBuildFunc` runs the external build system
3. The `SetBuildFunc` callback receives a `*Package` with the source, build, and installation directories resolved
4. CMake configures `SrcDir()` into `CMakeBuildDir()` (default `BuildDir()/cmake`) and installs to `CMakeInstallDir()` (the remote `InstallDir()` or local `BuildDir()/staging`)

## CMake Configuration and Artifacts

The default configuration follows VMake's debug/release/size mode
(Debug/Release/MinSizeRel). Use `SetCMakeBuildType("RelWithDebInfo")` for another
configuration, shared by configure, build, and install. Use `SetCMakeBuildDir` /
`SetCMakeInstallDir` to change paths;
relative paths are based on `BuildDir()`. Pass project-specific `-D` options to
`CMakeConfigure`, targets to `CMakeBuild("--target", "name")`, and install options
to `CMakeInstall("--component", "name")`. Configure presets are supported through
`CMakeConfigure("--preset", "name")`; build presets are rejected because they
override the managed build directory. See `references/api.md` for overrides.

Global C/CXX and linker flags are inherited automatically. Default hidden
visibility is package-local: an application's `SetDefaultVisibilityHidden()`
does not change this wrapper's upstream export rules. If the wrapper enables
hidden visibility itself, its CMake build receives those defaults. To add a C
flag while retaining package defaults and global flags, pass `"-DCMAKE_C_FLAGS="+p.MergedCFlags("-fno-builtin")`. ASM flags
must be supplied explicitly when the project needs them. The helpers resolve
compiler/binutils paths and target settings; do not recreate a generic toolchain
file or concatenate compiler prefixes in the wrapper.

A local package can expose a CMake-installed static library with `SetPrebuilt`.
Use the directory getter so declarations agree with all three CMake stages:

```go
p.OnBuild(func(ctx *api.BuildContext) {
    p.SetCMakeBuildType("MinSizeRel")
    ctx.Target("foo_build").SetKind(api.TargetVoid).
        SetBuildFunc(func(p *api.Package) error {
            p.CMakeConfigure("-DBUILD_SHARED_LIBS=OFF")
            p.CMakeBuild()
            p.CMakeInstall()
            return nil
        })
    ctx.Target("foo").SetKind(api.TargetStatic).
        AddDeps("foo_build").
        SetPrebuilt(filepath.Join(p.CMakeInstallDir(), "lib", "libfoo.a"))
})
```

Import `path/filepath` for this example. The CMake archive lives in
`CMakeBuildDir()`, the installed archive in `CMakeInstallDir()`, and VMake publishes
a symlink at `BuildDir()/libfoo.a`. Separate paths avoid two build systems claiming
the same output. Targets with post-link steps copy the prebuilt archive before modifying it.
Void callbacks run once per vmake invocation and CMake performs its own incremental checks.

## Wrapping a Plain C Library (No CMake)

For a small C library with a single translation unit, declare a static target directly — no `TargetVoid` needed. `AddPublicIncludes("src", "@tinyexpr.h")` adds `src` to dependents' include path and records the trailing `@` argument as an include rule, so only matching headers are copied when public includes are installed:

```go
p.OnPackage(func(p *api.Package) {
    p.SetGit("https://github.com/codeplea/tinyexpr.git").
        AddVersion("1.0.0", "9907207e5def0fabdb60c443517b0d9e9d521393").
        SetDescription("Tiny expression evaluator for C").
        SetLicense("zlib")
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("tinyexpr").
        SetKind(api.TargetStatic).
        AddProvidedLibs("tinyexpr", "m").
        AddFiles("src/tinyexpr.c").             // local SetGit: SourceDir()-relative, src/ prefix
        AddPublicIncludes("src", "@tinyexpr.h") // public -Isrc; install copies only tinyexpr.h
})
```

The official registry wrapper for the same library resolves from the checkout root, so it drops the prefix: `AddFiles("tinyexpr.c").AddPublicIncludes(".", "@tinyexpr.h")`.

Library-style bool options follow the same rule. `*Package` embeds `ConfigAccessor`, so inside `SetBuildFunc` the resolved values are readable directly (the official cJSON/zlib wrappers do this):

```go
p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.Option("shared").
        SetType(api.OptionBool).
        SetDefault(false).
        SetDescription("Build shared library")
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("cjson").SetKind(api.TargetVoid).
        AddProvidedLibs("cjson").
        SetBuildFunc(func(p *api.Package) error {
            p.CMakeConfigure("-DBUILD_SHARED_LIBS=" + p.BoolStr("shared"))
            p.CMakeBuild()
            p.CMakeInstall()
            return nil
        })
})
```

`p.BoolStr("shared")` returns `"ON"`/`"OFF"`; `p.Bool`, `p.String`, and `p.Int` are available on `*Package` inside `SetBuildFunc` as well, because option values are resolved before `OnBuild` runs.

## Autotools Example

`p.Configure()` runs `<SrcDir>/configure` with the working directory set to `BuildDir()` (out-of-source build) and appends `--prefix=p.InstallDir()` and `--host=p.TargetTriple()`. `p.Make()` then starts with `-C <BuildDir>`, so the matching build is `p.Make()` / `p.Make("install")` — do NOT point make at `SrcDir()` after configure, or it will not find the generated Makefile:

```go
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("libfoo").
        SetKind(api.TargetVoid).
        SetBuildFunc(func(p *api.Package) error {
            if err := p.Configure("--disable-static", "--enable-shared"); err != nil {
                return err
            }
            if err := p.Make(); err != nil {
                return err
            }
            return p.Make("install")
        })
})
```

Projects that ship a Makefile in the source tree with no `configure` script build in-source instead: keep the implicit `-C <BuildDir>` and add a second, absolute `-C` (`make` processes them in order, and `SrcDir()` is absolute):

```go
if err := p.Make("-C", filepath.ToSlash(p.SrcDir())); err != nil {
    return err
}
return p.Make("-C", filepath.ToSlash(p.SrcDir()), "install")
```

Import `path/filepath` for these examples. `PREFIX=p.InstallDir()` only sets a real prefix for remote wrappers — see the note under Custom Build Logic.

## Custom Build Logic

For libraries that need non-standard build steps:

```go
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("customlib").
        SetKind(api.TargetVoid).
        SetBuildFunc(func(p *api.Package) error {
            if err := p.Make("-C", filepath.ToSlash(p.SrcDir())); err != nil {
                return err
            }
            return p.Make("-C", filepath.ToSlash(p.SrcDir()), "install", "PREFIX="+filepath.ToSlash(p.InstallDir()))
        })
})
```

`PREFIX=p.InstallDir()` yields an empty prefix for local packages: `InstallDir()` is only populated for remote packages. Local wrappers should stage into `BuildDir()` (for example `filepath.Join(p.BuildDir(), "_install")`) and declare artifacts with `SetPrebuilt`/`AddPublicIncludes`, or publish through install items.

`p.Run`/`p.RunIn` raise a script error on failure and return nothing; execution boundaries release resources — call them as statements and `return nil` at the end (use `p.RunEnv` if you need the error). `p.Make` starts with `-C <BuildDir>`; pass a second `-C` with the absolute `SrcDir()` when building in the source tree. Import `path/filepath` and normalize Make path arguments with `filepath.ToSlash`. The helper supplies the invocation jobs budget. CMake projects use `CMakeBuild` / `CMakeInstall` for their selected generator and build directory.

## External Incremental Builds

VMake calls each local or remote `TargetVoid` once per vmake invocation. Make/CMake decides which work is incremental. `SetConfigFiles` stores package configuration metadata; it does not skip callbacks or declare target inputs:

```go
p.OnPackage(func(p *api.Package) {
    p.SetConfigFiles(".config")
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("mylib").
        SetKind(api.TargetVoid).
        SetBuildFunc(func(p *api.Package) error {
            p.CMakeConfigure("-DBUILD_SHARED_LIBS=OFF")
            p.CMakeBuild()
            p.CMakeInstall()
            return nil
        })
})
```

Synchronous subgraphs and the main graph share completed target state. A later invocation calls the callback again, including after a failed build or a partially populated installation directory.

## KConfig for Firmware Wrappers

Firmware wrapper packages (U-Boot, kernel, etc.) combine KConfig preset management with void targets:

```go
p.OnPackage(func(p *api.Package) {
    p.SetGit("https://github.com/u-boot/u-boot.git").
        AddVersion("2024.04", "v2024.04").
        SetConfigFiles(".config")
})

p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.KConfig("u-boot").
        AddPreset("rk3568_defconfig").
        AddPreset("stm32_defconfig").
        AddPreset("sandbox_defconfig").
        SetDefaultPreset("sandbox_defconfig").
        SetSrcDir("src"). // local SetGit: read .config from SrcDir() = SourceDir()/src
        SetKConfigPatches(map[string]string{"CONFIG_FOO=y": "# CONFIG_FOO is not set"})
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("uboot").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
        srcDir := pkg.SrcDir()
        pkg.EnsureConfig(srcDir)
        const ownFlags = "ifndef VMAKE_FIRMWARE_FLAGS_RESET\nundefine CFLAGS\nundefine CXXFLAGS\nundefine LDFLAGS\nexport VMAKE_FIRMWARE_FLAGS_RESET := 1\nendif"
        if err := pkg.Make("-C", filepath.ToSlash(srcDir), "--eval", ownFlags); err != nil {
            return err
        }
        return nil
    })
})
```

`EnsureConfig(pkg.SrcDir())` generates `<SrcDir>/.config` from the selected preset (`make <preset>`) when it is missing or empty, and applies KConfig patches to it; `SetSrcDir("src")` tells `vmake config` where that file is for a local `SetGit` package. Registry packages resolve from the checkout root, so there `SetSrcDir` can be omitted (it defaults to `SourceDir()`). The reference fixture is `test_linux/17_firmware/busybox`.

## Key Points

- `p.Run` / `p.Make` default to the package's `BuildDir`; CMake helpers manage `CMakeBuildDir()` across all three stages
- `p.SrcDir()` — the downloaded source tree (use this for source files, config headers, patching); equals `SourceDir()/src` for local `SetGit` packages and `SourceDir()` for registry packages
- `p.SourceDir()` — package root; remote packages receive their writable working tree under `.vmake_deps/`
- `p.BuildDir()` — scratch directory for intermediate files
- `p.InstallDir()` — installation prefix for **remote** packages; it is empty for local packages, so `PREFIX=p.InstallDir()` / `--prefix` only works in remote wrappers (a custom `SetCMakeInstallDir` must still publish results into `InstallDir()`)
- `p.CMakeBuildDir()` / `p.CMakeInstallDir()` — actual CMake build tree and installation prefix
- Void callbacks run once per vmake invocation even with a nonempty `InstallDir`; Make/CMake performs incremental checks
- `OnPackage` with `SetGit`/`AddVersion` works for registry packages and for local packages that wrap a downloaded library. Native remote repos must NOT use them — their version comes from git tags and the resolver wires the checkout itself
- Path prefixes differ: local `SetGit` packages need `"src/"` prefixes for `AddFiles`/`AddIncludes`/`AddPublicIncludes` (sources live in `SourceDir()/src`); registry packages resolve from the checkout root with no prefix — see `references/dirs.md`
- Local packages can also use `OnPackage` for metadata (`SetDescription`, `SetLicense`, `SetHomepage`) — it runs for all packages

## Patching Source Before Build

Some libraries use header-based configuration (e.g., mbedtls 2.x) where CMake options don't cover all config flags. You can patch source files inside `SetBuildFunc` using Go's `os` and `strings` packages before calling build helpers:

```go
SetBuildFunc(func(p *api.Package) error {
    configPath := filepath.Join(p.SrcDir(), "include", "mbedtls", "config.h")
    raw, _ := os.ReadFile(configPath)
    raw = []byte(strings.Replace(string(raw),
        "//#define MBEDTLS_SSL_DTLS_SRTP\n",
        "#define MBEDTLS_SSL_DTLS_SRTP\n", 1))
    os.WriteFile(configPath, raw, 0644)
    p.CMakeConfigure("-DBUILD_SHARED_LIBS=OFF")
    p.CMakeBuild()
    p.CMakeInstall()
    return nil
})
```

## Post-Install Processing

After `CMakeInstall`, the installed layout may not match what consumers expect. For example, cJSON installs headers to `include/cjson/cJSON.h` but consumer code uses `#include <cJSON.h>`. Use `api.CopyFile` to adjust the layout:

```go
SetBuildFunc(func(p *api.Package) error {
    p.CMakeConfigure("-DBUILD_SHARED_LIBS=OFF")
    p.CMakeBuild()
    p.CMakeInstall()
    api.CopyFile(
        filepath.Join(p.CMakeInstallDir(), "include", "cjson", "cJSON.h"),
        filepath.Join(p.CMakeInstallDir(), "include", "cJSON.h"),
    )
    return nil
})
```

## Consuming This Package

Other projects use it via `OnRequire` + `AddDeps` (see `examples/with-package.md`):

```go
p.OnRequire(func(ctx *api.RequireContext) {
    ctx.AddRequires("myrepo/somelib >=1.0")
})
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("app").AddDeps("myrepo/somelib")
})
```

## Running / Verifying

- Wrapper package (local build.go): run `vmake build` from the wrapper's project directory. A `TargetVoid` wrapper succeeds when its `Configure`/`Make`/`CMake*` commands succeed — the run ends with `Build succeeded!`, and `vmake query targets` lists the declared targets.
- Consuming project: `vmake build --install` resolves and checks out the registry package, builds dependencies first, then links the app. `vmake query` prints the dependency tree; a successful build ends with `Build succeeded!` (plus `Install succeeded!` with `--install`).
- Installed layout: for a local library target, `vmake build --install --install-type sdk` publishes the archive/shared library under `install/lib/` and the forwarded public headers under `install/include/` (static libraries are skipped at install without `sdk`). Remote packages publish into their own `InstallDir()` under the project's `.vmake_deps/` tree.
- The first run needs network access unless the pinned commit is already recorded in `.vmake/vmake.lock` and materialized under the project's `.vmake_deps/`; registry repos must be added/trusted first (`vmake repo add`).

## See Also

- references/api.md - Package metadata setters, TargetVoid, SetBuildFunc
- references/dirs.md - SourceDir/SrcDir/BuildDir/InstallDir path rules
- examples/with-package.md - Consuming third-party packages
- references/gotchas.md — source patching and static-library linking
- examples/firmware.md — full multi-package KBuild flow (U-Boot, linux, rootfs)
- local vs registry vs native packages — `SKILL.md - Package Types`
- target kinds and helpers — `SKILL.md - Target API at a Glance`
- AddRequires/AddDeps wiring — `SKILL.md - Dependencies`
