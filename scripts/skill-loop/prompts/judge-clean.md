You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- `vmake clean` runs `OnClean`, removes the active configuration's build outputs and `build/compile_commands.json`, and preserves `.vmake_deps/` source trees, `install/`, and other configurations' `BuildDir`s.
- `vmake clean --all` removes all configurations' build outputs and `build/compile_commands.json`, but keeps `install/`.
- `vmake distclean` also removes `install/`; `--purge-sources` additionally deletes `.vmake_deps/`. It keeps `.vmake/` configuration and lock files.
- `clean`, `distclean`, and `pkg clean` remove both `build/vmake-build.json` and `build/vmake-test.json`.
- Missing-toolchain cleanup: `clean --all`/`distclean` can proceed and skip toolchain-dependent `OnClean` with a warning.

FAIL if the answer says ordinary `clean` or `clean --all` deletes `install/`, downloaded sources, or other configurations' build dirs; says `distclean` deletes `.vmake/`; or omits `OnClean`.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
