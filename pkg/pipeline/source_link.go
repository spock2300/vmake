package pipeline

import (
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/pkg/resolver"
)

func linkedSourceDir(node *resolver.PackageNode) (string, bool) {
	if node == nil || node.Source == nil || node.Source.Link == nil {
		return "", false
	}
	return node.Source.Link.Path, true
}

func linkedThirdPartyRoot(ctx *RuntimeContext, name string, node *resolver.PackageNode) string {
	if node == nil || node.Source == nil || node.Source.Link == nil || !node.Source.Link.Native {
		return ""
	}
	if _, isSub := ctx.Resolver.SubParents()[name]; !isSub {
		return ""
	}
	if node.Pkg == nil || len(node.Pkg.GitURLs()) == 0 {
		return ""
	}
	owner := remoteOwnerName(ctx, name)
	member := remoteMemberPath(ctx, name)
	return filepath.Join(remoteTreeDir(ctx, owner), "third_party", filepath.FromSlash(member))
}

func linkedThirdPartySrcDir(ctx *RuntimeContext, name string, node *resolver.PackageNode) string {
	if root := linkedThirdPartyRoot(ctx, name, node); root != "" {
		return filepath.Join(root, "src")
	}
	return ""
}

// effectiveSourceDir is the directory that supplies build.go and headers. For
// linked packages this is the user worktree, not the managed .vmake_deps tree.
func effectiveSourceDir(ctx *RuntimeContext, name string, node *resolver.PackageNode) string {
	if link, ok := linkedSourceDir(node); ok {
		return link
	}
	if node == nil || node.Source == nil {
		return ""
	}
	if node.IsLocal() {
		if node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 {
			return filepath.Join(node.Source.Dir, "src")
		}
		return node.Source.Dir
	}
	owner := remoteOwnerName(ctx, name)
	member := remoteMemberPath(ctx, name)
	return filepath.Join(remoteTreeDir(ctx, owner), "src", filepath.FromSlash(member))
}

func pathWithin(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == path {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}
