package pipeline

import (
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/pkg/resolver"
)

func linkedThirdPartyRoot(ctx *RuntimeContext, name string, node *resolver.PackageNode) string {
	return packageThirdPartyRoot(ctx, name, node)
}

func pathWithin(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == path {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}
