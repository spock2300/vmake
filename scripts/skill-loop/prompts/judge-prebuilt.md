You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- Use `SetPrebuilt(path)` with a path under `p.CMakeInstallDir()` (for example `filepath.Join(p.CMakeInstallDir(), "lib/libfoo.a")`); no compilation and no `AddFiles` for the prebuilt library.
- Set the target kind to `TargetStatic`/`TargetShared` (or equivalent) and use `SetPrebuilt` instead of compiling.
- Expose headers with `AddPublicIncludes(filepath.Join(p.CMakeInstallDir(), "include"))` or an equivalent public include.
- Propagate system libraries to consumers with `AddPublicLinks(...)` when needed.

FAIL if the answer recompiles the library, uses `AddFiles` for it, or does not use `SetPrebuilt`/`CMakeInstallDir`.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
