You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- `ctx.SetDefaultVisibilityHidden()` is package-local and adds hidden visibility flags.
- Export only the public list with `SetKeepGlobalSymbols("include/public.sym")` or `SetVersionScript("exports.map")`.
- At least one additional policy API is mentioned: `AddExcludeLibs`, `SetSymbolBinding`, or `SetSymbolPrefix`.
- These symbol policies are ELF-only; they fail on PE and are out of scope for Mach-O.
- `vmake check-symbols` audits ELF exports with `nm -D`/dynamic symbols and does not cover Mach-O.

FAIL if the answer claims the policies work on Windows/Mach-O, or names no export-list mechanism.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
