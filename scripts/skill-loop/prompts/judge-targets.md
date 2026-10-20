You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- `TargetBinary` is an executable; `TargetStatic` an archive; `TargetShared` a shared library; `TargetObject` an object; `TargetVoid` is a side-effect/codegen/custom-command target that does not produce a normal linked artifact.
- A native test target is a `TargetBinary` with `SetTest(true)`; test targets are excluded from normal builds/install and run with `vmake test` (or built with `vmake build --tests` for cross targets).
- `SetLinkable(false)` on a `TargetObject` or `TargetStatic` exposes its output through `ctx.DepOutput` without auto-linking it into dependents.
- Optional incremental `TargetVoid` requires both `AddInputs` and `AddOutputs`.

FAIL if the answer confuses the target kinds, says `TargetVoid` produces a linked artifact, or omits `SetLinkable(false)`.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
