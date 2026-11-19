#!/usr/bin/env python3
"""Benchmark vmake's fixed per-build overhead and incremental checks.

The script generates throwaway projects in a temporary directory and reports
minimum wall times, so before/after comparisons are meaningful on any host:

* a void target that only pays vmake's own startup and tool resolution;
* the same target calling CMakeConfigure, CMakeBuild and CMakeInstall, one
  stage per fixture, to attribute the cost of each CMake helper call;
* bare ``cmake --build`` / ``cmake --install`` on the same build tree as the
  reference cost of the external work;
* a native static target with many translation units, cold and up to date;
* optionally large object files, which shows how much an incremental check
  spends re-reading artifacts.

Usage:
    python3 test_windows/bench_build.py ./vmake [--json] [--repeat 3]

The script is host-neutral; run it on Windows against a fresh ``vmake.exe`` and
record the JSON in ``docs/WINDOWS_VALIDATION.md``.
"""

import argparse
import json
import os
import platform
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path


def run(command, directory, env=None, timeout=900):
    started = time.perf_counter()
    result = subprocess.run(command, cwd=directory, env=env, text=True,
                            encoding="utf-8", errors="replace",
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                            timeout=timeout)
    elapsed = (time.perf_counter() - started) * 1000
    if result.returncode:
        raise AssertionError(f"exit {result.returncode}: {command}\n{result.stdout[-2000:]}")
    return elapsed, result.stdout


def best(command, directory, repeat, env=None):
    times = []
    for _ in range(repeat):
        elapsed, _ = run(command, directory, env)
        times.append(elapsed)
    return min(times)


def write(path, content):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def install_config(project):
    write(project / "configs/default.json",
          json.dumps({"global": {"toolchain": "host", "mode": "debug"}}))
    (project / ".vmake").mkdir(parents=True, exist_ok=True)


CMAKE_LISTS = """cmake_minimum_required(VERSION 3.20)
project(vmake_bench LANGUAGES C)
add_library(vmake_bench STATIC source/bench.c)
install(TARGETS vmake_bench ARCHIVE DESTINATION lib)
"""


def gen_cmake_project(root, name, stages):
    """stages is a list of package method calls executed by the void target."""
    project = root / name
    install_config(project)
    write(project / "CMakeLists.txt", CMAKE_LISTS)
    write(project / "source/bench.c", "int vmake_bench(void) { return 1; }\n")
    calls = "\n".join(f"            pkg.{stage}()" for stage in stages)
    write(project / "build.go", """package main

import (
    "encoding/json"
    "os"
    "path/filepath"

    "github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("external").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
@CALLS@
            data, err := json.Marshal(map[string]string{
                "build": pkg.CMakeBuildDir(), "install": pkg.CMakeInstallDir(),
            })
            if err != nil {
                return err
            }
            return os.WriteFile(filepath.Join(pkg.SourceDir(), "paths.json"), data, 0644)
        })
    })
}
""".replace("@CALLS@", calls))
    return project


def gen_native_project(root, name, tus, headers, object_mb):
    project = root / name
    install_config(project)
    for index in range(headers):
        write(project / "source" / f"h{index}.h",
              f"#ifndef H{index}\n#define H{index}\nstatic inline int f{index}(void) {{ return {index}; }}\n#endif\n")
    for index in range(tus):
        body = f"int t{index}(void);\n"
        body += "".join(f'#include "h{header}.h"\n' for header in range(headers))
        pad = ""
        if object_mb:
            pad = f"static char pad[{object_mb * 1024 * 1024}] = {{1}};\n"
        value = " + ".join(f"f{header}()" for header in range(headers)) or "0"
        body += f"{pad}int t{index}(void) {{ return {value}{' + pad[0]' if object_mb else ''}; }}\n"
        write(project / "source" / f"t{index}.c", body)
    write(project / "build.go", """package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("bench").SetKind(api.TargetStatic).
            AddFiles("source/*.c").AddIncludes("source")
    })
}
""")
    return project


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("vmake")
    parser.add_argument("--repeat", type=int, default=3)
    parser.add_argument("--tus", type=int, default=100)
    parser.add_argument("--headers", type=int, default=10)
    parser.add_argument("--object-mb", type=int, default=0,
                        help="size of a static array per TU; measures artifact re-reading")
    parser.add_argument("--json", action="store_true")
    parser.add_argument("--keep", action="store_true")
    args = parser.parse_args()

    vmake = str(Path(args.vmake).resolve())
    for program in ("cmake", "ninja"):
        if shutil.which(program) is None:
            raise SystemExit(f"required program not found: {program}")

    root = Path(tempfile.mkdtemp(prefix="vmake bench "))
    home = root / "home"
    home.mkdir()
    env = dict(os.environ)
    env["VMAKE_CACHE"] = str(root / "cache")
    for key in ("HOME", "USERPROFILE"):
        env[key] = str(home)

    def vmake_build(project):
        return [vmake, "build"], project

    try:
        stages = {
            "void_noop": [],
            "configure_noop": ["CMakeConfigure"],
            "build_noop": ["CMakeConfigure", "CMakeBuild"],
            "install_noop": ["CMakeConfigure", "CMakeBuild", "CMakeInstall"],
        }
        projects = {}
        for name, calls in stages.items():
            projects[name] = gen_cmake_project(root, name, calls)

        metrics = {}
        environment = {
            "platform": platform.platform(),
            "python": platform.python_version(),
            "cpus": os.cpu_count(),
            "vmake": vmake,
            "cmake": shutil.which("cmake"),
            "ninja": shutil.which("ninja"),
        }
        metrics["startup"] = best([vmake, "version"], root, args.repeat, env)
        environment["vmake_version"] = run([vmake, "version"], root, env)[1].strip().splitlines()[0]

        full = projects["install_noop"]
        cold_command, cold_dir = vmake_build(full)
        metrics["cmake_cold"] = run(cold_command, cold_dir, env)[0]

        for name in ("void_noop", "configure_noop", "build_noop", "install_noop"):
            project = projects[name]
            command, directory = vmake_build(project)
            run(command, directory, env)
            metrics[name] = best(command, directory, args.repeat, env)

        paths = json.loads((full / "paths.json").read_text(encoding="utf-8"))
        build_dir = Path(paths["build"])
        install_dir = Path(paths["install"])
        parallel = ["--parallel", str(os.cpu_count() or 1)]
        metrics["bare_build"] = best(["cmake", "--build", str(build_dir)] + parallel,
                                     full, args.repeat, env)
        metrics["bare_install"] = best(["cmake", "--install", str(build_dir),
                                        "--prefix", str(install_dir)],
                                       full, args.repeat, env)

        native = gen_native_project(root, "native", args.tus, args.headers, 0)
        metrics["native_cold"] = run([vmake, "build"], native, env)[0]
        metrics["native_noop"] = best([vmake, "build"], native, args.repeat, env)

        if args.object_mb:
            large = gen_native_project(root, "native_objects", args.tus,
                                       args.headers, args.object_mb)
            metrics["native_objects_cold"] = run([vmake, "build"], large, env)[0]
            metrics["native_objects_noop"] = best([vmake, "build"], large, args.repeat, env)

        if args.json:
            print(json.dumps({"environment": environment, "metrics": metrics},
                             indent=2, sort_keys=True))
        else:
            print(f"{environment['vmake_version']} on {environment['platform']}")
            width = max(len(name) for name in metrics)
            for name, value in metrics.items():
                print(f"{name:<{width}}  {value:8.1f} ms")
    finally:
        if args.keep:
            print(f"benchmark root kept: {root}", file=sys.stderr)
        else:
            shutil.rmtree(root, ignore_errors=True)


if __name__ == "__main__":
    main()
