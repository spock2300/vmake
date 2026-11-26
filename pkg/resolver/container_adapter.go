package resolver

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/repo"
)

type containerAdapter interface {
	resolveContainer(r *Resolver, caller *ContainerNode, containerID string, constraints []string) (*ContainerNode, error)
	loadPackageSource(r *Resolver, container *ContainerNode, pkgID, relPath string) (*buildscript.Source, error)
	finalizePackageLink(r *Resolver, container *ContainerNode, pkgID string, src *buildscript.Source, pkg *api.Package) error
}

type localContainerAdapter struct{}
type registryContainerAdapter struct{}
type nativeContainerAdapter struct{}

func adapterForKind(kind SourceKind) containerAdapter {
	switch kind {
	case SourceKindLocal:
		return localContainerAdapter{}
	case SourceKindRegistry:
		return registryContainerAdapter{}
	case SourceKindNative:
		return nativeContainerAdapter{}
	default:
		return nil
	}
}

func externalAdapterForID(containerID string) containerAdapter {
	if !strings.Contains(containerID, "/") {
		return registryContainerAdapter{}
	}
	return nativeContainerAdapter{}
}

func (localContainerAdapter) resolveContainer(_ *Resolver, _ *ContainerNode, containerID string, _ []string) (*ContainerNode, error) {
	return nil, fmt.Errorf("local container %s is initialized from project root", containerID)
}

func (localContainerAdapter) loadPackageSource(_ *Resolver, container *ContainerNode, pkgID, relPath string) (*buildscript.Source, error) {
	src, err := buildscript.LoadPackageSource(container.Dir, api.LocalContainerID, relPath, api.SourceLocal)
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", pkgID, err)
	}
	return src, nil
}

func (localContainerAdapter) finalizePackageLink(r *Resolver, _ *ContainerNode, pkgID string, src *buildscript.Source, pkg *api.Package) error {
	pkgLinkPath, pkgLinked := r.sourceLink(pkgID)
	if !pkgLinked {
		return nil
	}
	if pkg == nil || len(pkg.GitURLs()) == 0 {
		return fmt.Errorf("local package %s does not declare SetGit and cannot be linked", pkgID)
	}
	linkCommit, err := repo.GetCurrentCommitContext(r.ctx, pkgLinkPath)
	if err != nil {
		return fmt.Errorf("read linked source %s: %w", pkgLinkPath, err)
	}
	src.Link = &buildscript.SourceLink{
		Path:        pkgLinkPath,
		Commit:      linkCommit,
		LocalSource: true,
	}
	return nil
}

func (registryContainerAdapter) resolveContainer(r *Resolver, _ *ContainerNode, repoName string, _ []string) (*ContainerNode, error) {
	if r.repoMgr == nil {
		return nil, fmt.Errorf("resolve container %s: no repository manager configured", repoName)
	}
	return r.resolveRegistryContainer(repoName)
}

func (registryContainerAdapter) loadPackageSource(r *Resolver, container *ContainerNode, pkgID, relPath string) (*buildscript.Source, error) {
	if relPath == "" {
		return nil, fmt.Errorf("registry container %s requires a package name (<repo>/<pkg>)", container.ID)
	}
	buildGo, err := r.repoMgr.FindPackageGo(container.Repo, relPath)
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", pkgID, err)
	}
	dir := filepath.Dir(buildGo)
	scriptOpts, err := buildscript.DetectScriptOptions(dir, container.Dir)
	if err != nil {
		return nil, err
	}
	src := buildscript.NewSource(pkgID, buildGo, dir, api.SourceRemote)
	src.Container = container.ID
	src.RelPath = relPath
	src.Repo = container.Repo
	src.Commit = container.Commit
	src.Script = scriptOpts
	if linkPath, linked := r.sourceLink(pkgID); linked {
		linkCommit, headErr := repo.GetCurrentCommitContext(r.ctx, linkPath)
		if headErr != nil {
			return nil, fmt.Errorf("read linked source %s: %w", linkPath, headErr)
		}
		src.Link = &buildscript.SourceLink{
			Path:        linkPath,
			Commit:      linkCommit,
			LocalSource: true,
		}
	}
	return src, nil
}

func (registryContainerAdapter) finalizePackageLink(_ *Resolver, _ *ContainerNode, _ string, _ *buildscript.Source, _ *api.Package) error {
	return nil
}

func (nativeContainerAdapter) resolveContainer(r *Resolver, caller *ContainerNode, containerID string, constraints []string) (*ContainerNode, error) {
	repoName, modName, _ := api.SplitPackageRef(containerID)
	if r.repoMgr != nil && r.repoMgr.Exists(repoName) {
		if err := r.lockRepo(repoName); err != nil {
			return nil, err
		}
		if !r.repoMgr.IsNative(repoName) {
			return nil, fmt.Errorf("container %q: repository %q is a registry repository, not a native repository; declare %q in vmake.json \"modules\" and put package version constraints in \"sources\"", containerID, repoName, repoName)
		}
		return r.resolveNativeContainer(containerID, repoName, modName, constraints)
	}
	if tmpl := r.manifestNativeRepoURL(caller, repoName); tmpl != "" {
		return r.resolveNativeContainerWithURL(containerID, repoName, modName, tmpl, tmpl, constraints)
	}
	if r.repoMgr == nil {
		return nil, fmt.Errorf("resolve container %s: no repository manager configured", containerID)
	}
	return nil, fmt.Errorf("repository '%s' not found (for container %s); register it with 'vmake repo add --native %s <url>' or declare it under \"repos\" in vmake.json", repoName, containerID, repoName)
}

func (nativeContainerAdapter) loadPackageSource(_ *Resolver, container *ContainerNode, pkgID, relPath string) (*buildscript.Source, error) {
	origin := api.SourceRemote
	if container.IsLocalPath() && !container.DeclaredByRemote {
		origin = api.SourceLocal
	}
	src, err := buildscript.LoadPackageSource(container.Dir, container.ID, relPath, origin)
	if err != nil {
		if relPath == "" {
			return nil, fmt.Errorf("container %s has no root build.go (in %s); require a concrete package path like %s/<path>", container.ID, container.Dir, container.ID)
		}
		return nil, fmt.Errorf("find %s in container %s: %w", pkgID, container.ID, err)
	}
	src.Name = pkgID
	src.Repo = container.Repo
	src.RepoURL = container.RepoURL
	src.URL = container.URL
	src.Commit = container.Commit
	if container.Link != nil {
		src.Link = &buildscript.SourceLink{
			Path:        src.Dir,
			Commit:      container.Link.Commit,
			Native:      true,
			LocalSource: true,
		}
	}
	return src, nil
}

func (nativeContainerAdapter) finalizePackageLink(r *Resolver, container *ContainerNode, pkgID string, src *buildscript.Source, pkg *api.Package) error {
	hasOwnGit := pkg != nil && len(pkg.GitURLs()) > 0
	pkgLinkPath, pkgLinked := r.sourceLink(pkgID)
	if pkgID == container.ID {
		pkgLinked = false
	}
	if hasOwnGit {
		if pkgLinked {
			linkCommit, err := repo.GetCurrentCommitContext(r.ctx, pkgLinkPath)
			if err != nil {
				return fmt.Errorf("read linked source %s: %w", pkgLinkPath, err)
			}
			src.Link = &buildscript.SourceLink{
				Path:        pkgLinkPath,
				Commit:      linkCommit,
				Native:      container.Link != nil,
				LocalSource: true,
			}
		} else if container.Link != nil {
			src.Link = &buildscript.SourceLink{
				Path:        src.Dir,
				Commit:      container.Link.Commit,
				Native:      true,
				LocalSource: false,
			}
		}
	} else if pkgLinked {
		return fmt.Errorf("package %s does not declare SetGit; link its container %s instead", pkgID, container.ID)
	}
	return nil
}
