You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- Options are declared in `OnConfig`; `SetDefault`/`SetValues` infer the option type, and a choice uses `SetValues(...)`.
- `ctx.GenerateConfigDefines()` emits `-DCONFIG_*` defines and `ctx.GenerateConfigHeader()` emits `generated/autoconf.h`, both called in `OnBuild`.
- A choice option emits both `CONFIG_<NAME>=<value>` and `CONFIG_<NAME>_<VALUE>=1`.
- `ctx.ImportConfig("pkg")` plus `GenerateConfigDefines()` (or `ctx.SyncConfigDefines("pkg")`) merges another package's options.
- Package options are not exported automatically; only global options auto-export `CONFIG_<UPPER_NAME>` defines.

FAIL if the answer declares options outside `OnConfig`, says package options are auto-exported, or omits the define/header generation.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
