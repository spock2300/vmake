# Skill loop results

Pass-rate time series for `scripts/skill-loop/run.sh`. Append one section per
recorded run, newest last. Each section records the score tuple
(`passes/total`, PATH/source-repo violations, skill words) and the environment
it was measured in, because the score is only comparable across similar hosts
and models.

`docs/GOALS.md` M0 asks for the first row: `compiler`, `multi`, and
`inspect-report` from a clean home with only the installed skill.

## 2026-11-15 - M0 baseline

| field | value |
| --- | --- |
| host | Deepin 25, Linux 6.18.56 x86_64, gcc 12.3.0 |
| source | worktree at `5e4bc7c` plus uncommitted docs and duplicate-package scanner edits |
| vmake | `v1.0.88-0.20261115084228-5e4bc7cc3bb8+dirty`, built from `./cmd/vmake` |
| go | go1.27.2 linux/amd64 |
| agent | opencode v2.2.6, `deepseek/deepseek-flash` |
| tasks | `compiler multi inspect-report`; baseline round only (`VMAKE_SKILL_ROUNDS=0`) |
| clean room | `~/.vmake`, `~/.agents/skills`, and `~/.claude/skills` moved aside for the run; the solver's `vmake` was the freshly built binary on `PATH`; only the installed `vmake` skill was visible in the session |

| task | result | evidence |
| --- | --- | --- |
| `compiler` | PASS | build report `succeeded`; `hello` printed `Hello, VMake!`; answer named the resolved compiler `/tmp/m0-skill-loop/tc/bin/gcc` from `vmake toolchain show` and `compile_commands.json`; no `PATH`/`CC` changes |
| `multi` | PASS | build and test reports `succeeded`; 1/1 native test passed |
| `inspect-report` | FAIL | judge: the answer omitted that native remote resolution may materialize a source tree |

**Score: 2/3 passes, 0 violations, 43503 words.**

### Failure detail

`inspect-report` asked what `vmake inspect` outputs and what is read-only during
inspection. The answer said "inspect checks without fetching", which is the
general rule but not the whole rule: the missing-source check does not fetch,
while native remote resolution may still materialize a tree.

The skill does state the exception, but only in the preceding paragraph
([references/inspect.md:22](../../cmd/vmake/skills/vmake/references/inspect.md#L22)).
The `VMAKE_W_SOURCE_MISSING` bullet repeats the no-fetch claim without it
([references/inspect.md:98](../../cmd/vmake/skills/vmake/references/inspect.md#L98)),
and the solver generalized that bullet. Fixed in the next entry by repeating
the remote exception where the no-fetch claim is made.

### Run notes

- The clean room moves state aside instead of setting `HOME`: opencode reads
  credentials and global skill directories from the real user home, so an
  isolated `HOME` reports `Model unavailable` and still scans the real
  `~/.agents/skills`.
- When the caller captures the runner's stdout (a background job or a `tee`
  pipeline), each agent's timeout watchdog leaves its `sleep` child (reparented
  to init) holding the pipe after the round, so the caller blocks until those
  sleeps exit or are killed. This baseline was recorded by killing them after
  the score file was written; `run.sh` still needs the watchdog fix.

## 2026-11-15 - inspect.md clarification

Same host, agent, tasks, and clean-room method as the baseline above. One skill
edit: the `VMAKE_W_SOURCE_MISSING` bullet now repeats the native-remote
materialization exception at the point where the no-fetch claim is made
([references/inspect.md:98](../../cmd/vmake/skills/vmake/references/inspect.md#L98)),
instead of leaving it in the preceding paragraph only.

| task | result |
| --- | --- |
| `compiler` | PASS |
| `multi` | PASS |
| `inspect-report` | PASS - answer states "native remote resolution may materialize a tree" |

**Score: 3/3 passes, 0 violations, 43514 words** (+11 words from the baseline).


## 2026-11-15 - M1 skill and doctor changes

Same host, agent, tasks, and clean-room method. Delta since the previous entry:
`vmake skill install` records `.vmake-skill.json` and `vmake doctor` reports a
stale or unversioned copy, and the skill documents that check. The three
behavior tasks are unchanged and still pass.

| task | result |
| --- | --- |
| `compiler` | PASS |
| `multi` | PASS |
| `inspect-report` | PASS |

**Score: 3/3 passes, 0 violations, 43542 words** (+28 words from the previous entry).

## 2026-11-15 - Factcheck audit (M1 doc changes)

`factcheck-api` passed after two corrections: `PackageMeta` fields are
unexported (use `FullName()`), and `DepPackageDir` returns `""` in dry-run
without validating the dependency. `factcheck-skill` reported five
discrepancies in the first pass and four in the second; both lists were fixed
against the code:

- local duplicate basenames are an error, not skipped (DD-33)
- global-option macros are recomputed over the `FilterDeps` set, not kept
  unfiltered
- cross-package option reads also work in `OnConfigureDeps`
- `chip` merges by unioning values and filling one unset default; builtin
  options skip the values-equality check
- native packages select versions from upstream tags and fail with
  `no valid versions found` when none match
- an explicitly named linked package is rejected by `deps update`/`deps pin`;
  bare `deps update` keeps its lock entry

The final `factcheck-skill` re-run exceeded the agent timeout without producing
a result, and `factcheck-refs`/`factcheck-examples` did not complete in this
pass. Re-run all four before a release.

## 2026-11-15 - Factcheck audit (refs, examples)

`factcheck-refs` (6 findings) and `factcheck-examples` (5 findings) completed
against the post-fix docs; all were fixed against the code:

- `SetOnApply` can run twice per invocation: config phase for every package,
  then needed packages after `FilterDeps` resolves derived options
- `FilterDeps` merges resolved global option values into pass 2
- `globalDefines` covers only needed packages
- an incremental void target reports `built` and counts in `callbacks` even when
  its callback is skipped as up to date
- only `host` is a built-in toolchain; `make`/`cmake` resolve through the
  selected toolchain and fall back to PATH; `deps update` skips toolchain
  resolution while `deps graph|why|check` do not
- examples: `OnConfigureDeps` cross-package reads, linker-script resolution
  (own package first, same-depth ambiguity is an error), `vmake repo list`
  prints `(registry, N packages)`, and `vmake query` prints versions only for
  native packages

`TestSkill*` passes and the reinstalled skill still matches `vmake doctor`.

## 2026-11-15 - Release-gate regression (skill changes)

`factcheck-skill` completed with one finding: native-package version failures
were conflated (`no valid versions found` versus
`no version matching '<constraint>' in <available>`); SKILL.md now states both.

Three runs of `compiler`, `multi`, `inspect-report` followed: 2/3, 2/3, 3/3
(43822/43824/43841 words). Both 2/3 runs failed `inspect-report` with different
missing rubric facts - first `vmake query config` outputting generated defines,
then `query`/`query targets` running `OnBuild` in dry-run and the
native-remote materialization caveat. The facts exist in the skill; this is a
recall-completeness variance in a broad knowledge question, not missing
documentation. After making the `query config` output and the materialization
caveat explicit in SKILL.md, the third run passed 3/3.

Run `inspect-report` more than once before treating the release gate as green.

## 2026-11-15 - Factcheck round 2

Full re-audit of all four documents after the fixes recorded above:

| task | pass 1 | pass 2 | pass 3 |
| --- | --- | --- | --- |
| `factcheck-skill` | FAIL 5 | FAIL 1 | PASS |
| `factcheck-api` | FAIL 2 | FAIL 2 | PASS |
| `factcheck-refs` | FAIL 6 | FAIL 6 | FAIL 4 |
| `factcheck-examples` | FAIL 5 | FAIL 4 | FAIL 1 |

Every reported item was fixed against the code. `refs` and `examples` were not
re-audited after their pass-3 fixes. The recurring classes: `chip` and
`SetExportDefine` merge rules, global option values in `FilterDeps`,
cross-package reads in `OnConfigureDeps`, `buildDir`/`buildKey` readiness (absent
tree or unresolved remote version), report/test-summary wording, and
`make`/`cmake`/`git` toolchain resolution.

Final behavior run: 3/3 passes, 0 violations, 44039 words.

## 2026-11-15 - Factcheck round 3 (refs, examples)

`factcheck-refs` (pass 4) reported 6 more items and `factcheck-examples` 2; all
were fixed against the code:

- the Windows CMake default `-G Ninja` is skipped when the arguments contain
  `--preset`
- `git` resolves from the process PATH, not the toolchain-augmented PATH;
  `cmake` uses the latter and is not a `toolchain.json` field
- `tests[]`/`testsFound` count validated, started targets (an interrupted run
  omits the rest), and test success also covers "no runnable test target"
- own-Git native members: readiness checks the member directory, so
  `buildDir`/`buildKey` can exist while `sourcePresent` is false; the
  missing-source warning currently reports the parent URL/version
- a custom install prefix needs `.vmake-install` and must be outside the project
  root; the prebuilt symlink target is absolute

Final behavior run: 3/3 passes, 0 violations, 44222 words. `refs` and `examples`
were not re-audited after these fixes.

## 2026-11-15 - Factcheck round 4 (refs, examples)

Two more audit passes each; every finding was fixed against the code:

- refs pass 5: linked packages use their link path, and an own-Git native member
  under a linked parent uses `.vmake_deps/<owner>/third_party/<member>/src`
- examples pass 5: per-package `SetPrefix` is never validated or cleared; the
  marker/project-root checks apply only to the CLI `--prefix`
- refs pass 6: dry-run `OnBuild` output to `os.Stderr` bypasses the wrapper;
  `summary` is absent whenever `complete`/`partial` did not run (including a
  report-finalization failure, not only installation); `inspect` and normal
  `clean` also fail on tool version/hash probe errors, and `clean --all` skips
  toolchain resolution
- examples pass 6: only a non-default custom prefix gets `.vmake-install`; the
  default `<project>/install` is not marked

Final behavior run: 3/3 passes, 0 violations, 44306 words. The audit loop stops
here; `refs` and `examples` were not re-audited after the pass-6 fixes.
