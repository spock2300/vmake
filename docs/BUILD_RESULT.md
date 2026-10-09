# Structured build results

`vmake build` (including bare `vmake`, `build -iV`) and `vmake rebuild` atomically publish `build/vmake-build.json`. This is separate from the dependency version manifest in the install directory. Build scripts do not declare hardware or IDE configuration.

The version 1 JSON object contains:

| Field | Meaning |
| --- | --- |
| schemaVersion, buildId, root | Schema version, unique invocation ID, absolute project root |
| status | running, succeeded, failed, or cancelled |
| started, finished, error | UTC timestamps and failure text; finished is empty while running |
| configFile, configSha256 | Selected `.vmake` filename and SHA-256 of the exact bytes used to resolve configuration; SHA of empty bytes for the unsaved default |
| global, packages | Effective global and per-package configuration values |
| toolchain, mode | Effective main build toolchain and mode; package overrides remain in packages |
| compileDatabase | Absolute path to this build's compilation database, or empty if none was generated |
| targets | Executed targets: package, target, kind and publishable output paths, including declared post-link outputs |
| artifacts | Only files actually installed by this invocation, each with package, target, buildPath, installPath, size, sha256 |

Artifact paths are absolute, and every artifact has a nonempty installPath. An extra `AddInstalls` file may have an empty target. The artifact list comes exclusively from the invocation's installer records: installed main outputs, declared post-link outputs, headers and extra files are included. Directories contribute only files that passed the installer filter in this invocation; stale destination files are excluded. Source and installed bytes must have matching SHA-256 identities. The build context is reused without a second OnBuild invocation or build-key reconstruction. Incremental no-op builds with installation still publish a fresh build ID and verify installed file identities. Without `--install`, artifacts is an empty array even if a previous installation remains on disk; configuration, targets and the compilation database are still reported.

The main output of an `object` target is an intermediate link input. It is excluded from the target's published outputs, including prebuilt object targets. An object target with no declared post-link outputs therefore has an empty output list. Compilation and linking still use its object file normally. Other executed targets retain their output paths in targets for build metadata, but uninstalled outputs never become artifacts. Runtime installation skips static libraries, so their `.a` files do not appear in artifacts; `--install-type sdk` includes libraries that are actually installed. No extension-based display filter is needed by consumers.

A running record replaces the previous success before configuration/build work starts, under the normal project storage lock. Configuration identity is captured from the same bytes that were parsed, and rechecked after build/install. Success requires build, declared post-link outputs, installation and result collection to complete. Failure/cancellation clears artifacts. Panic unwinding cannot publish success before collection finishes. Termination before the running record reaches disk, or a failed first write, can leave the preceding report in place; buildId, configSha256 and artifact sha256 identify whether it belongs to the invocation being watched. clean/distclean invalidate the report before cleanup.

Consumers must require status=succeeded, matching project/configuration, and verify selected artifacts against sha256 before executing them. A report proves the outputs of a particular completed build; it does not assert that all source files are unchanged since that build. Keep buildId when correlating tasks, and do not infer freshness from installed directory contents.
