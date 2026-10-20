You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- Use `AddPublicIncludes` (or `AddPublicIncludes("include")`) and `AddPublicDefines` (or `AddPublicDefines("MYLIB_API=1")`) on the library target.
- Public flags/includes/defines propagate transitively to dependents along `AddDeps`.
- Workspace-wide `AddGlobal*` flags apply to every package and change the global-flags hash, rotating every package's `BuildDir`; prefer target public settings for library-specific needs.

FAIL if the answer uses `AddGlobal*` for this library-specific propagation, claims public settings only affect the target itself, or omits the BuildDir consequence.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
