# Skill optimization loop

Automated multi-agent loop for improving `cmd/vmake/skills/vmake/`.
The controller is this script; the agents are separate `opencode run` sessions.

## Roles

| Role | Session | Can see | Produces |
| --- | --- | --- | --- |
| solver | fresh, temp project | installed skill + project | build result, commands, answer |
| judge | fresh, temp dir | installed skill + candidate answer | `PASS` / `FAIL: reason` |
| author | fresh, disposable repo copy | feedback + repo copy | skill-only patch |
| controller | this script | everything | score, accept/reject, rollback |

The solver is never given the author's reasoning or the source repository. The
author works on a disposable copy; the controller copies back only
`cmd/vmake/skills/vmake/` when the candidate improves the score.

## Reward

Tasks:

1. `compiler` - a toolchain is selected but its compiler is outside `PATH`.
   Pass requires a successful build, a runnable `hello`, the reported compiler
   path, and zero `PATH`/`CC`/`CXX`/`AR`/`LD` mutations.
2. `multi` - build a multi-target project and run its native tests.
   Pass requires succeeded build/test reports and zero PATH mutations.
3. `platform` - judge whether the skill states the vmake-specific Windows and
   macOS requirements without turning into a general installation tutorial.
4. `doctor_json` - judge whether the skill correctly says `vmake doctor` has no
   JSON output and points at `vmake inspect` / `vmake toolchain show`.
5. `install-manifest` - judge whether the skill gives a complete
   `toolchain.json` for a first-use HTTPS installation (`version`,
   `installations`, `method`, `file`, `root_dir`, `url`, `sha256`).
6. `onrequire` - two-phase `OnRequire` discovery and `ctx.When`/`If`/`Select`.
7. `public` - `AddPublic*` propagation versus global flags and BuildKey rotation.
8. `buildkey` - what rotates `build/<pkg>/<buildKey>/`, including action signatures.
9. `dryrun` - declarative writes in `OnBuild` and `p.DryRun()` guards.
10. `targets` - `TargetVoid/Object/Static/Shared/Binary`, `SetTest`, `SetLinkable(false)`.
11. `options` - choice options, `CONFIG_*`, `autoconf.h`, `ImportConfig`/`SyncConfigDefines`.
12. `prebuilt` - `SetPrebuilt`, `CMakeInstallDir`, public headers/links.
13. `symbols` - visibility, export lists, version scripts, ELF-only policy.
14. `subgraph` - `BuildSubGraph`, `DepOutput`, `DepBuildDir`.
15. `install` - `OnInstall`/`AddInstalls`, `SetInstall(false)`, `SetInstallDir`, manifest.
16. `cmake` - `CMakeConfigure/Build/Install`, configure fingerprint, toolchain use.
17. `firmware` - KConfig presets, `EnsureConfig`, post-link HEX/BIN/Size.
18. `packages` - version constraints, `SetGit`, lockfile, repo/extension trust.
19. `inspect-report` - inspect JSON, build/test reports, query commands.
20. `clean` - `clean`/`clean --all`/`distclean`, `OnClean`, preserved sources.
21. `mode` - debug/release/size flags, global flag ordering, BuildKey rotation.
22. `buildscope` - no target filter, `-p` prefix, `SetRoot(true)`.
23. `factcheck-skill` - audit SKILL.md against code/CLI.
24. `factcheck-api` - audit references/api.md signatures and behavior.
25. `factcheck-refs` - audit dirs/gotchas/inspect/build-result/platforms/toolchains.
26. `factcheck-examples` - audit every example against code/CLI.

The factcheck tasks run with repository access and return `PASS` or a
discrepancy list; that full list is passed to the author.

Score tuple: `(passes, -violations, -words)`.
- More passes wins.
- Equal passes: fewer PATH/source-repo violations wins.
- Equal passes and violations: fewer words wins.
- All tasks passing with zero violations stops the loop.
- A candidate that does not improve the tuple is reverted.

## Usage

```bash
scripts/skill-loop/run.sh
```

Environment variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `VMAKE_SKILL_MODEL` | `deepseek/deepseek-flash` | model for all agent sessions |
| `VMAKE_SKILL_ROUNDS` | `3` | maximum author rounds |
| `VMAKE_SKILL_AGENT_TIMEOUT` | `1800` | per-agent timeout in seconds |
| `VMAKE_SKILL_TASKS` | all tasks | space-separated subset, e.g. `onrequire public buildkey` |
| `VMAKE_SKILL_WORK` | `mktemp -d` | work directory |
| `VMAKE_SKILL_KEEP` | `0` | keep the work directory and logs |

Requirements: `opencode`, `jq`, `git`, `go`, and a host `gcc`/`g++`/`ar`/`ld`.

The script installs the current candidate skill into the real
`~/.claude/skills/vmake` and `~/.agents/skills/vmake`, registers a temporary
temporary `skill-loop-tc-<pid>` extension in `~/.vmake`, and removes the
extension on exit.

## Adding a task

Add a prompt under `prompts/`, a `task_<name>` function in `run.sh` that calls
`run_agent`, and add `<name>` to the task loop in `run_round`. A task must set
`TASK_PASS`, `TASK_REASON`, and `TASK_VIOLATIONS`.

Keep tasks behavior-based. Do not make the judge depend on a particular
sentence; judge whether the skill prevents the wrong action. Add held-out tasks
if you want a generalization signal rather than only the optimization target.

## Limits

See [COVERAGE.md](COVERAGE.md) for the section-by-section map and the
remaining gaps. This is a development harness, not part of `go test`. It spends model tokens and
can take minutes per round. The "optimal" result is relative to the task suite:
if a task is missing, the loop cannot discover the corresponding skill gap.
