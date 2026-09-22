import argparse
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import tempfile
import time


def run(command, directory):
    result = subprocess.run(command, cwd=directory, text=True, encoding="utf-8",
                            errors="replace", stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, timeout=180)
    print(result.stdout, end="", flush=True)
    if result.returncode:
        raise AssertionError(f"exit {result.returncode}: {command}")
    return result.stdout


def check_archive(path, arm):
    data = path.read_bytes()
    assert data.startswith(b"!<arch>\n"), path
    offset = 8
    objects = 0
    while offset + 60 <= len(data):
        size = int(data[offset + 48:offset + 58])
        body = data[offset + 60:offset + 60 + size]
        if body.startswith(b"\x7fELF"):
            machine = struct.unpack_from("<H", body, 18)[0]
            assert machine == (40 if arm else 62), (path, machine)
            objects += 1
        elif body[:2] == b"\x64\x86":
            assert not arm and os.name == "nt", path
            objects += 1
        offset += 60 + size + size % 2
    assert objects == 3, (path, objects)


def verify(vmake, root, name, toolchain, generator, custom=False, preset=False,
           default_mode=False):
    project = root / name
    source = project / "source"
    source.mkdir(parents=True)
    (project / ".vmake").mkdir()
    arm = toolchain == "arm-none-eabi"
    if not default_mode:
        (project / ".vmake/config.json").write_text(json.dumps({
            "global": {"toolchain": toolchain, "mode": "debug"}
        }), encoding="utf-8")
    (project / "CMakeLists.txt").write_text('''cmake_minimum_required(VERSION 3.20)
project(vmake_probe LANGUAGES C CXX ASM)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)
add_library(probe STATIC source/probe.c source/probe.cpp source/probe.S)
target_include_directories(probe PRIVATE source)
install(TARGETS probe ARCHIVE DESTINATION lib)
install(FILES source/value.h DESTINATION include)
if(NOT PROBE_BARE_METAL)
  add_executable(probe_app source/main.c)
  target_link_libraries(probe_app PRIVATE probe)
  install(TARGETS probe_app RUNTIME DESTINATION bin)
  add_library(probe_module MODULE source/probe.c)
  target_include_directories(probe_module PRIVATE source)
  install(TARGETS probe_module LIBRARY DESTINATION lib)
endif()
''', encoding="utf-8")
    (source / "value.h").write_text("#define PROBE_VALUE 7\n", encoding="utf-8")
    (source / "probe.c").write_text('''#include "value.h"
#ifndef VMAKE_C_FLAG
#error missing C flag
#endif
#ifdef VMAKE_CXX_FLAG
#error unexpected CXX flag
#endif
int probe_value(void) { return PROBE_VALUE; }
''', encoding="utf-8")
    (source / "probe.cpp").write_text('''#ifndef VMAKE_CXX_FLAG
#error missing CXX flag
#endif
#ifdef VMAKE_C_FLAG
#error unexpected C flag
#endif
extern "C" int probe_cpp(void) { return 2; }
''', encoding="utf-8")
    (source / "probe.S").write_text('''#ifndef VMAKE_ASM_FLAG
#error missing ASM flag
#endif
.text
.globl probe_asm
probe_asm:
.byte 7
''', encoding="utf-8")
    (source / "main.c").write_text('''int probe_value(void);
int probe_cpp(void);
int main(void) { return probe_value() + probe_cpp() > 0 ? 0 : 1; }
''', encoding="utf-8")
    settings = "" if default_mode else 'p.SetCMakeBuildType("MinSizeRel")'
    if custom:
        settings += '\np.SetCMakeBuildDir("intermediate cmake").SetCMakeInstallDir("stage area")'
    flags = 'ctx.AddGlobalCFlags("-DVMAKE_C_FLAG=1")\nctx.AddGlobalCxxFlags("-DVMAKE_CXX_FLAG=1")'
    flags += '\nctx.AddGlobalLdFlags("-Wl,--gc-sections")'
    if default_mode:
        flags += '\nctx.GlobalMode()'
    if arm:
        flags += '\nctx.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("none")'
        flags += '\nctx.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString).SetDefault("arm-none-eabi")'
        flags += '\nctx.AddGlobalCFlags("-mcpu=cortex-m4", "-mthumb")\nctx.AddGlobalCxxFlags("-mcpu=cortex-m4", "-mthumb")'
    configure = ["--preset=probe"] if preset else ["-G", generator]
    if generator == "Ninja Multi-Config":
        configure += ["-DCMAKE_CONFIGURATION_TYPES=Debug;Release;MinSizeRel"]
    if arm:
        configure += ["-DPROBE_BARE_METAL=ON", "-DCMAKE_SYSTEM_PROCESSOR=arm"]
    if preset:
        (project / "CMakePresets.json").write_text(json.dumps({
            "version": 3,
            "configurePresets": [{"name": "probe", "generator": generator}]
        }), encoding="utf-8")
    configure_go = ", ".join(json.dumps(arg) for arg in configure)
    (project / "build.go").write_text('''package main
import (
    "encoding/json"
    "os"
    "path/filepath"
    "github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
    p.SetConfigFiles("source/value.h")
    p.OnConfig(func(ctx *api.ConfigContext) { @FLAGS@ })
    p.OnBuild(func(ctx *api.BuildContext) {
        @SETTINGS@
        ctx.Target("external").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
            pkg.CMakeConfigure(@CONFIGURE@,
                "-DCMAKE_ASM_FLAGS=" + pkg.MergedCFlags("-DVMAKE_ASM_FLAG=1"))
            pkg.CMakeBuild()
            pkg.CMakeInstall()
            data, err := json.Marshal(map[string]string{
                "build": pkg.CMakeBuildDir(), "install": pkg.CMakeInstallDir(),
                "package": pkg.BuildDir(),
            })
            if err != nil { return err }
            return os.WriteFile(filepath.Join(pkg.SourceDir(), "paths.json"), data, 0644)
        })
        ctx.Target("probe").SetKind(api.TargetStatic).
            SetPrebuilt(filepath.Join(p.CMakeInstallDir(), "lib", "libprobe.a")).AddDeps("external")
    })
}
'''.replace("@FLAGS@", flags).replace("@SETTINGS@", settings).replace("@CONFIGURE@", configure_go), encoding="utf-8")
    build_command = [vmake, "build", "--install", "--install-type", "sdk"]
    run(build_command, project)
    paths = {key: Path(value) for key, value in json.loads((project / "paths.json").read_text()).items()}
    build = paths["build"]
    install = paths["install"]
    package = paths["package"]
    assert build == package / ("intermediate cmake" if custom else "cmake"), paths
    assert install == package / ("stage area" if custom else "staging"), paths
    archive = install / "lib/libprobe.a"
    check_archive(archive, arm)
    published = package / "libprobe.a"
    assert published.is_symlink() and published.resolve() == archive.resolve(), published
    assert (install / "include/value.h").is_file()
    assert (project / "install/lib/libprobe.a").is_file()
    cache = (build / "CMakeCache.txt").read_text(encoding="utf-8")
    expected_type = "Debug" if default_mode else "MinSizeRel"
    assert (f"CMAKE_BUILD_TYPE:STRING={expected_type}" in cache
            or f"CMAKE_BUILD_TYPE:UNINITIALIZED={expected_type}" in cache), expected_type
    assert "CMAKE_LINKER:UNINITIALIZED=" not in cache
    assert "CMAKE_MODULE_LINKER_FLAGS:STRING=-Wl,--gc-sections" in cache
    commands = json.loads((build / "compile_commands.json").read_text(encoding="utf-8"))
    assert any("probe.S" in entry["file"] for entry in commands)
    assert all("-Werror " not in entry.get("command", "") for entry in commands)
    if not arm:
        run([str(install / "bin" / ("probe_app.exe" if os.name == "nt" else "probe_app"))], project)
    stamp = archive.stat().st_mtime_ns
    before = archive.read_bytes()
    run(build_command, project)
    assert archive.stat().st_mtime_ns == stamp, "unchanged CMake build rewrote archive"
    time.sleep(1.1)
    (source / "value.h").write_text("#define PROBE_VALUE 11\n", encoding="utf-8")
    run(build_command, project)
    assert archive.read_bytes() != before, "changed CMake input did not rebuild archive"
    assert "11" in (install / "include/value.h").read_text()
    print(f"CMake verification passed: {name}", flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("vmake")
    parser.add_argument("--arm", action="store_true")
    args = parser.parse_args()
    vmake = str(Path(args.vmake).resolve())
    for program in ("cmake", "ninja"):
        if shutil.which(program) is None:
            raise SystemExit(f"required program not found: {program}")
    root = Path(tempfile.mkdtemp(prefix="vmake CMake 中文 space "))
    verify(vmake, root, "native", "host", "Ninja")
    verify(vmake, root, "multi config", "host", "Ninja Multi-Config", custom=True)
    verify(vmake, root, "preset", "host", "Ninja", preset=True)
    verify(vmake, root, "default debug", "host", "Ninja", default_mode=True)
    if args.arm:
        verify(vmake, root, "ARM", "arm-none-eabi", "Ninja")
    print(f"All CMake checks passed: {root}")


if __name__ == "__main__":
    main()
