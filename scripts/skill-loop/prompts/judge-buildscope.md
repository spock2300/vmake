You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- There is no package/target filter flag on `vmake build` or `vmake test`.
- `-p`/`--prefix` is the installation prefix, not a package selector.
- `SetRoot(true)` on one local package makes it the explicit entry point; roots are otherwise inferred from the local package graph.
- `vmake -C <dir>` selects the project root; building all needed local packages is the default.

FAIL if the answer claims `-p <pkg>` or `-t <target>` scopes the build, or invents a filter flag.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
