package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

// preparePackageWorkspace points a package at its project-local working tree,
// materializing it when necessary.
func (s *sourceStage) preparePackageWorkspace(name string) error {
	node := s.rc.ctx.DepGraph.Packages[name]
	dirs := s.pkgDirs[name]
	if node == nil || node.Pkg == nil || dirs == nil {
		return nil
	}
	source := s.rc.ctx.packageSource(name, node)
	if source == nil {
		return fmt.Errorf("package %s has no source placement", name)
	}
	if node.HasOwnGit() && !source.Linked() {
		if err := s.selectPackageSourceRoot(name, source); err != nil {
			return err
		}
	}
	return source.Bind(node.Pkg, *dirs, s.sourceSeeds[name], s.dryRun)
}

// selectPackageSourceRoot materializes the package's own SetGit repository
// (local, registry, or native member) into the placement's source root.
func (s *sourceStage) selectPackageSourceRoot(name string, source *PackageSource) error {
	node := s.rc.ctx.DepGraph.Packages[name]
	if node == nil || !node.HasOwnGit() {
		return nil
	}
	if s.sourceCommits[name] != "" {
		return nil
	}
	if node.IsLocal() && hasUnmanagedSourceDir(node) {
		return fmt.Errorf("SetGit source %s already exists as a real directory; move it aside before creating a managed source link", filepath.Join(node.Source.Dir, "src"))
	}
	req, err := s.sourceRequestFor(name, node, source)
	if err != nil {
		return err
	}
	res, err := s.ensureSource(req)
	if err != nil {
		return err
	}
	s.sourceSeeds[name] = res.SrcDir
	s.sourceCommits[name] = res.Commit
	s.sourceVersions[name] = req.Version
	return nil
}

func (s *sourceStage) resolvePackageSourceVersion(name string, node *resolver.PackageNode) (string, string, string, error) {
	if node == nil || !node.HasOwnGit() {
		return "", "", "", nil
	}
	decision := s.rc.ctx.lockQuery().Sources().Decide(name, "", "")
	if decision.Provenance == resolver.ProvenanceTargetCommit {
		return "", decision.Commit, decision.Commit, nil
	}

	commitConstraint, semverConstraints, err := resolver.SplitConstraints(name, node.Constraints)
	if err != nil {
		return "", "", "", err
	}

	if len(node.Pkg.Versions()) == 0 {
		if decision.Provenance == resolver.ProvenanceTargetVersion || decision.Provenance == resolver.ProvenanceConfigPin {
			return "", "", "", fmt.Errorf("package %s declares no versions; specify a 40-character commit SHA instead of %q", name, decision.Version)
		}
		if decision.Provenance == resolver.ProvenanceLock && decision.Version == "" && decision.Commit != "" {
			return "", decision.Commit, decision.Commit, nil
		}
		if commitConstraint != "" {
			return "", commitConstraint, commitConstraint, nil
		}
		return "", "", "", nil
	}

	pin := ""
	switch decision.Provenance {
	case resolver.ProvenanceTargetVersion, resolver.ProvenanceConfigPin:
		if _, exists := node.Pkg.Versions()[decision.Version]; !exists {
			if decision.FromTarget {
				return "", "", "", fmt.Errorf("requested version %s for %s not found in declared versions", decision.Version, name)
			}
			return "", "", "", fmt.Errorf("config-pinned version %s for %s not found in declared versions", decision.Version, name)
		}
		pin = decision.Version
	case resolver.ProvenanceLock:
		if decision.Version != "" {
			pin = decision.Version
		} else if decision.Commit != "" {
			return "", decision.Commit, decision.Commit, nil
		}
	}

	if pin == "" && commitConstraint != "" {
		return "", commitConstraint, commitConstraint, nil
	}

	constraints := append([]string{}, semverConstraints...)
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
	if decision.Commit != "" && decision.Version == version {
		expected = decision.Commit
	}
	return version, ref, expected, nil
}

func existingLocalSourceCommit(node *resolver.PackageNode) (string, error) {
	if !node.HasOwnGit() {
		return "", nil
	}
	if node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource {
		return node.Source.Link.Commit, nil
	}
	if !DetectExistingSrcDir(node) {
		return "", nil
	}
	return repo.GetCurrentCommit(node.Pkg.SrcDir())
}

func (s *sourceStage) packageCommitKey(name, ownerCommit string) string {
	return sourceCommitKey(ownerCommit, s.sourceCommits[name])
}

func sourceCommitKey(ownerCommit, sourceCommit string) string {
	if sourceCommit != "" && sourceCommit != ownerCommit {
		if ownerCommit == "" {
			return sourceCommit
		}
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
