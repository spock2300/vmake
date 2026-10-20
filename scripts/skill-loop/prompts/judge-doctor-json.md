You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

Question being judged: does vmake have a JSON/machine-readable `vmake doctor` report, and what should the user run instead?

PASS only if the answer explicitly states that `vmake doctor` has no `--json` / no JSON output, and names the closest supported commands:
- `vmake inspect` for the JSON project model, and/or
- `vmake toolchain show <name>` for resolved tool paths, while `vmake doctor` remains human-readable.

FAIL if the answer recommends `vmake doctor --json`, claims such JSON output exists, or only says "not covered by the skill" without stating that doctor has no JSON output.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
