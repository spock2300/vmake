import hashlib
import json
from pathlib import Path
import shutil
import tempfile
import urllib.request


def setup_toolchain(destination):
    manifest = Path(__file__).with_name("arm_toolchain.json")
    definition = json.loads(manifest.read_text(encoding="utf-8"))
    installation = definition["installations"]["windows/amd64"]
    with tempfile.TemporaryDirectory(prefix="vmake-arm-download-") as temporary:
        archive = Path(temporary) / installation["file"]
        with urllib.request.urlopen(installation["url"], timeout=60) as response, archive.open("wb") as output:
            shutil.copyfileobj(response, output)
        with archive.open("rb") as source:
            actual = hashlib.file_digest(source, "sha256").hexdigest()
        if actual != installation["sha256"]:
            raise RuntimeError(f"ARM archive SHA256 mismatch: {actual}")
        assets = destination / "assets" / "toolchains"
        assets.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(archive, assets / installation["file"])
    toolchain = destination / "arm-none-eabi"
    toolchain.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(manifest, toolchain / "toolchain.json")
    plugin = destination / "tools"
    source = plugin / "src"
    source.mkdir(parents=True, exist_ok=True)
    (plugin / "plugin.json").write_text(json.dumps({"name": "tools", "entry": "src/main.go", "enabled": True}), encoding="utf-8")
    (source / "main.go").write_text(
        'package main\n\nimport "github.com/spock2300/vmake/pkg/plugin"\n\n'
        'func Main(ctx *plugin.Context) { ctx.RegisterToolchainsFromRepo() }\n',
        encoding="utf-8",
    )
    print(f"Windows ARM toolchain plugin ready: {destination}")


if __name__ == "__main__":
    setup_toolchain(Path.home() / ".vmake" / "extensions" / "ci-arm")
