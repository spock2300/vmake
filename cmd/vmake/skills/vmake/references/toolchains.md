# Toolchain Resolution and Compiler Paths

vmake selects one toolchain per build and resolves its tools itself. Do not add
compiler directories to `PATH`, and do not set `CC`/`CXX`/`AR`/`LD` in the shell
or in `build.go`. The only PATH entry vmake needs is the `vmake` binary itself.
Hand-run tools and host test scripts are different: they may need the toolchain
on PATH, so use the absolute paths from `p.Env()` or prepend it only for that
command.

## Resolution Order

1. `--toolchain <name>` on `build`/`test`/`doctor`/`check-symbols`/`inspect`/`query`.
2. The project `toolchain` option in the active configuration:
   `vmake config --set toolchain=<name>`.
3. The built-in `host` toolchain.

The selected name is reported by `vmake inspect` (`toolchainInfo`) and by
`build/vmake-build.json` (`toolchain`).

## Built-in host

The built-in `host` toolchain uses these executable names from `PATH`:

| Host | CC | CXX | AR | LD |
| --- | --- | --- | --- | --- |
| Linux | `gcc` | `g++` | `ar` | `ld` |
| macOS | `cc` | `c++` | `ar` | `ld` |
| Windows | `gcc` | `g++` | `ar` | `ld` |

A Linux machine with only clang does not satisfy the built-in `host`; declare a
toolchain that names the clang executables, or install gcc/g++. On macOS the
built-in `host` intentionally uses Apple clang (`cc`/`c++`) and leaves
`objcopy`/`objdump` unset.

## Extension toolchains

A toolchain is declared by a `toolchain.json` in an extension repository
subdirectory. It can also declare installable archives:

```json
{
  "name": "arm-gcc",
  "version": "12.2.0",
  "display_name": "ARM GCC 12.2.0",
  "prefix": "arm-none-eabi-",
  "target_os": "none",
  "target_triple": "arm-none-eabi",
  "tools": {
    "cc": "arm-none-eabi-gcc",
    "cxx": "arm-none-eabi-g++",
    "ar": "arm-none-eabi-ar",
    "ld": "arm-none-eabi-gcc"
  },
  "installations": {
    "linux/amd64": {
      "method": "http",
      "url": "https://example.com/arm-gcc-12.2.0-x86_64-linux.tar.xz",
      "file": "arm-gcc-12.2.0-x86_64-linux.tar.xz",
      "format": "tar.xz",
      "sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
      "root_dir": "arm-gcc-12.2.0"
    }
  }
}
```

The `url` and `sha256` values above are placeholders; use the archive's real
values.

`installations` is keyed by host `GOOS/GOARCH` (`linux/amd64`,
`darwin/arm64`, `windows/amd64`, ...). Each entry declares `method` (`"lfs"`
or `"http"`), the archive `file` (a literal filename, no wildcards), and
`root_dir`, the single directory name inside the archive that contains `bin`
(`.` for the archive root). `method: "lfs"` reads `assets/toolchains/<file>`
from the extension repository and pulls it through Git LFS when needed;
`method: "http"` additionally requires an absolute HTTPS `url` and the
archive's 64-character `sha256`, and downloads on first use. `format` is
optional (`tar.gz`, `tar.xz`, `tar.bz2`, `zip`; auto-detected when empty).
The top-level `version` is required when `installations` is present. Unknown
fields are rejected, including the legacy `host`/`install` fields and
`default_flags`.

Rules:

- `tools` values are full executable names. Absolute paths are used as-is.
- With an `installations` entry, vmake installs the archive under
  `~/.vmake/toolchains/<host-os>/<host-arch>/<name>/<version>/` and sets that
  directory as `InstallPath`. A manifest without an entry for the current host
  fails instead of falling back.
- When `InstallPath` is set, relative tool names resolve **only** under
  `<InstallPath>/bin`. There is no PATH fallback, so a typo fails instead of
  silently using a host compiler.
- `prefix` is for external build systems (`CROSS_COMPILE`); vmake never builds
  tool names by concatenating the prefix with `tools` values.
- `target_os` / `target_triple` are fallback defaults; explicit project values win.

`vmake toolchain list` shows declared toolchains and whether they are installed.
`vmake toolchain show <name>` prints the configured name, the resolved absolute
path, and whether it came from an absolute path, `InstallPath`, or `PATH`.

## Compiler outside PATH

If the compiler exists but is not on `PATH`, declare it instead of exporting
PATH. A local git repository with one `toolchain.json` is enough:

```bash
mkdir -p ~/my-toolchain/mytc
# Write ~/my-toolchain/mytc/toolchain.json with absolute tools.cc/cxx/ar/ld.
git -C ~/my-toolchain init
git -C ~/my-toolchain add .
git -C ~/my-toolchain -c user.email=you@example.com -c user.name=you commit -m "add my-gcc"

vmake -y ext add my-toolchain ~/my-toolchain
vmake build --toolchain my-gcc
```

To make it the project default:

```bash
vmake config --set toolchain=my-gcc
vmake build
```

Adding the compiler's directory to `PATH` does not select it: `host` still asks
for `gcc`/`g++`. For a cross compiler such as `arm-none-eabi-gcc`, a PATH export
would not change the configured tool names at all.

## Inspecting the selected tools

```bash
vmake toolchain list
vmake toolchain show host
vmake toolchain show my-gcc
vmake doctor --toolchain my-gcc
```

`vmake doctor` validates that every configured tool resolves. `vmake toolchain
show` prints the absolute path, which is the answer to "where is the compiler?".
`vmake inspect` also exposes `toolchainInfo` with `name`, `installPath`, `cc`,
`cxx`, `ar`, and `ld`.

`vmake toolchain show` does not install a manifest toolchain. If `Install Path`
is absent, the toolchain is not installed yet; relative names may resolve from
PATH and can be host tools rather than the archive's tools. Run a build with
`--toolchain <name>` to install it, then re-run `show` to see the install path.

## Inside build.go

| Need | Use |
| --- | --- |
| Configured tool name (may be relative) | `p.CC()`, `p.CXX()` |
| Resolved absolute executable | `p.Env()["CC"]`, `p.Env()["CXX"]` |
| Environment for an external command | `p.Env()` |
| CMake with the selected toolchain | `p.CMakeConfigure()`, `p.CMakeBuild()`, `p.CMakeInstall()` |
| Make with the selected toolchain | `p.Make(...)` |

`p.Env()` resolves `CC`/`CXX`/`AR`/`LD` and the optional binutils to absolute
paths and includes `CROSS_COMPILE`, `CFLAGS`, `CXXFLAGS`, `LDFLAGS`, `MAKE` (the
configured make name or `make`), and the optional
`STRIP`/`RANLIB`/`OBJCOPY`/`SIZE`/`OBJDUMP`/`NM` entries. It includes `PATH` when
the toolchain contributes directories (an installed toolchain or absolute
compilers); the built-in `host` leaves `PATH` to the inherited process
environment. When calling a build tool manually, pass `p.Env()` rather than
reconstructing paths from a prefix.

## Missing compiler decision tree

1. Run `vmake toolchain list` and `vmake doctor --toolchain <name>`.
2. If the toolchain is declared but not installed, run a build with
   `--toolchain <name>`; vmake installs a manifest toolchain on first use.
3. If the toolchain is not declared, add its extension repository
   (`vmake ext add` + `vmake ext trust`) or create a local `toolchain.json`.
4. If the compiler is at a known path, use a `toolchain.json` with absolute
   `tools` entries.
5. Only for the built-in `host` should you ensure `gcc`/`g++` are on PATH. For
   any other compiler, PATH is not the selection mechanism.

## Common errors

| Message | Meaning |
| --- | --- |
| `exec: "gcc": executable file not found in $PATH` | Built-in `host` is selected but gcc is not on PATH; install gcc or select another toolchain |
| `cc "arm-none-eabi-gcc": ... not found` | The toolchain's relative tool is missing under `<InstallPath>/bin`; install the toolchain or fix `toolchain.json` |
| `toolchain 'x' not found` | No declared toolchain with that name; check `vmake ext list` and trust |
| Build uses the wrong compiler | Check `vmake toolchain show <name>` and the effective `toolchain` option; `--toolchain` overrides it for one run |
