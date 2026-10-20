# Platform Prerequisites

vmake resolves compilers and build tools through the selected toolchain; do not
export `PATH`, `CC`, `CXX`, `AR`, `LD`, or `MAKE`. See [toolchains.md](toolchains.md)
for resolution order and [dirs.md](dirs.md) for storage paths.

## Linux

- The built-in `host` toolchain uses `gcc`/`g++`/`ar`/`ld`, `strip`, `ranlib`,
  `objcopy`, `size`, `objdump`, and `nm` from `PATH`.
- A host with only clang does not satisfy `host`; declare a toolchain whose
  `tools.cc` and `tools.cxx` name the clang executables.

## macOS

- The built-in `host` toolchain uses `cc`/`c++`/`ar`/`ld`, `strip`, `ranlib`,
  `nm`, and `size`, and leaves `objcopy`/`objdump` unset. Post-link helpers that
  need `objcopy` (`AddPostLinkHex`, `AddPostLinkBin`, `SetSymbolPrefix`) require
  a selected toolchain that declares it.
- Kconfig, Linux kernel, and U-Boot builds need GNU Make 3.82+; select a
  toolchain whose `make` is `gmake`. `vmake doctor` prints the resolved make
  path and warns when the macOS Make is older than 3.82.

## Windows

- vmake storage requires real symbolic links. Enable Developer Mode or use an
  elevated shell; junctions are not supported.
- vmake locates the full Git for Windows userland and prepends it to its own
  process `PATH`. MinGit does not include that userland.
- Git for Windows supplies neither a C compiler nor make. Use MinGW-w64 for
  native Windows binaries and the ARM GNU Toolchain for bare-metal firmware;
  an ARM build does not need a native MinGW C compiler. MSVC is out of scope.
- On Windows, the CMake helpers default to Ninja when no generator is passed and
  `CMAKE_GENERATOR` is unset. `p.Make()`, Kconfig preset generation, and the
  default `menuconfig` use the selected toolchain's make program; an explicitly
  configured `make` is validated.

## Checking the host

`vmake doctor` checks symbolic-link support, the Git for Windows userland on
Windows, make, and the selected toolchain's configured tools. It does not check
CMake or Ninja. `vmake doctor --toolchain <name>` diagnoses a specific
toolchain, and `vmake toolchain show <name>` prints each configured tool and its
resolved absolute path.

## Cross-platform notes

- `target_os` controls artifact naming, linker flags, and CMake settings
  independently of the host OS; `target_triple` supplies the compiler target
  (`--host=` and the CMake compiler target). `host` maps to the host OS and
  `none` is bare metal; CMake also recognizes `freebsd`, `openbsd`, `netbsd`,
  and `android`. Other values are not aliased.
- `vmake test` refuses a cross target with
  `cannot run tests for project target (...) on <host>` and points to
  `vmake build --tests` to build the test targets without running them.
