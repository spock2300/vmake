---
status: accepted
---

# Separate Module Containers from Packages

VMake 1.x treated a native Git repository (`hl/hl-mcu-sdk`) as both a versioned source container and a dependency-graph node, while representing nested `build.go` files through a side-table `SubParents` map and context-sensitive short-name heuristics. In VMake 2.0, we separate **module containers** (versioning, trust, checkout, and container-level linking) from **packages** (build targets, options, and dependency edges), move module/source version constraints into `vmake.json`, adopt path-as-identity (`//<relpath>` locally, `<repo>/<module>[/<relpath>]` remotely) with O(1) direct loading, and treat `SetGit` upstream sources orthogonally across local, registry, and native packages.

## Context

In 1.x, conflating source containers with packages forced consumers of multi-package SDKs to declare phantom dependencies on empty root packages (`AddRequires("hl/hl-mcu-sdk")`) in strict list order, required bolt-on checks (DD-7), leaked `SubParents` across 15+ call sites, broke `PackageNode.SourceKind()` / `IsNative()` for lazy subpackages, exempted own-Git subpackages from `vmake.lock` / `--locked` / `install/manifest.json`, and caused `vmake config` to synthesize duplicate `IsGroup` directory nodes that desynchronized the tree and detail panels.

## Decisions

1. **Module root manifest (`vmake.json`)**: Extends the existing root `vmake.json` (retaining `"script"`). Declares `"roots"` (default `["//"]` when omitted on single-package root projects, replacing script-side `p.SetRoot(true)`), `"modules"` (direct external container dependencies and version constraints), and optional `"sources"` (version constraints on `SetGit` packages). All version constraints live in `vmake.json`; `AddRequires` accepts pure package IDs only.
2. **Unified Container seam**: Keeps `~/.vmake/repos` and adopter-controlled `~/.vmake/trust.json`, while unifying `native` and `registry` repositories behind one internal container interface with two adapters. A container root `build.go` is optional: when present it is a normal package (and may depend on `//...`); when absent, the container is pure metadata and never enters the dependency graph.
3. **Path-as-identity and O(1) loading**: Canonical package IDs are `//` or `//<relpath>` for local packages and `<repo>/<module>[/<relpath>]` for remote packages everywhere (scripts, `configs/*.json`, CLI, `inspect`, lockfile). Inside any module's scripts, `//<relpath>` always resolves relative to that module's own root (identical whether built locally or consumed remotely); cross-module references must begin with a module declared in that module's own `vmake.json` (no module aliases, no phantom transitive imports, no global leaf-name short-name matching). Local and remote packages both load lazily O(1) by path from declared roots instead of blind directory scanning.
4. **Lexical separation of members (`:`)**: In `AddDeps`, bare names are always same-package targets and cross-package references use `<pkg>` or `<pkg>:<target>`. Cross-package option reads (`ctx.Bool`) and `vmake config -s` / `--set` / `--unset` use `<pkg>:<opt>` (e.g. `//:debug=true`, `//firmware:wifi=true`, `hl/hl-mcu-sdk/system/freertos/lvgl:perf_monitor=true`); names without `:` are global options.
5. **Orthogonal `SetGit` sources, `vmake.lock` v2, and `pkg link`**: Whether a package's upstream Git source is locked, verified by `--locked`, shown in `deps status`, and recorded in `install/manifest.json` depends solely on whether it declares `SetGit`. `vmake.lock` v2 splits `containers` and `sources`. `vmake pkg link` independently supports container-level links and per-package `SetGit` source links; own-Git upstreams of native container packages live under `<owner>/third_party/<member>` regardless of link state.
6. **Transitive module resolution**: When multiple active modules constrain the same external container, all active constraints are intersected to select a single version per build (conflict is a hard error resolved by the root module or `deps pin`).
7. **TUI presentation (`vmake config`)**: The left tree groups packages by module container; container and pure directory headers render a group summary in the right panel instead of retaining a stale package selection. Active zero-option packages remain real selectable nodes inside their container (toggleable with `H`), and the ego-graph contains real packages and real dependency edges only.

## Considered Options

- **Container-unique logical package names (Cargo workspace/crate style)**: Kept shorter names across directory refactors, but required full-tree scanning or explicit member listings to map logical names to directories and preserved name-collision limits within a container. Rejected in favor of O(1) path-as-identity.
- **Per-module aliases in `vmake.json` (e.g. `@sdk/...`)**: Shortened script strings, but introduced a second context-dependent name alongside canonical IDs in configs, diagnostics, and `inspect`. Rejected because Go buildscripts can define local constants directly without adding a resolver translation seam.
- **Direct Git URLs in `vmake.json`**: Removed `~/.vmake/repos` indirection, but would bypass adopter-managed trust and intranet mirror URL templates. Rejected to preserve the supply-chain trust model in `docs/GOALS.md`.
- **Container-closed `SetGit` subpackages**: Kept lockfiles smaller by forcing static SHAs inside SDK scripts, but left third-party upstream trees outside `--locked` and `deps status`. Rejected in favor of orthogonal source tracking.

## Consequences

- Supersedes DD-1, DD-2, DD-3, DD-7, and DD-33, and marks the 2.0 breaking model cutover allowed under DD-34 (no external adopter compatibility promise yet).
- Eliminates `SubParents`, `ResolveSubPackageName`, `SubPackageCandidates`, `Scan`, `ScanSubPackages`, `SetRoot`, and version syntax in `AddRequires`.
- Existing 1.x multi-package projects and fixtures must add `vmake.json`, qualify package refs with `//` or canonical `<repo>/<module>/<path>`, switch option selectors to `<pkg>:<opt>`, and regenerate `.vmake/vmake.lock` (v2).
