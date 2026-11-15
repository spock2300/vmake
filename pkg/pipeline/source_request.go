package pipeline

import (
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

func (s *sourceStage) sourceRequestFor(name string, node *resolver.PackageNode, sourceRoot string) (repo.SourceRequest, error) {
	version, ref, expected, err := s.resolvePackageSourceVersion(name, node)
	if err != nil {
		return repo.SourceRequest{}, err
	}
	key := name
	root := sourceRoot
	if node.IsLocal() {
		key = repo.PackageTreeKey("", name)
		root = ""
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
		Refresh:    s.rc.ctx.isLockIgnoredFor(name),
	}
	if req.Version == "" && req.Commit == "" {
		req.Ref = ""
		if len(req.URLs) > 0 {
			req.Refresh = req.Refresh || repo.IsLocalGitURL(req.URLs[0])
		}
	}
	return req, nil
}
