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
	"github.com/spock2300/vmake/pkg/config"
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
	remote := &remoteVersionState{
		entries: make(map[string]*config.EntryConfig),
		commits: make(map[string]string),
		trees:   make(map[string]string),
	}
	downloadLog := &sourceDownloadLog{}

	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		if !s.conf.needed[name] {
			continue
		}
		if !s.rc.ctx.DepGraph.Packages[name].IsLocal() {
			remote.entries[name] = config.GetEntry(s.rc.ctx.Config, name)
		}
	}

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

func (s *sourceStage) writeLockfile() error {
	if s.rc.ctx.LockPath == "" {
		return nil
	}
	updated := lockfile.New()
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		node := s.rc.ctx.DepGraph.Packages[name]
		if node.IsLocal() {
			if version := s.sourceVersions[name]; version != "" {
				updated.Set(name, &lockfile.LockedPkg{Version: version, Commit: s.sourceCommits[name], Source: "git"})
			} else if node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 && len(node.Pkg.Versions()) > 0 && s.rc.ctx.Lock != nil {
				if old, ok := s.rc.ctx.Lock.Get(name); ok && old.Source == "git" {
					updated.Set(name, old)
				}
			}
			continue
		}
		if _, isSub := s.rc.ctx.Resolver.SubParents()[name]; isSub {
			continue
		}
		entry := s.remote.entries[name]
		if entry == nil || entry.Version == "" {
			if s.rc.ctx.Lock != nil {
				if old, ok := s.rc.ctx.Lock.Get(name); ok {
					updated.Set(name, old)
				}
			}
			continue
		}
		source := "registry"
		if node.Native != nil {
			source = "native"
		}
		locked := &lockfile.LockedPkg{
			Version: entry.Version,
			Commit:  s.remote.commits[name],
			Source:  source,
		}
		if source == "registry" {
			wrapperCommit, err := s.registryWrapperCommit(name)
			if err != nil {
				return fmt.Errorf("pin wrapper commit for %s: %w", name, err)
			}
			locked.WrapperCommit = wrapperCommit
		}
		updated.Set(name, locked)
	}
	if s.rc.ctx.Lock != nil {
		for _, name := range s.rc.ctx.Lock.SortedNames() {
			if _, ok := updated.Get(name); !ok {
				vlog.Verbose("  lock: dropped stale entry %s", name)
			}
		}
	}
	if len(updated.Packages) == 0 {
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
	sourceMgr := repo.NewSourceManager(depsDir, s.rc.ctx.Paths.CacheDir).WithSession(s.rc.ctx.Locks).WithContext(s.rc.ctx.Context)
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
		entryCfg := remote.entries[name]
		repoName, pkgName, ok := api.SplitPackageRef(name)
		if !ok {
			return fmt.Errorf("invalid package ref %q", name)
		}
		pkg := api.NewPackage().SetRepo(repoName).SetName(pkgName)
		if node.IsNative() {
			if entryCfg.Version == "" {
				entryCfg.Version = node.Native.Selected
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

		if locked, isLocked := s.lockedVersion(name); isLocked && entryCfg.Version == "" {
			entryCfg.Version = locked
		}
		if entryCfg.Version == "" && len(pkg.GetVersions()) > 0 {
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
			entryCfg.Version = selected
		}
		expectedCommit := ""
		if s.rc.ctx.Lock != nil && !s.rc.ctx.IgnoreLock {
			if locked, ok := s.rc.ctx.Lock.Get(name); ok && locked.Commit != "" && locked.Version == entryCfg.Version {
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
		if entryCfg.Version != "" {
			ref = pkg.GetRef(entryCfg.Version)
			if ref == "" {
				ref = entryCfg.Version
			}
		}
		req := repo.SourceRequest{
			Key:        name,
			URLs:       pkg.GitURLs(),
			Version:    entryCfg.Version,
			Ref:        ref,
			Commit:     expectedCommit,
			PatchHash:  treePatchHash,
			Submodules: pkg.Submodules(),
		}
		if !s.sourceRequestReady(req) {
			downloadLog.print()
		}
		res, err := sourceMgr.EnsureSource(req)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", name, err)
		}
		remote.commits[name] = res.Commit
		remote.trees[name] = res.Root
		s.sourceVersions[name] = entryCfg.Version
		s.patchHashes[name] = patchHash
		vlog.Verbose("  %s@%s -> %s", name, entryCfg.Version, res.SrcDir)
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

func (s *sourceStage) lockedVersion(name string) (string, bool) {
	if s.rc.ctx.Lock == nil || s.rc.ctx.IgnoreLock {
		return "", false
	}
	if locked, ok := s.rc.ctx.Lock.Get(name); ok && locked.Version != "" {
		return locked.Version, true
	}
	return "", false
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
	return state.Ref == req.Ref && state.Version == req.Version
}

// localSourceNeedsMaterialization predicts whether finalizing a local SetGit
// package will touch its managed tree.
func (s *sourceStage) localSourceNeedsMaterialization(name string, node *resolver.PackageNode) bool {
	if node.Pkg == nil || len(node.Pkg.GitURLs()) == 0 {
		return false
	}
	version, ref, expected, err := s.localSourceVersion(name, node)
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
	return !s.sourceRequestReady(repo.SourceRequest{
		Key:        name,
		Root:       sourceRoot,
		URLs:       node.Pkg.GitURLs(),
		PatchHash:  s.patchHashes[name],
		Submodules: node.Pkg.Submodules(),
	})
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
		parentEntry := config.GetEntry(s.rc.ctx.Config, rootParent)
		version, commit, ok := remoteVersionKey(s.rc.ctx, rootParent, parentNode, parentEntry)
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
		patchHash, err := patchHashForNode(name, node)
		if err != nil {
			return err
		}
		s.patchHashes[name] = patchHash
		sourceRoot := filepath.Join(tree, "src", filepath.FromSlash(relPath))
		if s.ownGitSourceNeedsMaterialization(name, sourceRoot, node) {
			downloadLog.print()
		}
		if err := s.finalizePackageSource(name, sourceRoot, resolvedTools); err != nil {
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
	if err := applyPatchesContext(s.rc.ctx.Context, node.Pkg, node.Pkg.SrcDir()); err != nil {
		s.clearPatchState(name)
		return fmt.Errorf("apply patches for %s: %w", name, err)
	}
	return nil
}

func (s *sourceStage) clearPatchState(name string) {
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
	if len(pkgDirs) == 0 {
		return nil
	}
	result := make(map[string]*api.InstalledPackage)
	for name, d := range pkgDirs {
		if d.InstallDir != "" {
			if rc, ok := r.entries[name]; ok {
				result[name] = api.NewInstalledPackage(name, rc.Version, d.InstallDir, nil)
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
