You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- Register extra install entries with `p.OnInstall(func(ctx *api.InstallContext) { ctx.AddInstalls("include", "include") })` or `ctx.AddInstalls(...)` in `OnBuild`.
- Skip an internal target with `t.SetInstall(false)`.
- Change the target's install subdirectory with `t.SetInstallDir("lib/plugins")`.
- `install/manifest.json` records the vmake version, toolchain, mode, generation time, and package versions/refs. Installed artifact paths/sizes/hashes belong to `build/vmake-build.json`, not the manifest.
- The install phase runs after builds and reuses the invocation's `BuildContext`s without replaying `OnBuild`.

FAIL if the answer says install copies files automatically without an API, reruns `OnBuild`, omits the manifest fields, or claims installed artifacts are recorded in the manifest.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
