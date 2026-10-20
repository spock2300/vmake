You are a strict fact-checker for the shipped vmake skill. Do not edit any file.

Audit these references against the repository code and live CLI:
- `cmd/vmake/skills/vmake/references/dirs.md`
- `references/gotchas.md`
- `references/inspect.md`
- `references/build-result.md`
- `references/platforms.md`
- `references/toolchains.md`

The code/CLI is the source of truth. Check paths, BuildKey inputs/rotation, source/materialization behavior, JSON schemas and omission rules, report fields/counts, toolchain resolution/host requirements, platform requirements, and every command example.

For each discrepancy, quote the claim, give the actual code/CLI behavior with the symbol/command (no line numbers), and classify wrong/stale/missing/ambiguous. Do not report style issues. Do not use outside knowledge.

End with exactly one of:
- `PASS: no discrepancies found`
- `FAIL: <N> discrepancies` followed by the numbered list.
