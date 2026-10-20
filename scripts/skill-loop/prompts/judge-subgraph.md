You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- Call `ctx.BuildSubGraph("tools")` (or the relevant package) inside `OnBuild`.
- The subgraph builds synchronously before the main target continues; the tool package uses its own toolchain/options.
- Obtain the tool output with `ctx.DepOutput("tools:codegen")` and the directory with `ctx.DepBuildDir("tools:codegen")` (or equivalent target references).
- Use the returned path in the main target's inputs/commands instead of assuming a fixed build path.

FAIL if the answer runs an external command without building the tool, or omits `BuildSubGraph`/`DepOutput`/`DepBuildDir`.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
