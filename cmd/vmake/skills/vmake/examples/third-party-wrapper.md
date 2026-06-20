# Third-Party Package Wrapper

Wrapping an external C/C++ library (CMake, Autotools, etc.) as a vmake package using `TargetVoid` and `SetBuildFunc`. This is the pattern used for **registry repo** packages.

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

- **`p.OnPackage`** — Package metadata phase (runs during plugin extraction, before any lifecycle phases)
- **`SetGit(urls...)`** — Git repository URLs for source download
- **`AddVersion(version, ref)`** — Map human-readable version to git ref (tag/commit)
- **`AddProvidedLibs(libs...)`** — Library names that consumers will link against
- **`api.TargetVoid`** — Target that doesn't produce a compilation artifact
- **`SetBuildFunc(fn)`** — Custom build function; receives `*Package`, returns `error`

## How It Works

1. **OnPackage**: Declares metadata so the dependency resolver can download and version-match the source
2. **OnBuild**: `TargetVoid` with `SetBuildFunc` runs the external build system
3. The `SetBuildFunc` callback receives a `*Package` with `SourceDir()`, `BuildDir()`, `InstallDir()` already set correctly
4. Built-in helpers (`CMakeConfigure`, `CMakeBuild`, `CMakeInstall`, `Configure`, `Make`) automatically use the right directories and prefix

## Autotools Example

```go
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("libfoo").
        SetKind(api.TargetVoid).
        SetBuildFunc(func(p *api.Package) error {
            p.Configure("--disable-static", "--enable-shared")
            p.Make()
            p.Make("install")
            return nil
        })
})
```

## Custom Build Logic

For libraries that need non-standard build steps:

```go
p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("customlib").
        SetKind(api.TargetVoid).
        SetBuildFunc(func(p *api.Package) error {
            if err := p.RunIn(p.SourceDir(), "make", "-j4"); err != nil {
                return err
            }
            if err := p.RunIn(p.SourceDir(), "make", "install", "PREFIX="+p.InstallDir()); err != nil {
                return err
            }
            return nil
        })
})
```

## Stamp-Based Skip with SetConfigFiles

For local packages using `TargetVoid` (no `InstallDir`), vmake uses a `.vmake_stamp` file in `BuildDir` to skip already-built targets. Use `SetConfigFiles` to declare files that invalidate the stamp:

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

The stamp becomes stale when config file content changes (SHA-256 hash comparison of registered files), the source git revision changes, or the stamp is deleted.

## KConfig for Firmware Wrappers

Firmware wrapper packages (U-Boot, kernel, etc.) combine KConfig preset management with void targets:

```go
p.OnPackage(func(p *api.Package) {
    p.SetConfigFiles(".config")
})

p.OnConfig(func(ctx *api.ConfigContext) {
    ctx.KConfig("u-boot").
        AddPreset("rk3568_defconfig").
        AddPreset("stm32_defconfig").
        SetDefault("sandbox_defconfig").
        PatchKConfig(map[string]string{"CONFIG_FOO": "y"})
})

p.OnBuild(func(ctx *api.BuildContext) {
    ctx.Target("uboot").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
        srcDir := pkg.SourceDir()
        pkg.EnsureConfig(srcDir)
        pkg.RunIn(srcDir, "make", "-j"+strconv.Itoa(runtime.NumCPU()))
        return nil
    })
})
```

## Key Points

- `SetBuildFunc` callback runs in the package's `BuildDir` context
- `p.SrcDir()` — the downloaded source tree (use this for source files, config headers, patching)
- `p.SourceDir()` — where the package's `build.go` lives (registry package metadata directory)
- `p.BuildDir()` — scratch directory for intermediate files
- `p.InstallDir()` — where headers/libs/binaries should be installed to
- Already-installed packages are automatically skipped (non-empty `InstallDir`)
- `OnPackage` with `SetGit`/`AddVersion` is ONLY for registry repo packages — native repo packages must NOT use these
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
        filepath.Join(p.InstallDir(), "include", "cjson", "cJSON.h"),
        filepath.Join(p.InstallDir(), "include", "cJSON.h"),
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

## See Also

- references/api.md - Package metadata setters, TargetVoid, SetBuildFunc
- examples/with-package.md - Consuming third-party packages
- SKILL.md - Registry repo vs Native repo
