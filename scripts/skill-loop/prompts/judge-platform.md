You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake requirements:
- Windows: real symbolic links are required (Developer Mode or elevated shell; no junctions); the full Git for Windows is required and MinGit is insufficient; the Git userland supplies neither a compiler nor make; MinGW-w64 is used for native Windows and the ARM GNU Toolchain for bare metal; MSVC is out of scope.
- macOS: `objcopy`/`objdump` are unset; Kconfig/kernel/U-Boot builds need GNU Make 3.82+ and a toolchain whose `make` is `gmake`. The scenario already has Xcode Command Line Tools, so restating `cc`/`c++` is optional; if the answer discusses the host compiler, it must say `cc`/`c++`.
- The answer does not include general installation tutorials (for example `xcode-select --install`, `brew install make`, "install Ninja and put it on PATH") as the fix. Saying "not covered by the skill" for general installation details is acceptable.

FAIL if any vmake requirement is missing or wrong, or if the answer recommends a general platform installation tutorial instead of vmake behavior.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
