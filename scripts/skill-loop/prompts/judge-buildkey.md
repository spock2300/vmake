You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

PASS only if the answer states all of these vmake facts:
- The BuildKey includes the format version, toolchain identity, build mode, options, and the global-flags hash.
- A local `SetGit` package additionally contributes its resolved source commit.
- A remote package additionally contributes source version, commit, and patch-set hash.
- Buildscript text is action-level: commands, includes, defines, and file lists are captured by action signatures, so a comment-only build.go edit does not rotate the BuildDir.
- Files a script writes into `BuildDir()` outside declared actions are not tracked.

FAIL if the answer says every build.go edit rotates the BuildDir, omits global flags, or omits the local/remote source material.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
