package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

// sourceDownloadLog prints the source-download banner only when a package tree
// actually needs materialization, so no-op builds keep their existing shape.
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
	remote := &remoteVersionState{
		commits:  make(map[string]string),
		trees:    make(map[string]string),
		versions: make(map[string]string),
	}
	downloadLog := &sourceDownloadLog{}

	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		node := s.rc.ctx.DepGraph.Packages[name]
		if !s.conf.needed[name] || node.Source == nil || !node.IsLocal() {
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
		if s.localSourceNeedsMaterialization(name, node) {
			downloadLog.print()
		}
		if err := s.finalizePackageSource(name, node.Source.Dir, resolvedTools); err != nil {
			return err
		}
	}

	depsDir := s.rc.ctx.Paths.DepsDir

	if err := s.downloadRemoteSources(remote, depsDir, downloadLog); err != nil {
		return err
	}
	s.remote = remote

	if err := s.setupSubPackageDirs(downloadLog); err != nil {
		return err
	}

	if err := s.cloneLocalGitSources(); err != nil {
		return err
	}

	if err := s.cloneSubPackageGitSources(); err != nil {
		return err
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

func (s *sourceStage) registryWrapperCommit(name string) (string, error) {
	repoName, _, ok := api.SplitPackageRef(name)
	if !ok {
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
	subParents := s.rc.ctx.Resolver.SubParents()
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		if !s.conf.needed[name] {
			continue
		}
		node := s.rc.ctx.DepGraph.Packages[name]
		if node == nil || (node.Source != nil && node.Source.Link != nil) {
			continue
		}
		if _, isSub := subParents[name]; isSub {
			continue
		}
		if node.IsLocal() && (node.Pkg == nil || len(node.Pkg.GitURLs()) == 0) {
			continue
		}
		if s.rc.ctx.Lock == nil || len(s.rc.ctx.Lock.Packages) == 0 {
			return fmt.Errorf("--locked: vmake.lock is out of date or missing; run 'vmake deps sync' or 'vmake deps update'")
		}
		locked, ok := s.rc.ctx.Lock.Get(name)
		if !ok || locked.Commit == "" {
			return fmt.Errorf("--locked: package %s is not locked in vmake.lock; run 'vmake deps sync' or 'vmake deps update'", name)
		}
		if pin, _, hasPin := s.rc.ctx.packagePin(name); hasPin {
			if (repo.IsCommitID(pin) && locked.Commit != pin) || (!repo.IsCommitID(pin) && locked.Version != pin) {
				return fmt.Errorf("--locked: package %s is pinned to %s in .vmake/project.json, but vmake.lock has %s; run 'vmake deps sync'", name, pin, lockfile.DisplayVersion(locked.Version, locked.Commit))
			}
		}
		if !node.IsLocal() && node.Native == nil && locked.WrapperCommit != "" {
			wrapperCommit, err := s.registryWrapperCommit(name)
			if err != nil {
				return err
			}
			if wrapperCommit != "" && wrapperCommit != locked.WrapperCommit {
				return fmt.Errorf("--locked: registry wrapper commit for %s changed (%s -> %s); run 'vmake deps update %s'", name, repo.ShortCommit(locked.WrapperCommit), repo.ShortCommit(wrapperCommit), name)
			}
		}
	}
	return nil
}

func (s *sourceStage) buildLockfile() (*lockfile.Lock, error) {
	updated := lockfile.New()
	if s.rc.ctx.Lock != nil {
		for name := range s.rc.ctx.SourceLinks {
			if old, ok := s.rc.ctx.Lock.Get(name); ok {
				updated.Set(name, old)
			}
		}
	}
	subParents := s.rc.ctx.Resolver.SubParents()
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		node := s.rc.ctx.DepGraph.Packages[name]
		if node.Source != nil && node.Source.Link != nil {
			continue
		}
		if _, isSub := subParents[name]; isSub {
			continue
		}
		if node.IsLocal() {
			if version := s.sourceVersions[name]; version != "" || s.sourceCommits[name] != "" {
				updated.Set(name, &lockfile.LockedPkg{Version: version, Commit: s.sourceCommits[name], Source: "git"})
			} else if node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 && s.rc.ctx.Lock != nil && !s.rc.ctx.isLockIgnoredFor(name) {
				if old, ok := s.rc.ctx.Lock.Get(name); ok && old.Source == "git" {
					updated.Set(name, old)
				}
			}
			continue
		}
		version := s.sourceVersions[name]
		commit := ""
		if s.remote != nil {
			if version == "" {
				version = s.remote.versions[name]
			}
			commit = s.remote.commits[name]
		}
		if version == "" && commit == "" {
			if s.rc.ctx.Lock != nil && !s.rc.ctx.isLockIgnoredFor(name) {
				if old, ok := s.rc.ctx.Lock.Get(name); ok {
					updated.Set(name, old)
					continue
				}
			}
			if node.Native != nil && (node.Native.Selected != "" || node.Native.Commit != "") {
				version = node.Native.Selected
				commit = node.Native.Commit
			}
		}
		if version == "" && commit == "" {
			continue
		}
		source := "registry"
		if node.Native != nil {
			source = "native"
		}
		locked := &lockfile.LockedPkg{
			Version: version,
			Commit:  commit,
			Source:  source,
		}
		if source == "registry" {
			wrapperCommit, err := s.registryWrapperCommit(name)
			if err != nil {
				return nil, fmt.Errorf("pin wrapper commit for %s: %w", name, err)
			}
			locked.WrapperCommit = wrapperCommit
		}
		updated.Set(name, locked)
	}
	return updated, nil
}

func (s *sourceStage) verifyLocked() error {
	updated, err := s.buildLockfile()
	if err != nil {
		return err
	}
	if len(updated.Packages) == 0 && (s.rc.ctx.Lock == nil || len(s.rc.ctx.Lock.Packages) == 0) {
		return nil
	}
	if s.rc.ctx.Lock == nil || !s.rc.ctx.Lock.Equal(updated) {
		changes := lockfile.Diff(s.rc.ctx.Lock, updated)
		if len(changes) > 0 {
			var names []string
			for _, ch := range changes {
				names = append(names, fmt.Sprintf("%s (%s)", ch.Name, ch.Kind))
			}
			return fmt.Errorf("--locked: vmake.lock is out of date (%s); run 'vmake deps sync' or 'vmake deps update'", strings.Join(names, ", "))
		}
		return fmt.Errorf("--locked: vmake.lock is out of date or missing; run 'vmake deps sync' or 'vmake deps update'")
	}
	return nil
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
		for _, name := range s.rc.ctx.Lock.SortedNames() {
			if _, ok := updated.Get(name); !ok {
				vlog.Verbose("  lock: dropped stale entry %s", name)
			}
		}
	}
	if len(updated.Packages) == 0 && (s.rc.ctx.Lock == nil || len(s.rc.ctx.Lock.Packages) == 0) {
		return nil
	}
	if s.rc.ctx.Lock != nil && s.rc.ctx.Lock.Equal(updated) {
		return nil
	}
	if err := updated.Save(s.rc.ctx.LockPath); err != nil {
		return fmt.Errorf("write %s: %w", s.rc.ctx.LockPath, err)
	}
	s.rc.ctx.Lock = updated
	vlog.Info("Wrote %s (%d package(s))", s.rc.ctx.LockPath, len(updated.Packages))
	return nil
}

func (s *sourceStage) downloadRemoteSources(remote *remoteVersionState, depsDir string, downloadLog *sourceDownloadLog) error {
	s.remote = remote
	subParents := s.rc.ctx.Resolver.SubParents()
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		node := s.rc.ctx.DepGraph.Packages[name]
		if !s.conf.needed[name] || node.IsLocal() {
			continue
		}
		if _, isSub := subParents[name]; isSub {
			continue
		}
		resolvedTools, err := s.rc.toolsForPackage(name)
		if err != nil {
			return err
		}
		if node.Source != nil && node.Source.Link != nil {
			version := resolver.LocalLinkVersion
			s.sourceVersions[name] = version
			s.patchHashes[name] = ""
			remote.commits[name] = node.Source.Link.Commit
			remote.trees[name] = remoteTreeDir(s.rc.ctx, name)
			remote.setVersion(name, version)
			vlog.Verbose("  %s@%s (linked %s)", name, version, node.Source.Link.Path)
			if err := s.setPackageLayout(name, resolvedTools); err != nil {
				return err
			}
			if err := s.preparePackageWorkspace(name); err != nil {
				return fmt.Errorf("prepare workspace for %s: %w", name, err)
			}
			continue
		}
		target, fromTarget, hasPin := s.rc.ctx.packagePin(name)
		version := ""
		pinnedCommit := ""
		if hasPin {
			if repo.IsCommitID(target) {
				pinnedCommit = target
			} else {
				version = target
			}
		}
		repoName, pkgName, ok := api.SplitPackageRef(name)
		if !ok {
			return fmt.Errorf("invalid package ref %q", name)
		}
		pkg := api.NewPackage().SetRepo(repoName).SetName(pkgName)
		if node.IsNative() {
			if !hasPin && version == "" {
				version = node.Native.Selected
			}
			if pinnedCommit == "" && node.Native.Commit != "" && (version == node.Native.Selected || version == "") {
				pinnedCommit = node.Native.Commit
			}
			pkg.SetGit(node.Native.GitURL)
			pkg.SetVersions(node.Native.Versions)
			if node.Pkg != nil {
				pkg.SetSubmodules(node.Pkg.Submodules())
			}
		} else if node.Pkg != nil {
			pkg.SetGit(node.Pkg.GitURLs()...)
			pkg.SetVersions(node.Pkg.Versions())
			pkg.SetSubmodules(node.Pkg.Submodules())
		} else if node.Source != nil && node.Source.Path != "" {
			interpreted, err := buildscript.LoadBuildScriptWithTrust(*node.Source, s.rc.ctx.TrustChecker)
			if err != nil {
				return fmt.Errorf("load %s for download: %w", name, err)
			}
			pkg.SetVersions(interpreted.Versions())
			pkg.SetGit(interpreted.GitURLs()...)
		}

		if !hasPin && version == "" && pinnedCommit == "" {
			if locked, isLocked := s.rc.ctx.lockedEntry(name); isLocked {
				version = locked.Version
				if version == "" {
					pinnedCommit = locked.Commit
				}
			}
		}
		if hasPin && version != "" {
			if len(pkg.Versions()) > 0 {
				if _, exists := pkg.Versions()[version]; !exists {
					if fromTarget {
						return fmt.Errorf("requested version %s for %s not found in declared versions", version, name)
					}
					return fmt.Errorf("config-pinned version %s for %s not found in declared versions", version, name)
				}
			} else if len(pkg.GitURLs()) > 0 && !node.IsNative() {
				return fmt.Errorf("package %s declares no versions; specify a 40-character commit SHA instead of %q", name, version)
			}
		}
		if version == "" && pinnedCommit == "" && len(pkg.GetVersions()) > 0 {
			var selected string
			var err error
			if len(node.Constraints) > 0 {
				selected, err = pkg.SelectVersionMulti(node.Constraints)
			} else {
				selected, err = pkg.SelectVersion("")
			}
			if err != nil {
				return err
			}
			version = selected
		}
		expectedCommit := pinnedCommit
		if expectedCommit == "" {
			if locked, ok := s.rc.ctx.lockedEntry(name); ok && locked.Commit != "" && locked.Version == version {
				expectedCommit = locked.Commit
			}
		}
		patchHash, err := patchHashForNode(name, node)
		if err != nil {
			return err
		}
		treePatchHash, err := s.treePatchHash(name)
		if err != nil {
			return err
		}
		ref := ""
		if version != "" {
			ref = pkg.GetRef(version)
			if ref == "" {
				ref = version
			}
		}
		req := repo.SourceRequest{
			Key:        name,
			URLs:       pkg.GitURLs(),
			Version:    version,
			Ref:        ref,
			Commit:     expectedCommit,
			PatchHash:  treePatchHash,
			Submodules: pkg.Submodules(),
			Refresh:    expectedCommit == "" && s.rc.ctx.isLockIgnoredFor(name),
		}
		if !s.sourceRequestReady(req) {
			downloadLog.print()
		}
		res, err := s.ensureSource(req)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", name, err)
		}
		remote.commits[name] = res.Commit
		remote.trees[name] = res.Root
		remote.setVersion(name, version)
		s.sourceVersions[name] = version
		s.patchHashes[name] = patchHash
		vlog.Verbose("  %s@%s -> %s", name, version, res.SrcDir)
		if err := s.setPackageLayout(name, resolvedTools); err != nil {
			return err
		}
		if err := s.preparePackageWorkspace(name); err != nil {
			return fmt.Errorf("prepare workspace for %s: %w", name, err)
		}
	}
	return nil
}

// treePatchHash summarizes the patch sets applied into a package tree. Native
// members without their own repository share the parent's tree, so their
// patches are part of the tree identity: a changed member patch must recreate
// the tree instead of stacking onto an already patched checkout.
func (s *sourceStage) treePatchHash(owner string) (string, error) {
	names := []string{owner}
	for name, parent := range s.rc.ctx.Resolver.SubParents() {
		if parent != owner || !s.conf.needed[name] {
			continue
		}
		node := s.rc.ctx.DepGraph.Packages[name]
		if node == nil || node.Pkg == nil || len(node.Pkg.GitURLs()) > 0 {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	hashes := make(map[string]string, len(names))
	shared := false
	for _, name := range names {
		hash, err := patchHashForNode(name, s.rc.ctx.DepGraph.Packages[name])
		if err != nil {
			return "", err
		}
		s.patchHashes[name] = hash
		hashes[name] = hash
		if name != owner && hash != "" {
			shared = true
		}
	}
	if !shared {
		return hashes[owner], nil
	}
	h := sha256.New()
	for _, name := range names {
		fmt.Fprintf(h, "%s\x00%s\x00", name, hashes[name])
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// sourceRequestReady reports whether a package tree is already materialized at
// the identity EnsureSource would request, mirroring its reuse rules without
// materializing anything. A false result only decides whether the source
// phase banner is printed before the tree is fetched or recreated.
func (s *sourceStage) sourceRequestReady(req repo.SourceRequest) bool {
	manager := repo.NewSourceManager(s.rc.ctx.Paths.DepsDir, s.rc.ctx.Paths.CacheDir).WithSession(s.rc.ctx.Locks).WithContext(s.rc.ctx.Context)
	root := req.Root
	if root == "" {
		root = manager.TreeDir(req.Key)
	}
	state, err := manager.ReadState(root)
	if err != nil || state == nil || state.Commit == "" {
		return false
	}
	if len(req.URLs) > 0 && !slices.Equal(state.URLs, req.URLs) {
		return false
	}
	if manager.CommitAt(root) != state.Commit {
		return false
	}
	if state.PatchHash != req.PatchHash || state.Submodules != req.Submodules {
		return false
	}
	if req.Commit != "" {
		return state.Commit == req.Commit
	}
	if req.Refresh {
		return false
	}
	return state.Ref == req.Ref && state.Version == req.Version
}

// localSourceNeedsMaterialization predicts whether finalizing a local SetGit
// package will touch its managed tree.
func (s *sourceStage) localSourceNeedsMaterialization(name string, node *resolver.PackageNode) bool {
	if node.Pkg == nil || len(node.Pkg.GitURLs()) == 0 {
		return false
	}
	version, ref, expected, err := s.resolvePackageSourceVersion(name, node)
	if err != nil {
		return true
	}
	req := repo.SourceRequest{
		Key:        repo.PackageTreeKey("", name),
		URLs:       node.Pkg.GitURLs(),
		Version:    version,
		Ref:        ref,
		Commit:     expected,
		PatchHash:  s.patchHashes[name],
		Submodules: node.Pkg.Submodules(),
	}
	if version == "" {
		req.Ref = ""
	}
	return !s.sourceRequestReady(req)
}

// ownGitSourceNeedsMaterialization predicts whether a native member with its
// own repository will materialize a fresh tree.
func (s *sourceStage) ownGitSourceNeedsMaterialization(name, sourceRoot string, node *resolver.PackageNode) bool {
	if node.Pkg == nil || len(node.Pkg.GitURLs()) == 0 {
		return false
	}
	version, ref, expected, err := s.resolvePackageSourceVersion(name, node)
	if err != nil {
		return true
	}
	req := repo.SourceRequest{
		Key:        name,
		Root:       sourceRoot,
		URLs:       node.Pkg.GitURLs(),
		Version:    version,
		Ref:        ref,
		Commit:     expected,
		PatchHash:  s.patchHashes[name],
		Submodules: node.Pkg.Submodules(),
	}
	if version == "" {
		req.Ref = ""
	}
	return !s.sourceRequestReady(req)
}

func (s *sourceStage) cloneLocalGitSources() error {
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		node := s.rc.ctx.DepGraph.Packages[name]
		if !s.conf.needed[name] || !node.IsLocal() || node.Pkg == nil {
			continue
		}
		if err := s.preparePackageWorkspace(name); err != nil {
			return fmt.Errorf("prepare workspace for %s: %w", name, err)
		}
	}
	return nil
}

func (s *sourceStage) setupSubPackageDirs(downloadLog *sourceDownloadLog) error {
	subParents := s.rc.ctx.Resolver.SubParents()
	if len(subParents) == 0 {
		return nil
	}

	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		rootParent, isSub := subParents[name]
		if !isSub {
			continue
		}
		if !s.conf.needed[name] {
			continue
		}
		node := s.rc.ctx.DepGraph.Packages[name]
		if node.Source == nil {
			continue
		}
		resolvedTools, err := s.rc.toolsForPackage(name)
		if err != nil {
			return err
		}

		relPath := strings.TrimPrefix(name, rootParent+"/")
		parentNode := s.rc.ctx.DepGraph.Packages[rootParent]
		version, commit, ok := remoteVersionKey(s.rc.ctx, rootParent, parentNode)
		if !ok && s.remote != nil {
			version = s.remote.versions[rootParent]
			commit = s.remote.commits[rootParent]
			ok = version != "" || commit != ""
		}
		if !ok {
			return fmt.Errorf("native parent %s has no resolved version", rootParent)
		}
		if recorded := s.remote.commits[rootParent]; recorded != "" {
			commit = recorded
		}
		tree := s.remote.trees[rootParent]
		if tree == "" {
			tree = remoteTreeDir(s.rc.ctx, rootParent)
		}
		s.remote.trees[rootParent] = tree
		s.remote.commits[rootParent] = commit
		s.remote.trees[name] = tree
		s.remote.commits[name] = commit
		s.sourceVersions[name] = version
		s.remote.setVersion(name, version)
		patchHash, err := patchHashForNode(name, node)
		if err != nil {
			return err
		}
		s.patchHashes[name] = patchHash
		sourceRoot := filepath.Join(tree, "src", filepath.FromSlash(relPath))
		if parentNode != nil && parentNode.Source != nil && parentNode.Source.Link != nil {
			sourceRoot = filepath.Join(parentNode.Source.Link.Path, filepath.FromSlash(relPath))
		}
		materializeRoot := sourceRoot
		if thirdPartyRoot := linkedThirdPartyRoot(s.rc.ctx, name, node); thirdPartyRoot != "" {
			materializeRoot = thirdPartyRoot
		}
		if s.ownGitSourceNeedsMaterialization(name, materializeRoot, node) {
			downloadLog.print()
		}
		if err := s.finalizePackageSource(name, materializeRoot, resolvedTools); err != nil {
			return err
		}
	}
	return nil
}

func (s *sourceStage) cloneSubPackageGitSources() error {
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		if _, isSub := s.rc.ctx.Resolver.SubParents()[name]; !isSub || !s.conf.needed[name] {
			continue
		}
		if err := s.preparePackageWorkspace(name); err != nil {
			return fmt.Errorf("prepare workspace for %s: %w", name, err)
		}
	}
	return nil
}

func (s *sourceStage) applyPatchesToNeeded() error {
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		node := s.rc.ctx.DepGraph.Packages[name]
		if !s.conf.needed[name] || node.Pkg == nil || node.Source == nil {
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
	if node == nil || node.Pkg == nil {
		return nil
	}
	if node.Source != nil && node.Source.Link != nil && pathWithin(node.Source.Link.Path, node.Pkg.SrcDir()) {
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

// treeRootFor maps a package to the tree root that owns its state file.
func (s *sourceStage) treeRootFor(name string) string {
	node := s.rc.ctx.DepGraph.Packages[name]
	if node == nil || node.Pkg == nil {
		return ""
	}
	if node.IsLocal() {
		return filepath.Join(s.rc.ctx.Paths.DepsDir, "local", name)
	}
	if _, isSub := s.rc.ctx.Resolver.SubParents()[name]; isSub {
		if root := linkedThirdPartyRoot(s.rc.ctx, name, node); root != "" {
			return root
		}
		if len(node.Pkg.GitURLs()) > 0 && s.pkgDirs[name] != nil {
			return s.pkgDirs[name].SourceDir
		}
		return remoteTreeDir(s.rc.ctx, remoteOwnerName(s.rc.ctx, name))
	}
	return remoteTreeDir(s.rc.ctx, name)
}

func (s *sourceStage) restoreKConfigs() error {
	return restoreKConfigFiles(s.rc.ctx, s.pkgDirs, s.conf.needed)
}

func (r *remoteVersionState) installedPkgs(pkgDirs map[string]*api.PkgDirs) map[string]*api.InstalledPackage {
	if len(pkgDirs) == 0 || r == nil {
		return nil
	}
	result := make(map[string]*api.InstalledPackage)
	for name, d := range pkgDirs {
		if d.InstallDir != "" {
			if version, ok := r.versions[name]; ok {
				result[name] = api.NewInstalledPackage(name, version, d.InstallDir, nil)
			}
		}
	}
	return result
}

func acquireStorageOwners(ctx *RuntimeContext, needed map[string]bool) error {
	if ctx.Locks == nil {
		return nil
	}
	parents := ctx.Resolver.SubParents()
	var owners []string
	for name := range needed {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.IsLocal() {
			continue
		}
		for parents[name] != "" {
			name = parents[name]
		}
		owners = append(owners, name)
	}
	return ctx.Locks.AcquireOwnersContext(ctx.Context, owners)
}
