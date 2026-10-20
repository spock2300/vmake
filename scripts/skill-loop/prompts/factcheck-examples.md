You are a strict fact-checker for the shipped vmake skill. Do not edit any file.

Audit every file under `cmd/vmake/skills/vmake/examples/` against the repository code and live CLI. The code/CLI is the source of truth.

Check every factual claim and code block: API names/signatures, target kinds, paths, build/install/clean behavior, CLI commands/outputs, dependency/package semantics, CMake/Make/KConfig/symbol/post-link behavior, and platform-specific claims.

For each discrepancy, quote the file and claim, give the actual code/CLI behavior with the symbol/command (no line numbers), and classify wrong/stale/missing/ambiguous. Do not report style issues. Do not use outside knowledge.

End with exactly one of:
- `PASS: no discrepancies found`
- `FAIL: <N> discrepancies` followed by the numbered list.
