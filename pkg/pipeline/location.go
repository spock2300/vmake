package pipeline

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

// packageLocation is the single derivation of a package's directories:
//
//	Container       owning container ID ("//", "<repo>/<mod>", "<repo>")
//	Owner           remote tree key (<repo>/<mod> for native, <repo>/<pkg> for registry)
//	Member          relative path inside container ("", "firmware", "system/...")
//	OwnerTreeRoot   <deps>/<owner>                remote tree container
//	PackageRoot     directory supplying build.go  PkgDirs.SourceDir base
//	SourceTree      materialized source tree      Pkg.SrcDir() base
//	ThirdPartyRoot  own-Git member tree root      (<owner>/third_party/<memberDir>)
//	TreeRoot        tree owning state.json/patch  (see treeRootFor)
//	Linked          true when SourceTree is directly user-linked
//	ContainerLinked true when the native container is user-linked
type packageLocation struct {
	Name            string
	Container       string
	Owner           string
	Member          string
	OwnerTreeRoot   string
	PackageRoot     string
	SourceTree      string
	ThirdPartyRoot  string
	TreeRoot        string
	Linked          bool
	ContainerLinked bool
}

func (ctx *RuntimeContext) packageLocation(name string, node *resolver.PackageNode) packageLocation {
	loc := packageLocation{Name: name}
	if node == nil || node.Source == nil || ctx == nil || ctx.Paths == nil {
		return loc
	}

	if node.IsLocal() {
		rel := node.RelPath
		if rel == "" {
			rel, _ = api.LocalRelPath(name)
		}
		loc.Container = api.LocalContainerID
		loc.Member = rel
		loc.PackageRoot = node.Source.Dir
		loc.SourceTree = node.Source.Dir
		if node.HasOwnGit() {
			if node.Source.Link != nil && node.Source.Link.LocalSource {
				loc.Linked = true
				loc.SourceTree = node.Source.Link.Path
			} else {
				loc.TreeRoot = filepath.Join(ctx.Paths.DepsDir, "local", build.MemberDir(rel))
				loc.SourceTree = filepath.Join(node.Source.Dir, "src")
			}
		}
		return loc
	}

	if node.IsNative() {
		containerID := node.Container
		if containerID == "" {
			containerID = name
		}
		loc.Container = containerID
		loc.Owner = containerID
		loc.Member = node.RelPath
		loc.OwnerTreeRoot = remoteTreeDir(ctx, loc.Owner)

		var c *resolver.ContainerNode
		if ctx.DepGraph != nil {
			c = ctx.DepGraph.Containers[containerID]
		}
		containerLinked := (c != nil && c.IsLinked()) || (node.Source.Link != nil && node.Source.Link.Native)
		loc.ContainerLinked = containerLinked
		if containerLinked {
			loc.PackageRoot = node.Source.Dir
		} else {
			loc.PackageRoot = filepath.Join(loc.OwnerTreeRoot, "src", filepath.FromSlash(loc.Member))
		}

		if node.HasOwnGit() {
			if node.Source.Link != nil && node.Source.Link.LocalSource {
				loc.Linked = true
				loc.SourceTree = node.Source.Link.Path
				loc.TreeRoot = loc.OwnerTreeRoot
			} else {
				loc.ThirdPartyRoot = filepath.Join(loc.OwnerTreeRoot, "third_party", build.MemberDir(loc.Member))
				loc.SourceTree = filepath.Join(loc.ThirdPartyRoot, "src")
				loc.TreeRoot = loc.ThirdPartyRoot
			}
		} else {
			loc.SourceTree = loc.PackageRoot
			loc.TreeRoot = loc.OwnerTreeRoot
			if containerLinked {
				loc.Linked = true
			}
		}
		return loc
	}

	// Registry package: wrapper build.go in ~/.vmake/repos/<repo>, upstream source under <deps>/<repo>/<pkg>.
	containerID := node.Container
	if containerID == "" {
		repoName, _, _ := api.SplitPackageRef(name)
		containerID = repoName
	}
	loc.Container = containerID
	loc.Owner = name
	loc.Member = ""
	loc.OwnerTreeRoot = remoteTreeDir(ctx, name)
	if node.Source.Link != nil && node.Source.Link.LocalSource {
		loc.Linked = true
		loc.PackageRoot = node.Source.Link.Path
		loc.SourceTree = node.Source.Link.Path
		loc.TreeRoot = loc.OwnerTreeRoot
		return loc
	}
	loc.PackageRoot = filepath.Join(loc.OwnerTreeRoot, "src")
	loc.SourceTree = loc.PackageRoot
	loc.TreeRoot = loc.OwnerTreeRoot
	return loc
}

func packageSourceTree(ctx *RuntimeContext, name string, node *resolver.PackageNode) string {
	return ctx.packageLocation(name, node).SourceTree
}

func packageTreeRoot(ctx *RuntimeContext, name string, node *resolver.PackageNode) string {
	return ctx.packageLocation(name, node).TreeRoot
}

func managedThirdPartyRoot(sourceMgr *repo.SourceManager, name string) (string, bool) {
	repoName, rest, ok := api.SplitPackageRef(name)
	if !ok {
		return "", false
	}
	mod, member, hasMember := strings.Cut(rest, "/")
	if !hasMember {
		return "", false
	}
	ownerRoot := sourceMgr.TreeDir(repo.PackageTreeKey(repoName, mod))
	return filepath.Join(ownerRoot, "third_party", build.MemberDir(member)), true
}

func CleanLinkedMaterializedSource(sourceMgr *repo.SourceManager, projectDir, name string) error {
	if rel, ok := api.LocalRelPath(name); ok {
		pkgDir := projectDir
		if rel != "" {
			pkgDir = filepath.Join(projectDir, filepath.FromSlash(rel))
		}
		linkPath := filepath.Join(pkgDir, "src")
		if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(linkPath)
		}
		return sourceMgr.CleanMaterializedSource("", build.MemberDir(rel))
	}
	if thirdPartyRoot, ok := managedThirdPartyRoot(sourceMgr, name); ok {
		return sourceMgr.CleanMaterializedSourceAt(thirdPartyRoot)
	}
	repoName, rest, ok := api.SplitPackageRef(name)
	if !ok {
		return nil
	}
	return sourceMgr.CleanMaterializedSource(repoName, rest)
}
