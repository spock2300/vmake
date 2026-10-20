You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- `p.AddRequires("official/zlib >= 1.2.11")` (or another semver constraint) declares the dependency; `AddRequires` resolves/fetches but does not create target build edges.
- A local `SetGit(...)` package materializes under `.vmake_deps/local/<pkg>/src` and uses `SrcDir()` for the tree; `AddPatches`/`SetPatches` can patch it.
- Resolved versions/commits are pinned in `.vmake/vmake.lock`; `vmake lock update` explicitly re-resolves them.
- `vmake repo add <name> <url>` and `vmake repo trust <name>` (or the interactive/`-y` path) manage package repositories; `vmake ext add`/`ext trust` manage extension repositories.
- Remote buildscripts must be trusted before they run; an untrusted repository fails instead of executing.

FAIL if the answer says `AddRequires` creates target edges, omits the lockfile, or says remote scripts run without trust.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
