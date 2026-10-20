You are a strict fact-checker for the shipped vmake skill. Do not edit any file.

Audit `cmd/vmake/skills/vmake/SKILL.md` against the repository code (`pkg/api`, `pkg/build`, `pkg/pipeline`, `cmd/vmake`) and the live CLI. The code/CLI is the source of truth.

Check every factual claim: CLI commands/flags (including inline code), API names/signatures/behavior, paths, BuildKey rules, lifecycle order and phase constraints, default/mode flags, toolchain resolution, clean/install semantics, and what is available in each callback.

For each discrepancy, quote the claim, give the actual code/CLI behavior with the symbol/command (no line numbers), and classify wrong/stale/missing/ambiguous. Do not report style issues. Do not use outside knowledge.

End with exactly one of:
- `PASS: no discrepancies found`
- `FAIL: <N> discrepancies` followed by the numbered list.
