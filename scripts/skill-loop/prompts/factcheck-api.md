You are a strict fact-checker for the shipped vmake skill. Do not edit any file.

Audit `cmd/vmake/skills/vmake/references/api.md` against `pkg/api/*.go` (and `pkg/toolchain` only where the skill exposes toolchain types). The code is the source of truth.

Check every method name and signature, type, constant, constructor/utility function, behavior claim, and code block. Verify signatures including variadic element types and return values.

For each discrepancy, quote the claim, give the actual code symbol/signature or behavior (no line numbers), and classify wrong/stale/missing/ambiguous. Do not report style issues. Do not use outside knowledge.

End with exactly one of:
- `PASS: no discrepancies found`
- `FAIL: <N> discrepancies` followed by the numbered list.
