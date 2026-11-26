package pipeline

import (
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

func (s *sourceStage) sourceRequestFor(name string, node *resolver.PackageNode, source *PackageSource) (repo.SourceRequest, error) {
	version, ref, expected, err := s.resolvePackageSourceVersion(name, node)
	if err != nil {
		return repo.SourceRequest{}, err
	}
	req := repo.SourceRequest{
		URLs:       node.Pkg.GitURLs(),
		Version:    version,
		Ref:        ref,
		Commit:     expected,
		PatchHash:  s.patchHashes[name],
		Submodules: node.Pkg.Submodules(),
		Refresh:    s.rc.ctx.isLockIgnoredFor(name),
	}
	source.ApplyRequest(&req)
	if req.Version == "" && req.Commit == "" {
		req.Ref = ""
		if len(req.URLs) > 0 {
			req.Refresh = req.Refresh || repo.IsLocalGitURL(req.URLs[0])
		}
	}
	return req, nil
}
