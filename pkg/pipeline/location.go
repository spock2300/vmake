package pipeline

import (
	"path/filepath"

	"github.com/spock2300/vmake/pkg/resolver"
)

// packageLocation is the single derivation of a package's directories:
//
//	OwnerTreeRoot  <deps>/<owner>                  remote tree container
//	PackageRoot    directory supplying build.go    PkgDirs.SourceDir base
//	SourceTree     materialized source tree        Pkg.SrcDir() base
//	ThirdPartyRoot own-Git member tree root         (linked native subpackages)
//	TreeRoot       tree owning state.json/patch     (see treeRootFor)
type packageLocation struct {
	Name           string
	Owner          string
	Member         string
	OwnerTreeRoot  string
	PackageRoot    string
	SourceTree     string
	ThirdPartyRoot string
	TreeRoot       string
	Linked         bool
}

func (ctx *RuntimeContext) packageLocation(name string, node *resolver.PackageNode) packageLocation {
	loc := packageLocation{Name: name}
	if node == nil || node.Source == nil || ctx == nil || ctx.Paths == nil {
		return loc
	}
	loc.Owner = remoteOwnerName(ctx, name)
	loc.Member = remoteMemberPath(ctx, name)
	loc.OwnerTreeRoot = remoteTreeDir(ctx, loc.Owner)

	if link := node.Source.Link; link != nil {
		loc.Linked = true
		loc.PackageRoot = link.Path
		loc.SourceTree = link.Path
		loc.TreeRoot = loc.OwnerTreeRoot
		if link.Native && loc.Member != "" && node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 {
			loc.ThirdPartyRoot = filepath.Join(ctx.Paths.DepsDir, filepath.FromSlash(loc.Owner), "third_party", filepath.FromSlash(loc.Member))
			loc.SourceTree = filepath.Join(loc.ThirdPartyRoot, "src")
			loc.TreeRoot = loc.ThirdPartyRoot
		}
		return loc
	}

	if node.IsLocal() {
		loc.PackageRoot = node.Source.Dir
		loc.SourceTree = node.Source.Dir
		if node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 {
			loc.SourceTree = filepath.Join(node.Source.Dir, "src")
		}
		loc.TreeRoot = filepath.Join(ctx.Paths.DepsDir, "local", name)
		return loc
	}

	loc.PackageRoot = filepath.Join(loc.OwnerTreeRoot, "src", filepath.FromSlash(loc.Member))
	loc.SourceTree = loc.PackageRoot
	loc.TreeRoot = loc.OwnerTreeRoot
	if loc.Member != "" && node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 {
		loc.SourceTree = filepath.Join(loc.PackageRoot, "src")
		loc.TreeRoot = loc.PackageRoot
	}
	return loc
}

func packageSourceTree(ctx *RuntimeContext, name string, node *resolver.PackageNode) string {
	return ctx.packageLocation(name, node).SourceTree
}

func packageTreeRoot(ctx *RuntimeContext, name string, node *resolver.PackageNode) string {
	return ctx.packageLocation(name, node).TreeRoot
}

func packageThirdPartyRoot(ctx *RuntimeContext, name string, node *resolver.PackageNode) string {
	return ctx.packageLocation(name, node).ThirdPartyRoot
}
