You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- Use declarative helpers such as `p.WriteBuildFile`, `p.GenerateFile`, `p.Generate`, `p.BuildPath`, or `t.AddBinHeader` (at least two).
- They defer writes to scheduler execution and emit `$builddir/...` when `BuildDir()` is empty, so dry-run inspection works.
- `inspect` (without `--no-targets`)/`query`/`query targets` run `OnBuild` with read-only `scriptfs` and `p.DryRun() == true`; `inspect --no-targets` skips `OnBuild`; `check-symbols` uses dry-run declaration but does not set `scriptfs` read-only; `query config` does not run `OnBuild`.
- A procedural `os.WriteFile` must be guarded with `if !p.DryRun()`.

FAIL if the answer recommends unconditional `os.WriteFile`, says inspection executes the writes, or names no declarative helper.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
