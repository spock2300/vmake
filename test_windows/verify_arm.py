import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import sys
import tempfile


vmake = sys.argv[1:]
if not vmake:
    raise SystemExit("usage: python verify_arm.py [wine] /path/to/vmake[.exe]")
fixture = Path(__file__).resolve().parent / "arm_firmware"
root = Path(os.environ.get("VMAKE_WINDOWS_TEST_ROOT") or tempfile.mkdtemp(prefix="vmake ARM space "))
project = root / "firmware project"
shutil.copytree(fixture, project)
(project / "configs").mkdir()
(project / "configs" / "default.json").write_text(json.dumps({"global": {"toolchain": "arm-none-eabi"}}), encoding="utf-8")


def run(*args, success=True):
    with tempfile.TemporaryFile(mode="w+", encoding="utf-8") as log:
        result = subprocess.run(vmake + list(args), cwd=project, text=True, stdout=log, stderr=subprocess.STDOUT, timeout=180)
        log.seek(0)
        output = log.read()
    print(output, end="", flush=True)
    if (result.returncode == 0) != success:
        raise AssertionError(f"unexpected exit {result.returncode}: {args}")
    return output


def artifact(name):
    paths = list((project / "build").glob("*/" + name))
    assert len(paths) == 1, (name, paths)
    return paths[0]


def edit(name):
    path = project / name
    path.write_bytes(path.read_bytes() + b"\n")


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def response_paths(output, source):
    return [match.group(1) for line in output.splitlines() if source in line
            for match in [re.search(r"@(\S+\.rsp)", line)] if match]


verbose = run("build", "--install", "--jobs", "2", "-V")
run("doctor")
elf = artifact("firmware.elf")
header = elf.read_bytes()[:52]
assert header[:4] == b"\x7fELF" and header[4] == 1 and header[5] == 1
assert struct.unpack_from("<H", header, 18)[0] == 40
baseline = json.loads(fixture.with_name("arm_expected.json").read_text())
assert struct.unpack_from("<I", header, 24)[0] == baseline["entry"]
assert artifact("firmware.elf.hex").read_text().startswith(":")
assert artifact("firmware.elf.bin").stat().st_size > 0
expected = {name: digest(artifact(name)) for name in ("firmware.elf", "firmware.elf.hex", "firmware.elf.bin")}
for name, expected_hash in baseline["sha256"].items():
    assert expected[name] == expected_hash, (name, expected[name])

firmware_rsp = set()
for source in ("src/main.c", "src/raw.s", "src/startup.S"):
    paths = response_paths(verbose, source)
    assert len(paths) == 1, (source, paths)
    firmware_rsp.add(paths[0])
assert len(firmware_rsp) == 1, firmware_rsp
support_rsp = response_paths(verbose, "src/support.cpp")
assert len(support_rsp) == 1, support_rsp
assert support_rsp[0] not in firmware_rsp, (support_rsp, firmware_rsp)
cached = list((project / "build").rglob("state/rsp/*.rsp"))
assert cached, "expected cached response files"
assert not list((project / "build").rglob("vmake-*.rsp")), "temporary response files must not be used"
assert all(path.parent.name == "rsp" for path in (project / "build").rglob("*.rsp")), "response files outside state/rsp"

before = {p: p.stat().st_mtime_ns for p in elf.parent.rglob("*") if p.is_file() and p.suffix in (".o", ".elf", ".hex", ".bin")}
cached_before = {p: p.stat().st_mtime_ns for p in cached}
run("build")
assert all(p.stat().st_mtime_ns == stamp for p, stamp in before.items()), "unchanged build rewrote artifacts"
assert {p: p.stat().st_mtime_ns for p in cached} == cached_before, "unchanged build rewrote response files"
edit("include/value.h")
run("build")
assert {p: p.stat().st_mtime_ns for p in cached} == cached_before, "incremental build rewrote response files"
for dependency, sources in (("include/value.h", ("main.c", "startup.S")), ("include/raw.inc", ("raw.s",)), ("include/startup.inc", ("startup.S",))):
    edit(dependency)
    output = run("build")
    for source in sources:
        assert any("CC " in line and source in line for line in output.splitlines()), (dependency, source)
edit("board.ld")
assert "LINK firmware.elf" in run("build")
for name in ("firmware.elf.hex", "firmware.elf.bin"):
    artifact(name).unlink()
    assert "LINK firmware.elf" in run("build")
    assert digest(artifact(name)) == expected[name]
commands = json.loads((project / "build" / "compile_commands.json").read_text())
assert len(commands) == 4
assert max(sum(len(arg) + 1 for arg in item["arguments"]) for item in commands) > 32767
assert all(isinstance(item.get("arguments"), list) and not any(arg.startswith("@") for arg in item["arguments"]) for item in commands)
assert not list((project / "build").rglob("vmake-*.rsp")), "temporary response files must not be used"
assert "build --tests" in run("test", success=False)
run("rebuild")
assert digest(artifact("firmware.elf")) == expected["firmware.elf"]
run("distclean")
assert not any(p.is_file() for p in (project / "build").rglob("*"))
run("build")
assert digest(artifact("firmware.elf.bin")) == expected["firmware.elf.bin"]
print(f"ARM build and incremental checks passed: {project}")
