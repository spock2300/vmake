You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- `debug` injects `-O0 -g`; `release` injects `-O2 -DNDEBUG`; `size` injects `-Os -DNDEBUG`.
- Flags are ordered target → mode → global: mode flags are appended after target flags and before global flags, so a global `-O3` overrides the mode's `-O2`; `AddGlobalCFlags("-g3")` can keep release optimization and add debug info.
- Board-specific flags use `SetOnApply` with `AddGlobalDefines`/`AddGlobalCFlags`/`AddGlobalCxxFlags`/`AddGlobalLdFlags`/`AddGlobalLinks`; these are buffered per package and applied only to packages that survive `FilterDeps`.
- Global flag changes rotate the BuildKey and therefore every package's `BuildDir`; use target/public flags for library-specific settings.

FAIL if the answer says global flags are applied before mode flags, mode flags are applied after global flags, or global flags do not affect the BuildKey.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
