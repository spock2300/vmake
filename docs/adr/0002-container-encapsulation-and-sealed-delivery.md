---
status: accepted
---

# Container Encapsulation and Sealed Delivery

VMake 2.0 ([ADR-0001](0001-separate-module-containers-from-packages.md)) separated module containers from build packages using strict path-as-identity and orthogonal `SetGit` source tracking. In real multi-package SDK delivery (an SDK container consumed by external product repositories), those defaults leaked internal SDK directory trees, leaf package options, machine-global `~/.vmake/repos` aliases, and third-party upstream repositories onto every downstream adopter, while single-configuration lock pruning prevented multi-SKU projects from committing `.vmake/vmake.lock`. We extend VMake 2.x with opt-in container encapsulation, self-contained module sources, sealed third-party sources, container-internal options, development roots, and incremental multi-SKU lock retention while remaining 100% backward-compatible with VMake 2.0 manifests.

## Context

Applying ADR-0001 to `hl-mcu-sdk` (61 packages, 17 `SetGit` upstreams, 168 options) and `hl-demo` (205 SKU configurations) exposed seven concrete seam leaks:

1. External consumers hardcoded 47/61 internal SDK directory paths (`system/freertos/fs/romfs/romfs-image`, `system/chip/puya/py32f539`) because path-as-identity had no container-level public export table.
2. Leaf package options (168 in SDK alone) flooded external configuration files and the TUI with no way to restrict internal tuning knobs to same-container facades.
3. Single-configuration builds pruned inactive SKU entries from `.vmake/vmake.lock`, making committed lockfiles impossible in multi-SKU product repositories.
4. `inspect --json` did not expose resolved `packageDir` and `srcDir` paths, forcing external test scripts to re-implement VMake's internal `MemberDir` SHA-256 directory layout.
5. Library/SDK containers without a root `build.go` or production `roots` could not run standalone builds, source syncs, or self-tests.
6. Every consumer had to manually run machine-global `vmake repo add --native <alias> ...` before cloning and building a project.
7. Downstream consumers of an SDK container inherited live Git fetches and lock entries for all 17 of the SDK's internal `SetGit` upstreams instead of locking a single sealed SDK release.

## Decisions

1. **Container export whitelist (`exports` in `vmake.json`)** *(amends ADR-0001 Decision 3)*: A module container may declare `"exports": { "<public-path>": "//<internal-path>" }` (using `"."` to export the container root). Export targets are 1-to-1 unique within the container. When `exports` is present, external containers may only reference exported logical IDs (`<container>/<public-path>`); all unlisted packages are private to the container across `AddRequires`, `AddDeps`, `OnConfigureDeps`, external config files, CLI `-s`, and the TUI. Intra-container `//<internal-path>` references canonicalize to the single exported canonical ID when exported, avoiding duplicate loading while preserving O(1) direct loading. Containers omitting `exports` retain full VMake 2.0 behavior.
2. **Container-internal options (`Option.SetInternal(true)`)**: A package option marked internal (mutually exclusive with global options) may only be configured within its own container (same-container `OnConfigureDeps`, or local container configuration during development). Across container seams, internal options are rejected in `OnConfigureDeps`, cross-package option reads, `ImportConfig` macro propagation, external config files, and CLI `-s`, hidden from the TUI workbench, and marked `internal: true` in `inspect --json` `optionDefs`.
3. **Development roots (`dev_roots` in `vmake.json`)**: A container may declare `"dev_roots": ["//..."]` for local development and self-testing. When built locally as the root project without explicit `roots` (or config root overrides), VMake uses `dev_roots`; when consumed externally as a module container, `dev_roots` is ignored.
4. **Incremental multi-SKU lock retention and `vmake deps prune`**: Regular builds, `deps sync`, and `deps update` retain existing `.vmake/vmake.lock` entries for containers and sources not active in the current configuration, updating only active entries. `--locked` validates active entries against the lockfile while tolerating inactive entries belonging to other SKUs. Stale entries are removed only on explicit `vmake deps prune` (or `--prune` on `deps sync` / `deps update`).
5. **Project model path facts (`packageDir`, `srcDir`)**: `PackageFacts` and `inspect --json` expose both `packageDir` (`PackageRoot`, where `build.go` resides) and `srcDir` (`SourceTree`, the materialized or linked upstream source root) derived from `packageLocation`.
6. **Local path containers and self-contained `repos` in `vmake.json`**:
   - `modules` accepts either a version constraint string (`">=1.0.0"`) or an object (`{"version": ">=1.0.0"}` or `{"path": "./vendor/sdk"}`). Local path containers load directly from disk without `~/.vmake/repos` or remote lock entries; remote containers may not escape their own root directory with `..`.
   - `vmake.json` may declare `"repos": { "<name>": { "native": "<url-template>" } }`. Machine-global `~/.vmake/repos/<name>` takes precedence so corporate adopters can redirect to intranet mirrors without editing project manifests. Remote script execution still passes through the adopter-owned `~/.vmake/trust.json` gate bound to `(repo, url-template)`, and `vmake repo trust <name> --url <template>` supports non-interactive trust setup.
7. **Sealed third-party sources (`sealed_sources` in `vmake.json`)** *(amends ADR-0001 Decision 5)*: A container release may vendor pre-fetched, pre-patched third-party trees inside the container and declare `"sealed_sources": { "//<pkg>": "<rel-dir>" }`. Unless explicitly overridden by `vmake pkg link`, VMake points `p.SrcDir()` directly at the sealed directory, skips upstream Git fetches and patch application, omits sealed members from consumer `.vmake/vmake.lock` and `deps status`, and reports `sealed: true` in `inspect --json`. During internal development, omitting `sealed_sources` preserves the full orthogonal `SetGit` workflow.

## Considered Options

- **Separate CMake delivery adapter alongside VMake**: Rejected because both internal R&D and external adopters use VMake; maintaining a parallel CMake build system would duplicate configuration logic and force rewriting ~2,900 lines of in-process Go build generators into another host runtime.
- **Merging `hl-mcu-sdk` and `hl-demo` into a single monorepo**: Rejected as a repository-level architectural decision; keeping SDK (Apache-2.0) and Demo (BSD-3-Clause) separate preserves license isolation and forces internal development to exercise the real external container seam.
- **Shipping prebuilt `libc.a` or host tool binaries inside SDK releases**: Rejected; adopters pre-install toolchains and build from source, avoiding cross-platform binary trust and maintenance burden.

## Consequences

- VMake 2.0 manifests, lockfiles, and build scripts remain 100% backward-compatible when `exports`, `dev_roots`, `repos`, `sealed_sources`, object `modules`, and `SetInternal` are omitted.
- Single-configuration builds no longer drop inactive entries from `.vmake/vmake.lock`; adopters wishing to strip removed dependencies run `vmake deps prune` (or `deps sync --prune`).
- Both `hl-mcu-sdk` and `hl-demo` can remain cleanly separated repositories while presenting a small, deep, offline-capable, self-contained interface to external adopters.
