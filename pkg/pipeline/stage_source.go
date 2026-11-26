package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

// sourceDownloadLog prints the source-download banner only when a tree actually
// needs materialization, so no-op builds keep their existing shape.
type sourceDownloadLog struct {
	printed bool
}

func (l *sourceDownloadLog) print() {
	if l == nil || l.printed {
		return
	}
	l.printed = true
	vlog.Info("")
	vlog.Info("Downloading package sources...")
}

func (s *sourceStage) prepareAllPackages() error {
	if s.locked {
		if err := s.verifyLockBeforePrepare(); err != nil {
			return err
		}
	}
	downloadLog := &sourceDownloadLog{}

	if err := s.prepareContainers(downloadLog); err != nil {
		return err
	}

	if err := s.preparePackages(downloadLog); err != nil {
		return err
	}

	return nil
}

func (s *sourceStage) prepareContainers(downloadLog *sourceDownloadLog) error {
	neededContainers := NeededContainers(s.rc.ctx.DepGraph, s.conf.needed)
	containerIDs := make([]string, 0, len(neededContainers))
	for id := range neededContainers {
		containerIDs = append(containerIDs, id)
	}
	sort.Strings(containerIDs)

	for _, id := range containerIDs {
		c := s.rc.ctx.DepGraph.Containers[id]
		if c == nil || c.IsLocal() {
			continue
		}
		if c.IsRegistry() {
			commit := c.Commit
			if commit == "" {
				var err error
				commit, err = s.registryCommit(c.Repo)
				if err != nil {
					return err
				}
			}
			s.containerCommits[id] = commit
			continue
		}
		if c.IsNative() {
			if err := s.prepareNativeContainer(id, c, downloadLog); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *sourceStage) prepareNativeContainer(id string, c *resolver.ContainerNode, downloadLog *sourceDownloadLog) error {
	if c.IsLinked() {
		version := resolver.LocalLinkVersion
		s.containerVersions[id] = version
		s.containerCommits[id] = c.Link.Commit
		s.containerTrees[id] = remoteTreeDir(s.rc.ctx, id)
		vlog.Verbose("  %s@%s (linked %s)", id, version, c.Link.Path)
		return nil
	}
	if c.IsLocalPath() {
		s.containerVersions[id] = c.Version
		s.containerCommits[id] = c.Commit
		s.containerTrees[id] = remoteTreeDir(s.rc.ctx, id)
		vlog.Verbose("  %s@%s (path %s)", id, c.Version, c.Dir)
		return nil
	}

	decision := s.rc.ctx.lockQuery().Containers().Decide(id, c.Version, c.Commit)
	pinned := decision.Provenance == resolver.ProvenanceTargetCommit ||
		decision.Provenance == resolver.ProvenanceTargetVersion ||
		decision.Provenance == resolver.ProvenanceConfigPin
	fromTarget := decision.FromTarget
	version := decision.Version
	pinnedCommit := decision.Commit
	if decision.Provenance == resolver.ProvenanceTargetCommit {
		version = ""
	}

	if pinned && version != "" && len(c.Versions) > 0 {
		if _, exists := c.Versions[version]; !exists {
			if fromTarget {
				return fmt.Errorf("requested version %s for %s not found in declared versions", version, id)
			}
			return fmt.Errorf("config-pinned version %s for %s not found in declared versions", version, id)
		}
	}
	if version == "" && pinnedCommit == "" && len(c.Versions) > 0 {
		selected, _, err := repo.SelectNativeVersionMulti(c.Versions, c.Constraints)
		if err != nil {
			return err
		}
		version = selected
	}

	expectedCommit := pinnedCommit
	if expectedCommit == "" {
		if locked, ok := s.rc.ctx.lockQuery().Containers().LockEntry(id); ok && locked.Commit != "" && locked.Version == version {
			expectedCommit = locked.Commit
		}
	}

	treePatchHash, submodules, err := s.containerTreeIdentity(id)
	if err != nil {
		return err
	}

	ref := ""
	if version != "" {
		if r, ok := c.Versions[version]; ok && r != "" {
			ref = r
		} else {
			ref = version
		}
	}

	req := repo.SourceRequest{
		Key:        id,
		URLs:       []string{c.URL},
		Version:    version,
		Ref:        ref,
		Commit:     expectedCommit,
		PatchHash:  treePatchHash,
		Submodules: submodules,
		Refresh:    expectedCommit == "" && s.rc.ctx.isLockIgnoredFor(id),
	}
	if !s.sourceRequestReady(req) {
		downloadLog.print()
	}
	res, err := s.ensureSource(req)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", id, err)
	}
	s.containerCommits[id] = res.Commit
	s.containerTrees[id] = res.Root
	s.containerVersions[id] = version
	vlog.Verbose("  %s@%s -> %s", id, version, res.SrcDir)
	return nil
}

func (s *sourceStage) preparePackages(downloadLog *sourceDownloadLog) error {
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		node := s.rc.ctx.DepGraph.Packages[name]
		if !s.conf.needed[name] || node == nil || node.Source == nil {
			continue
		}
		resolvedTools, err := s.rc.toolsForPackage(name)
		if err != nil {
			return err
		}
		patchHash, err := patchHashForNode(name, node)
		if err != nil {
			return err
		}
		s.patchHashes[name] = patchHash

		loc := s.rc.ctx.packageLocation(name, node)

		if node.HasActiveOwnGit() {
			if loc.Linked {
				version := resolver.LocalLinkVersion
				s.sourceVersions[name] = version
				s.sourceCommits[name] = node.Source.Link.Commit
				s.sourceSeeds[name] = node.Source.Link.Path
				s.patchHashes[name] = ""
				vlog.Verbose("  %s@%s (linked %s)", name, version, node.Source.Link.Path)
			} else {
				materializeRoot := loc.ThirdPartyRoot
				if s.ownGitSourceNeedsMaterialization(name, materializeRoot, node) {
					downloadLog.print()
				}
				if err := s.selectPackageSourceRoot(name, materializeRoot); err != nil {
					return fmt.Errorf("select source for %s: %w", name, err)
				}
			}
		} else if node.IsRegistry() {
			if loc.Linked {
				s.sourceVersions[name] = resolver.LocalLinkVersion
				s.sourceCommits[name] = node.Source.Link.Commit
				s.sourceSeeds[name] = node.Source.Link.Path
			} else {
				return fmt.Errorf("failed to download %s: package %s has no git URL", name, name)
			}
		}

		if err := s.setPackageLayout(name, resolvedTools); err != nil {
			return err
		}
		if err := s.preparePackageWorkspace(name); err != nil {
			return fmt.Errorf("prepare workspace for %s: %w", name, err)
		}
	}
	return nil
}

func (s *sourceStage) ensureSource(req repo.SourceRequest) (*repo.SourceResult, error) {
	if s.offline {
		req.Offline = true
	}
	manager := repo.NewSourceManager(s.rc.ctx.Paths.DepsDir, s.rc.ctx.Paths.CacheDir).WithSession(s.rc.ctx.Locks).WithContext(s.rc.ctx.Context)
	if !s.dryRun || s.sourceRequestReady(req) {
		return manager.EnsureSource(req)
	}
	dryRunManager := repo.NewSourceManager(s.dryRunDepsDir, s.rc.ctx.Paths.CacheDir).WithSession(s.rc.ctx.Locks).WithContext(s.rc.ctx.Context)
	if req.Root != "" {
		req.Root = filepath.Join(s.dryRunDepsDir, "sub", filepath.FromSlash(req.Key))
	}
	return dryRunManager.EnsureSource(req)
}

func (s *sourceStage) registryCommit(repoName string) (string, error) {
	if repoName == "" || s.rc.ctx.Paths == nil || s.rc.ctx.Paths.ReposDir == "" {
		return "", nil
	}
	mgr := repo.NewRepoManager(s.rc.ctx.Paths.ReposDir)
	if !mgr.Exists(repoName) {
		return "", nil
	}
	commit, err := repo.GetCurrentCommitContext(s.rc.ctx.Context, mgr.Path(repoName))
	if err != nil {
		return "", fmt.Errorf("read registry %s HEAD: %w", repoName, err)
	}
	return commit, nil
}

func (s *sourceStage) verifyLockBeforePrepare() error {
	// 1. Check needed external containers.
	neededContainers := NeededContainers(s.rc.ctx.DepGraph, s.conf.needed)
	containerIDs := make([]string, 0, len(neededContainers))
	for id := range neededContainers {
		containerIDs = append(containerIDs, id)
	}
	sort.Strings(containerIDs)
	for _, id := range containerIDs {
		c := s.rc.ctx.DepGraph.Containers[id]
		if c == nil || c.IsLocal() || c.IsLinked() || c.IsLocalPath() {
			continue
		}
		if s.rc.ctx.Lock == nil || s.rc.ctx.Lock.IsEmpty() {
			return fmt.Errorf("--locked: vmake.lock is out of date or missing; run 'vmake deps sync' or 'vmake deps update'")
		}
		query := s.rc.ctx.lockQuery().Containers()
		if problem, bad := query.LockEntryProblem(id, true); bad {
			return problem.Error(id)
		}
		if problem, mismatch := query.PinLockMismatch(id); mismatch {
			return problem.Error(id)
		}
		if c.IsRegistry() {
			locked, _ := s.rc.ctx.Lock.GetContainer(id)
			head, err := s.registryCommit(c.Repo)
			if err != nil {
				return err
			}
			if head != "" && locked != nil && locked.Commit != "" && head != locked.Commit {
				return fmt.Errorf("--locked: registry '%s' content changed since lock (commit %s expected, now %s); run 'vmake deps update' to re-resolve",
					id, repo.ShortCommit(locked.Commit), repo.ShortCommit(head))
			}
		}
	}

	// 2. Check all needed SetGit sources (local, registry, and native members).
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		if !s.conf.needed[name] {
			continue
		}
		node := s.rc.ctx.DepGraph.Packages[name]
		if node == nil || !node.HasActiveOwnGit() {
			continue
		}
		if node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource {
			continue
		}
		if s.rc.ctx.Lock == nil || s.rc.ctx.Lock.IsEmpty() {
			return fmt.Errorf("--locked: vmake.lock is out of date or missing; run 'vmake deps sync' or 'vmake deps update'")
		}
		query := s.rc.ctx.lockQuery().Sources()
		if problem, bad := query.LockEntryProblem(name, true); bad {
			return problem.Error(name)
		}
		if problem, mismatch := query.PinLockMismatch(name); mismatch {
			return problem.Error(name)
		}
	}
	return nil
}

func (s *sourceStage) buildLockfile() (*lockfile.Lock, error) {
	decisions, err := s.lockDecisions()
	if err != nil {
		return nil, err
	}
	return projectLock(decisions, s.rc.ctx.Lock), nil
}

func (s *sourceStage) verifyLocked() error {
	decisions, err := s.lockDecisions()
	if err != nil {
		return err
	}
	return validateLockedProjection(decisions, s.rc.ctx.Lock)
}

func (s *sourceStage) writeLockfile() error {
	if s.rc.ctx.LockPath == "" {
		return nil
	}
	updated, err := s.buildLockfile()
	if err != nil {
		return err
	}
	return s.saveLockfile(updated)
}

func (s *sourceStage) saveLockfile(updated *lockfile.Lock) error {
	if s.rc.ctx.LockPath == "" {
		return nil
	}
	if s.rc.ctx.Lock != nil {
		for _, name := range s.rc.ctx.Lock.SortedContainerNames() {
			if _, ok := updated.GetContainer(name); !ok {
				vlog.Verbose("  lock: dropped stale container %s", name)
			}
		}
		for _, name := range s.rc.ctx.Lock.SortedSourceNames() {
			if _, ok := updated.GetSource(name); !ok {
				vlog.Verbose("  lock: dropped stale source %s", name)
			}
		}
	}
	if updated.IsEmpty() && (s.rc.ctx.Lock == nil || s.rc.ctx.Lock.IsEmpty()) {
		return nil
	}
	if s.rc.ctx.Lock != nil && s.rc.ctx.Lock.Equal(updated) {
		return nil
	}
	if err := updated.Save(s.rc.ctx.LockPath); err != nil {
		return fmt.Errorf("write %s: %w", s.rc.ctx.LockPath, err)
	}
	s.rc.ctx.Lock = updated
	vlog.Info("Wrote %s (%d container(s), %d source(s))", s.rc.ctx.LockPath, len(updated.Containers), len(updated.Sources))
	return nil
}

// containerTreeIdentity summarizes the patch sets and submodule flag across
// all needed non-own-Git packages sharing containerID's working tree.
func (s *sourceStage) containerTreeIdentity(containerID string) (string, bool, error) {
	var names []string
	submodules := false
	for name, node := range s.rc.ctx.DepGraph.Packages {
		if !s.conf.needed[name] || node == nil || node.Container != containerID {
			continue
		}
		if node.HasOwnGit() {
			continue
		}
		names = append(names, name)
		if node.Pkg != nil && node.Pkg.Submodules() {
			submodules = true
		}
	}
	sort.Strings(names)
	hashes := make(map[string]string, len(names))
	nonEmptyCount := 0
	singleHash := ""
	for _, name := range names {
		hash, err := patchHashForNode(name, s.rc.ctx.DepGraph.Packages[name])
		if err != nil {
			return "", false, err
		}
		s.patchHashes[name] = hash
		hashes[name] = hash
		if hash != "" {
			nonEmptyCount++
			singleHash = hash
		}
	}
	if nonEmptyCount == 0 {
		return "", submodules, nil
	}
	if nonEmptyCount == 1 && len(names) == 1 {
		return singleHash, submodules, nil
	}
	h := sha256.New()
	for _, name := range names {
		fmt.Fprintf(h, "%s\x00%s\x00", name, hashes[name])
	}
	return hex.EncodeToString(h.Sum(nil))[:16], submodules, nil
}

// sourceRequestReady reports whether a tree is already materialized at the
// identity EnsureSource would request, mirroring its reuse rules without
// materializing anything.
func (s *sourceStage) sourceRequestReady(req repo.SourceRequest) bool {
	manager := repo.NewSourceManager(s.rc.ctx.Paths.DepsDir, s.rc.ctx.Paths.CacheDir).WithSession(s.rc.ctx.Locks).WithContext(s.rc.ctx.Context)
	return !manager.WouldMaterialize(req)
}

func (s *sourceStage) ownGitSourceNeedsMaterialization(name, sourceRoot string, node *resolver.PackageNode) bool {
	if !node.HasActiveOwnGit() {
		return false
	}
	req, err := s.sourceRequestFor(name, node, sourceRoot)
	if err != nil {
		return true
	}
	return !s.sourceRequestReady(req)
}

func (s *sourceStage) applyPatchesToNeeded() error {
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		node := s.rc.ctx.DepGraph.Packages[name]
		if !s.conf.needed[name] || node == nil || node.Pkg == nil || node.Source == nil || node.IsSealed() {
			continue
		}
		if len(node.Pkg.GetPatches()) == 0 {
			continue
		}
		if err := s.applyPatchesFor(name); err != nil {
			return err
		}
	}
	return nil
}

// applyPatchesFor applies the package patch series to its working tree. A
// failed application clears the tree's patch identity so the next invocation
// recreates a clean tree instead of reusing a partially patched one.
func (s *sourceStage) applyPatchesFor(name string) error {
	node := s.rc.ctx.DepGraph.Packages[name]
	if node == nil || node.Pkg == nil || node.IsSealed() {
		return nil
	}
	if node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource && pathWithin(node.Source.Link.Path, node.Pkg.SrcDir()) {
		return fmt.Errorf("apply patches for %s: linked source %s is user-managed", name, node.Source.Link.Path)
	}
	if err := applyPatchesContext(s.rc.ctx.Context, node.Pkg, node.Pkg.SrcDir()); err != nil {
		s.clearPatchState(name)
		return fmt.Errorf("apply patches for %s: %w", name, err)
	}
	return nil
}

func (s *sourceStage) clearPatchState(name string) {
	if s.dryRun {
		return
	}
	root := s.treeRootFor(name)
	if root == "" {
		return
	}
	manager := repo.NewSourceManager(s.rc.ctx.Paths.DepsDir, s.rc.ctx.Paths.CacheDir).WithSession(s.rc.ctx.Locks).WithContext(s.rc.ctx.Context)
	if err := manager.ClearPatchState(root); err != nil {
		vlog.Info("  warning: reset patch state for %s: %v", name, err)
	}
}

func (s *sourceStage) treeRootFor(name string) string {
	return packageTreeRoot(s.rc.ctx, name, s.rc.ctx.DepGraph.Packages[name])
}

func (s *sourceStage) restoreKConfigs() error {
	return restoreKConfigFiles(s.rc.ctx, s.pkgDirs, s.conf.needed)
}

func (s *sourceStage) installedPkgs() map[string]*api.InstalledPackage {
	if len(s.pkgDirs) == 0 {
		return nil
	}
	result := make(map[string]*api.InstalledPackage)
	for name, d := range s.pkgDirs {
		if d.InstallDir == "" {
			continue
		}
		node := s.rc.ctx.DepGraph.Packages[name]
		if node == nil || node.IsLocal() {
			continue
		}
		version := s.sourceVersions[name]
		if node.IsNative() {
			version = s.containerVersions[node.Container]
		}
		result[name] = api.NewInstalledPackage(name, version, d.InstallDir, nil)
	}
	return result
}

func acquireStorageOwners(ctx *RuntimeContext, needed NeededSet) error {
	if ctx.Locks == nil {
		return nil
	}
	seen := make(map[string]bool)
	var owners []string
	for name := range needed {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.IsLocal() {
			continue
		}
		owner := remoteOwnerName(ctx, name)
		if owner != "" && !seen[owner] {
			seen[owner] = true
			owners = append(owners, owner)
		}
	}
	sort.Strings(owners)
	return ctx.Locks.AcquireOwnersContext(ctx.Context, owners)
}
