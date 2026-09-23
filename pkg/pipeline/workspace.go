package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

func (s *buildPhaseState) preparePackageWorkspace(name string) error {
	node := s.ctx.DepGraph.Packages[name]
	dirs := s.pkgDirs[name]
	if node == nil || node.Pkg == nil || dirs == nil {
		return nil
	}
	manager := repo.NewSourceManager(s.ctx.Paths.DepsDir, s.ctx.Paths.CacheDir).WithSession(s.ctx.Locks).WithContext(s.ctx.Context)
	if !node.IsLocal() {
		owner := remoteOwnerName(s.ctx, name)
		versionDir := s.remote.versionDirs[name]
		if versionDir == "" {
			versionDir = s.remote.versionDirs[owner]
		}
		if err := prepareRemoteWorkspace(manager, node, dirs, versionDir, s.remote.commits[owner], s.patchHashes[name]); err != nil {
			return err
		}
	}
	_, isSub := s.ctx.Resolver.SubParents()[name]
	if !node.IsLocal() && !isSub {
		return nil
	}
	urls := node.Pkg.GitURLs()
	if len(urls) == 0 {
		if isSub {
			return fs.EnsureSymlink(nativeMemberSourceLink(s.ctx, name), dirs.SourceDir)
		}
		return nil
	}
	if err := s.selectPackageSource(name); err != nil {
		return err
	}
	seed := s.sourceSeeds[name]
	work := filepath.Join(dirs.SourceDir, "src")
	if node.IsLocal() {
		work = filepath.Join(dirs.BuildDir, "work", "src")
	} else if _, err := os.Stat(work); err == nil {
		if _, err := os.Stat(work + ".vmake-workspace"); err != nil {
			return fmt.Errorf("SetGit source %s already exists and is not a managed workspace", work)
		}
	}
	patchHash, err := patchHashForNode(name, node)
	if err != nil {
		return err
	}
	if err := manager.EnsureWorkspace(seed, work, s.sourceCommits[name]+"\x00"+patchHash); err != nil {
		return err
	}
	if node.IsLocal() {
		link := filepath.Join(node.Source.Dir, "src")
		if err := fs.EnsureSymlink(link, work); err != nil {
			return err
		}
		work = link
	} else if err := fs.EnsureSymlink(nativeMemberSourceLink(s.ctx, name), work); err != nil {
		return err
	}
	node.Pkg.SetDirs(*dirs)
	node.Pkg.SetSrcDir(work)
	return nil
}

func (s *buildPhaseState) selectPackageSource(name string) error {
	node := s.ctx.DepGraph.Packages[name]
	if node == nil || node.Pkg == nil || len(node.Pkg.GitURLs()) == 0 {
		return nil
	}
	if s.sourceSeeds == nil {
		s.sourceSeeds = make(map[string]string)
		s.sourceCommits = make(map[string]string)
	}
	if s.sourceVersions == nil {
		s.sourceVersions = make(map[string]string)
	}
	if s.sourceSeeds[name] != "" {
		return nil
	}
	if node.IsLocal() && hasUnmanagedSourceDir(node) {
		return fmt.Errorf("SetGit source %s already exists as a real directory; move it aside before creating a managed source link", filepath.Join(node.Source.Dir, "src"))
	}
	version, ref, expected, err := s.localSourceVersion(name, node)
	if err != nil {
		return err
	}
	manager := repo.NewSourceManager(s.ctx.Paths.DepsDir, s.ctx.Paths.CacheDir).WithSession(s.ctx.Locks).WithContext(s.ctx.Context)
	var seed string
	if version != "" {
		seed, err = manager.EnsureURLRef(node.Pkg.GitURLs()[0], ref, expected, s.ctx.IgnoreLock)
	} else {
		seed, err = manager.EnsureURL(node.Pkg.GitURLs()[0])
	}
	if err != nil {
		return err
	}
	commit, err := repo.GetCurrentCommitContext(s.ctx.Context, seed)
	if err != nil {
		return err
	}
	s.sourceSeeds[name], s.sourceCommits[name] = seed, commit
	s.sourceVersions[name] = version
	return nil
}

func (s *buildPhaseState) localSourceVersion(name string, node *resolver.PackageNode) (string, string, string, error) {
	if !node.IsLocal() || len(node.Pkg.Versions()) == 0 {
		return "", "", "", nil
	}
	pin := config.GetEntry(s.ctx.Config, name).Version
	if pin == "" {
		pin, _ = s.lockedVersion(name)
	}
	constraints := append([]string{}, node.Constraints...)
	if pin != "" {
		constraints = append(constraints, "="+pin)
	}
	version, err := node.Pkg.SelectVersionMulti(constraints)
	if err != nil {
		return "", "", "", fmt.Errorf("select source version for %s: %w", name, err)
	}
	ref := node.Pkg.GetRef(version)
	if ref == "" {
		return "", "", "", fmt.Errorf("source version %s for %s has no git ref", version, name)
	}
	expected := ""
	if s.ctx.Lock != nil && !s.ctx.IgnoreLock {
		if locked, ok := s.ctx.Lock.Get(name); ok && locked.Version == version {
			expected = locked.Commit
		}
	}
	return version, ref, expected, nil
}

func existingLocalSourceCommit(node *resolver.PackageNode) (string, error) {
	if node.Pkg == nil || len(node.Pkg.GitURLs()) == 0 || !DetectExistingSrcDir(node) {
		return "", nil
	}
	return repo.GetCurrentCommit(node.Pkg.SrcDir())
}

func (s *buildPhaseState) packageCommitKey(name, ownerCommit string) string {
	return sourceCommitKey(ownerCommit, s.sourceCommits[name])
}

func sourceCommitKey(ownerCommit, sourceCommit string) string {
	if sourceCommit != "" {
		return ownerCommit + "\x00" + sourceCommit
	}
	return ownerCommit
}

func remoteVersionDir(ctx *RuntimeContext, owner, version string) string {
	return filepath.Join(storage.CacheDir(ctx.Paths.CacheDir), filepath.FromSlash(owner), version)
}

func nativeMemberSourceLink(ctx *RuntimeContext, name string) string {
	return filepath.Join(ctx.Paths.DepsDir, filepath.FromSlash(remoteOwnerName(ctx, name)), "_members", storage.OwnerKey(filepath.ToSlash(remoteMemberPath(ctx, name))), "src")
}

func prepareRemoteWorkspace(manager *repo.SourceManager, node *resolver.PackageNode, dirs *api.PkgDirs, versionDir, commit, patchHash string) error {
	workRoot := filepath.Join(filepath.Dir(dirs.BuildDir), "work", "repo")
	member, err := filepath.Rel(workRoot, dirs.SourceDir)
	if err != nil {
		return fmt.Errorf("resolve source member for %s: %w", node.Source.Name, err)
	}
	if _, err := sourceRepositoryRoot(dirs.SourceDir, member); err != nil {
		return fmt.Errorf("resolve source member for %s: %w", node.Source.Name, err)
	}
	var sourceDir string
	if node.Pkg != nil && node.Pkg.SrcDirRaw() != "" {
		oldRoot := node.Pkg.SourceDir()
		if oldRoot == "" {
			oldRoot = node.Source.Dir
		}
		sourceDir, err = rebaseSourcePath(node.Pkg.SrcDirRaw(), oldRoot, dirs.SourceDir, member, node.Source.Dir)
		if err != nil {
			return fmt.Errorf("rebase source for %s: %w", node.Source.Name, err)
		}
	}
	if err := manager.EnsureWorkspace(filepath.Join(versionDir, "src"), workRoot, commit+"\x00"+patchHash); err != nil {
		return err
	}
	if node.Pkg != nil {
		if sourceDir != "" {
			node.Pkg.SetSrcDir(sourceDir)
		}
		node.Pkg.SetDirs(*dirs)
	}
	return nil
}

func rebaseSourcePath(path, oldRoot, newRoot, member string, otherRoots ...string) (string, error) {
	newRepo, err := sourceRepositoryRoot(newRoot, member)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		if filepath.VolumeName(path) != "" {
			return "", fmt.Errorf("source path must be absolute or relative to its package: %s", path)
		}
		if _, err := sourceRepositoryRoot(oldRoot, member); err != nil {
			return "", err
		}
		path = filepath.Join(oldRoot, path)
	}
	for _, root := range append([]string{oldRoot}, otherRoots...) {
		oldRepo, err := sourceRepositoryRoot(root, member)
		if err != nil {
			return "", err
		}
		roots := []string{oldRepo}
		if resolved, err := filepath.EvalSymlinks(oldRepo); err == nil {
			if resolved != oldRepo {
				roots = append(roots, resolved)
			}
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolve source repository %s: %w", oldRepo, err)
		}
		for _, candidate := range roots {
			if !strings.EqualFold(filepath.VolumeName(candidate), filepath.VolumeName(path)) {
				continue
			}
			rel, err := filepath.Rel(candidate, path)
			if err != nil {
				return "", fmt.Errorf("rebase source path %s from %s: %w", path, candidate, err)
			}
			if filepath.IsLocal(rel) {
				return filepath.Join(newRepo, rel), nil
			}
		}
	}
	return path, nil
}

func sourceRepositoryRoot(sourceDir, member string) (string, error) {
	if !filepath.IsAbs(sourceDir) {
		return "", fmt.Errorf("package source directory must be absolute: %s", sourceDir)
	}
	member = filepath.Clean(member)
	if !filepath.IsLocal(member) {
		return "", fmt.Errorf("source member must stay inside its repository: %s", member)
	}
	root := filepath.Clean(sourceDir)
	if member != "." {
		for range strings.Split(member, string(filepath.Separator)) {
			root = filepath.Dir(root)
		}
	}
	if filepath.Join(root, member) != filepath.Clean(sourceDir) {
		return "", fmt.Errorf("package source directory %s does not match repository member %s", sourceDir, member)
	}
	return root, nil
}
