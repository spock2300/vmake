You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- `vmake inspect` prints one JSON project model (schemaVersion 1) with root/config/mode/toolchain, global values, roots, packages, targets, graph, compile database, build/test report paths, and diagnostics.
- `vmake query` prints the dependency tree; `vmake query targets` lists targets without building; `vmake query config <pkg>` shows effective option values and generated defines.
- `build/vmake-build.json` (schemaVersion 2) records status, duration, error, parsed diagnostics, summary, targets, tests, and installed artifacts; `vmake test` writes a separate `build/vmake-test.json`.
- `inspect`/`query` run `OnBuild` in dry-run mode with read-only `scriptfs` and do not materialize sources or execute builds.

FAIL if the answer recommends `vmake doctor --json`, says inspection executes the build or writes files, or confuses the build and test reports.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
