# Skill coverage map

This map answers "do the optimization targets cover the whole skill?".
There are three levels:

- **static** - deterministic test over the shipped Markdown/Go/CLI text.
- **behavior** - a solver task with a judge or a build assertion.
- **gap** - not exercised yet.

Static coverage is broad: every Go block is parsed and method-checked, every
fenced `vmake` command is checked against the Cobra tree, links and removed
flags are checked, and `api.*` symbols are checked.

Behavior coverage is cluster-based, not sentence-by-sentence. The tasks are
listed in `README.md`; a task can cover several sections.

## SKILL.md

| Section | Covered by | Level |
| --- | --- | --- |
| 0. Before You Run vmake | compiler, public, buildkey, dryrun | behavior |
| Quick Commands | TestSkillCLIExamples, TestSkillRemovedCLIFlags, TestSkillCLISnapshot | static |
| Toolchains & Compiler Discovery | compiler, install-manifest | behavior |
| Platform Prerequisites | platform | behavior |
| Build Script Structure | TestSkillGoBlocksParse, TestSkillMethodReferences, TestSkillAPISymbols | static |
| Directory Structure | buildkey | behavior |
| Lifecycle & Build Order | onrequire, options, install, clean | partial |
| Package Types | packages | partial |
| Target API at a Glance | targets, prebuilt, symbols, dryrun | behavior |
| Prebuilt Libraries | prebuilt | behavior |
| Symbol Management | symbols | behavior |
| Test Targets | multi, targets | behavior |
| Dependencies | onrequire, packages | behavior |
| Sub-Packages | packages | partial |
| Version Constraints | packages | behavior |
| OnRequire Two-Phase Execution | onrequire | behavior |
| Option & Conditional | options | behavior |
| Multiple Project Configurations | - | gap |
| Generated Defines / OptionChoice | options | behavior |
| Global Flags & Mode Flags | mode, buildkey | behavior |
| GlobalOption Cross-Package Consistency | options, mode | partial |
| Default Build Flags | mode, buildkey | partial |
| Cross-Compiling | compiler, platform, firmware | behavior |
| RTOS / Embedded | firmware | behavior |
| KConfig Preset Management | firmware | behavior |
| Install | install | behavior |
| OnInstall Lifecycle | install | behavior |
| Multi-File Build Scripts | TestSkillGoBlocksParse, TestSkillMethodReferences | static |
| Wrapping Third-Party Libraries | cmake, prebuilt | behavior |
| Build Scope | buildscope | behavior |
| Clean | clean | behavior |
| Repository & Toolchain Commands | packages, install-manifest | behavior |
| Common Mistakes | onrequire, public, dryrun, static tests | partial |
| Checklist | all behavior tasks | partial |

## References

| File | Covered by | Level |
| --- | --- | --- |
| api.md | TestSkillAPISymbols, TestSkillMethodReferences; behavior subset | static + partial behavior |
| build-result.md | multi, inspect-report | behavior |
| cli.md | TestSkillCLISnapshot, TestSkillCLIExamples | static |
| dirs.md | buildkey; path accessors partial | partial |
| gotchas.md | onrequire, public, dryrun; patching and `[]string` gaps | partial |
| inspect.md | inspect-report | behavior |
| platforms.md | platform | behavior |
| toolchains.md | compiler, install-manifest, platform | behavior |

## Examples

| Example | Covered by | Level |
| --- | --- | --- |
| simple.md | compiler, multi | behavior |
| config.md | options | partial |
| conditional.md | onrequire, options | partial |
| config-propagate.md | options | partial |
| config-to-define.md | options | partial |
| complete.md | composite of many tasks | partial |
| embedded-rtos.md | firmware | behavior |
| firmware.md | firmware | behavior |
| multi-module.md | - | gap |
| multi-target.md | multi, targets | behavior |
| on-clean.md | clean | behavior |
| on-install.md | install | behavior |
| prebuilt.md | prebuilt | behavior |
| simple.md | compiler, multi | behavior |
| subbuild.md | subgraph | behavior |
| symbol-management.md | symbols | behavior |
| third-party-wrapper.md | cmake, prebuilt | behavior |
| with-package.md | packages | behavior |

## Remaining gaps

These are not covered behaviorally yet:

- Multiple named configurations (`config copy/use`, `.vmake/project.json`) and
  per-configuration build keys.
- Multi-module workspace roots, local package dependency graphs, and `SetRoot`.
- `AddPatches`/`SetPatches` source patching, including the `[]string` spread
  gotcha.
- Full lifecycle callback ordering across `OnPackage`/`OnConfig`/`OnBuild`/
  `OnInstall`/`OnClean`.
- Actual `check-symbols`, manifest, cache, and lockfile command behavior.
- Windows/macOS builds (the loop runs on the host; platform knowledge is
  covered, native execution is not).
- Every example file as a standalone fixture; the examples are covered by
  clusters, not one task per file.

Adding a task for one of these is a prompt plus a rubric in this directory and
a `VMAKE_SKILL_TASKS` entry. Do not claim full semantic coverage without doing
that.
