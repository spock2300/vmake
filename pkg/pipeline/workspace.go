package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

// preparePackageWorkspace points a package at its single project-local working
// tree, materializing it when necessary.
func (s *buildPhaseState) preparePackageWorkspace(name string) error {
	node := s.ctx.DepGraph.Packages[name]
	dirs := s.pkgDirs[name]
	if node == nil || node.Pkg == nil || dirs == nil {
		return nil
	}
	_, isSub := s.ctx.Resolver.SubParents()[name]
	if !node.IsLocal() && !isSub {
		oldSourceDir := node.Pkg.SourceDir()
		node.Pkg.SetDirs(*dirs)
		return rebindRemoteSrcDir(s.ctx, node, dirs, oldSourceDir, name)
	}
	urls := node.Pkg.GitURLs()
	if len(urls) == 0 {
		oldSourceDir := node.Pkg.SourceDir()
		node.Pkg.SetDirs(*dirs)
		if !node.IsLocal() {
			return rebindRemoteSrcDir(s.ctx, node, dirs, oldSourceDir, name)
		}
		return nil
	}
	if err := s.selectPackageSource(name); err != nil {
		return err
	}
	oldSourceDir := node.Pkg.SourceDir()
	node.Pkg.SetDirs(*dirs)
	if node.IsLocal() {
		treeSrc := s.sourceSeeds[name]
		if treeSrc == "" {
			return fmt.Errorf("package %s has no materialized source tree", name)
		}
		link := filepath.Join(node.Source.Dir, "src")
		if err := fs.EnsureSymlink(link, treeSrc); err != nil {
			return err
		}
		node.Pkg.SetSrcDir(link)
		return nil
	}
	if err := rebindRemoteSrcDir(s.ctx, node, dirs, oldSourceDir, name); err != nil {
		return err
	}
	if node.Pkg.SrcDirRaw() == "" {
		node.Pkg.SetSrcDir(filepath.Join(dirs.SourceDir, "src"))
	}
	return nil
}

// rebindRemoteSrcDir keeps an explicit SrcDir mapping valid when the package
// tree moves: paths inside the old repository root are rebased onto the new
// one, paths outside are left untouched.
func rebindRemoteSrcDir(ctx *RuntimeContext, node *resolver.PackageNode, dirs *api.PkgDirs, oldSourceDir, name string) error {
	raw := node.Pkg.SrcDirRaw()
	if raw == "" || node.Source == nil {
		return nil
	}
	oldRoot := oldSourceDir
	if oldRoot == "" {
		oldRoot = node.Source.Dir
	}
	member := remoteMemberPath(ctx, name)
	rebased, err := rebaseSourcePath(raw, oldRoot, dirs.SourceDir, member, node.Source.Dir)
	if err != nil {
		return err
	}
	node.Pkg.SetSrcDir(rebased)
	return nil
}

// selectPackageSource materializes the package's own repository (local SetGit
// packages and native members with their own git URL) into its working tree.
func (s *buildPhaseState) selectPackageSource(name string) error {
	node := s.ctx.DepGraph.Packages[name]
	if node == nil || node.Pkg == nil || len(node.Pkg.GitURLs()) == 0 {
		return nil
	}
	if s.sourceCommits[name] != "" {
		return nil
	}
	if node.IsLocal() && hasUnmanagedSourceDir(node) {
		return fmt.Errorf("SetGit source %s already exists as a real directory; move it aside before creating a managed source link", filepath.Join(node.Source.Dir, "src"))
	}
	version, ref, expected, err := s.localSourceVersion(name, node)
	if err != nil {
		return err
	}
	dirs := s.pkgDirs[name]
	key := name
	root := ""
	if node.IsLocal() {
		key = repo.PackageTreeKey("", name)
	} else if dirs != nil {
		root = dirs.SourceDir
	}
	req := repo.SourceRequest{
		Key:        key,
		Root:       root,
		URLs:       node.Pkg.GitURLs(),
		Version:    version,
		Ref:        ref,
		Commit:     expected,
		PatchHash:  s.patchHashes[name],
		Submodules: node.Pkg.Submodules(),
		Refresh:    s.ctx.IgnoreLock,
	}
	if version == "" {
		req.Ref = ""
		req.Refresh = s.ctx.IgnoreLock || repo.IsLocalGitURL(req.URLs[0])
	}
	manager := repo.NewSourceManager(s.ctx.Paths.DepsDir, s.ctx.Paths.CacheDir).WithSession(s.ctx.Locks).WithContext(s.ctx.Context)
	res, err := manager.EnsureSource(req)
	if err != nil {
		return err
	}
	s.sourceSeeds[name] = res.SrcDir
	s.sourceCommits[name] = res.Commit
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

// remoteTreeDir is the single package tree root for a remote owner.
func remoteTreeDir(ctx *RuntimeContext, owner string) string {
	return filepath.Join(ctx.Paths.DepsDir, filepath.FromSlash(owner))
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
