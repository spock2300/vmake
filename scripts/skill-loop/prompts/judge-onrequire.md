You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- `OnRequire` runs in two phases: discovery before config values exist, then `FilterDeps` after `OnConfig`.
- Direct `ctx.Bool`/`ctx.String`/`ctx.Int` reads are fatal in the discovery pass.
- Use a discovery-safe helper such as `ctx.When`, `ctx.WhenNot`, `ctx.WhenAny`, `ctx.If`, or `ctx.Select`, and add the dependency with `ctx.AddRequires`.
- The example gates the dependency on `enable_tls` being true (or equivalent).

FAIL if the answer recommends a direct option read in discovery, or gives no conditional-dependency helper.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
