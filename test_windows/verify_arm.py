import hashlib
import json
import os
from pathlib import Path
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
(project / ".vmake").mkdir()
(project / ".vmake" / "config.json").write_text(json.dumps({"global": {"toolchain": "arm-none-eabi"}}))


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


run("build", "--install", "--jobs", "2")
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
before = {p: p.stat().st_mtime_ns for p in elf.parent.rglob("*") if p.is_file() and p.suffix in (".o", ".elf", ".hex", ".bin")}
run("build")
assert all(p.stat().st_mtime_ns == stamp for p, stamp in before.items()), "unchanged build rewrote artifacts"
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
assert not list((project / "build").rglob("*.rsp")), "response files were not cleaned up"
assert "build --tests" in run("test", success=False)
run("rebuild")
assert digest(artifact("firmware.elf")) == expected["firmware.elf"]
run("distclean")
assert not any(p.is_file() for p in (project / "build").rglob("*"))
run("build")
assert digest(artifact("firmware.elf.bin")) == expected["firmware.elf.bin"]
print(f"ARM build and incremental checks passed: {project}")
