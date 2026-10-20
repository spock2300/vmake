You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- Declare Kconfig presets in `OnConfig` with `ctx.KConfig("...").AddPreset(...).SetDefaultPreset(...)`; presets are make target names, not complete `.config` files.
- Call `pkg.EnsureConfig(pkg.SrcDir())` before `pkg.Make(...)` inside the package's `TargetVoid` build function.
- Use post-link helpers such as `AddPostLinkHex()`, `AddPostLinkBin()`, and `AddPostLinkSize()` (or explicit `AddPostLink` with declared dependencies/outputs).
- Use a linker script via `SetLinkerScript(...)`/`UseDependencyLinkerScript()` and the appropriate `target_os`/`target_triple` and toolchain make program.

FAIL if the answer treats a preset as a `.config` file, omits `EnsureConfig`, or omits the post-link outputs.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
