---
status: accepted
---

# Container Encapsulation and Multi-SKU Delivery

VMake 2.0 ([ADR-0001](0001-separate-module-containers-from-packages.md)) separated module containers from build packages using strict path-as-identity and orthogonal `SetGit` source tracking. In real multi-package SDK delivery (an SDK container consumed by external product repositories), those defaults leaked internal SDK directory trees, leaf package options, and machine-global `~/.vmake/repos` aliases onto every downstream adopter, while single-configuration lock pruning prevented multi-SKU projects from committing `.vmake/vmake.lock`. We extend VMake 2.x with opt-in container encapsulation, self-contained module repository declarations, container-internal options, development roots, project-model path facts, and incremental multi-SKU lock retention while remaining 100% backward-compatible with VMake 2.0 manifests. Third-party `SetGit` sources always clone from their configured Git URLs rather than being vendored inside containers.

## Context

Applying ADR-0001 to `hl-mcu-sdk` (61 packages, 17 `SetGit` upstreams, 169 options) and `hl-demo` (205 SKU configurations) exposed six concrete seam leaks:

1. External consumers hardcoded 47/61 internal SDK directory paths (`system/freertos/fs/romfs/romfs-image`, `system/chip/puya/py32f539`) because path-as-identity had no container-level public export table.
2. Leaf package options flooded external configuration files and the TUI with no way to restrict internal tuning knobs to same-container facades.
3. Single-configuration builds pruned inactive SKU entries from `.vmake/vmake.lock`, making committed lockfiles impossible in multi-SKU product repositories.
4. `inspect --json` did not expose resolved `packageDir` and `srcDir` paths, forcing external test scripts to re-implement VMake's internal `MemberDir` SHA-256 directory layout.
5. Library/SDK containers without a root `build.go` or production `roots` could not run standalone builds, source syncs, or self-tests.
6. Every consumer had to manually run machine-global `vmake repo add --native <alias> ...` before cloning and building a project.

## Decisions

1. **Container export whitelist (`exports` in `vmake.json`)** *(amends ADR-0001 Decision 3)*: A module container may declare `"exports": { "<public-path>": "//<internal-path>" }` (using `"."` to export the container root). Export targets are 1-to-1 unique within the container. When `exports` is present (including an explicit empty `{}`), external containers may only reference exported logical IDs (`<container>/<public-path>`); all unlisted packages are private to the container across `AddRequires`, `AddDeps`, `OnConfigureDeps`, external config files, CLI `-s`, and the TUI. Intra-container `//<internal-path>` references canonicalize to the single exported canonical ID when exported, avoiding duplicate loading while preserving O(1) direct loading. Containers omitting `exports` retain full VMake 2.0 behavior.
2. **Option presentation vs encapsulation (`Option.SetAdvanced(true)` and `Option.SetInternal(true)`)**:
   - `SetAdvanced(true)` is UI-only tiering: advanced options stay collapsed by default in the TUI (toggled via clickable header/title/in-list controls or `a`, auto-shown when explicitly set in the active config or jumped to by search) and are tagged `advanced: true` in `inspect --json` `optionDefs`, while remaining **always customizable** via CLI `-s`, `configs/*.json`, `OnConfigureDeps`, and `ImportConfig`. SDKs should use `SetAdvanced(true)` for low-level production tuning knobs so adopters can customize without editing SDK source.
   - `SetInternal(true)` (mutually exclusive with global options) is strictly for container-private build plumbing that must never cross container seams: rejected across container boundaries in `OnConfigureDeps`, cross-package option reads, `ImportConfig` macro propagation, external config files, and CLI `-s`, hidden from the TUI workbench, and marked `internal: true` in `inspect --json` `optionDefs`.
3. **Development roots (`dev_roots` in `vmake.json`)**: A container may declare `"dev_roots": ["//..."]` for local development and self-testing. When built locally as the root project without explicit `roots` (or config root overrides), VMake uses `dev_roots`; when consumed externally as a module container, `dev_roots` is ignored.
4. **Incremental multi-SKU lock retention and `vmake deps prune`**: Regular builds, `deps sync`, and `deps update` retain existing `.vmake/vmake.lock` entries for containers and sources not active in the current configuration, updating only active entries. `--locked` validates active entries against the lockfile while tolerating inactive entries belonging to other SKUs. Stale entries are removed only on explicit `vmake deps prune` (or `--prune` on `deps sync` / `deps update`).
5. **Project model path facts (`packageDir`, `srcDir`)**: `PackageFacts` and `inspect --json` expose both `packageDir` (`PackageRoot`/`SourceDir()`, where `build.go` resides for local/native packages or `<deps>/<repo>/<pkg>/src` for registry packages) and `srcDir` (`SourceTree`/`SrcDir()`, the materialized or linked upstream source root) derived from `packageLocation`.
6. **Local path containers and self-contained `repos` in `vmake.json`**:
   - `modules` accepts either a version constraint string (`">=1.0.0"`) or an object (`{"version": ">=1.0.0"}` or `{"path": "./vendor/sdk"}`). Local path containers load directly from disk without `~/.vmake/repos` or remote lock entries; remote containers may not escape their own root directory with `..` or symlinks.
   - `vmake.json` may declare `"repos": { "<name>": { "native": "<url-template>" } }`. Machine-global `~/.vmake/repos/<name>` takes precedence so corporate adopters can redirect to intranet mirrors without editing project manifests. Remote script execution still passes through the adopter-owned `~/.vmake/trust.json` gate bound to `(repo, url-template)`, and `vmake repo trust <name> --url <template>` supports non-interactive trust setup.

## Considered Options

- **Sealing / vendoring third-party sources inside containers (`sealed_sources`)**: Rejected; third-party sources must always be cloned from their configured `SetGit` URLs (with lockfile provenance and patch auditability preserved via VMake's shared Git object cache) rather than vendored inside release containers.
- **Separate CMake delivery adapter alongside VMake**: Rejected because both internal R&D and external adopters use VMake; maintaining a parallel CMake build system would duplicate configuration logic and force rewriting ~2,900 lines of in-process Go build generators into another host runtime.
- **Merging `hl-mcu-sdk` and `hl-demo` into a single monorepo**: Rejected as a repository-level architectural decision; keeping SDK (Apache-2.0) and Demo (BSD-3-Clause) separate preserves license isolation and forces internal development to exercise the real external container seam.
- **Shipping prebuilt `libc.a` or host tool binaries inside SDK releases**: Rejected; adopters pre-install toolchains and build from source, avoiding cross-platform binary trust and maintenance burden.

## Consequences

- VMake 2.0 manifests, lockfiles, and build scripts remain 100% backward-compatible when `exports`, `dev_roots`, `repos`, object `modules`, and `SetInternal` are omitted.
- Single-configuration builds no longer drop inactive entries from `.vmake/vmake.lock`; adopters wishing to strip removed dependencies run `vmake deps prune` (or `deps sync --prune`).
- Third-party `SetGit` repositories continue to resolve, cache, lock, and patch uniformly from their configured Git URLs.
- Both `hl-mcu-sdk` and `hl-demo` can remain cleanly separated repositories while presenting a small, deep, self-contained interface to external adopters.
