package repo

import (
	"fmt"

	"gitee.com/spock2300/vmake/pkg/api"
)

type PackageDef struct {
	Repo        string
	Name        string
	GitURL      string
	Homepage    string
	Description string
	License     string
	Versions    map[string]string
	Package     *api.Package
}

func NewPackageDef(repo, name string) *PackageDef {
	return &PackageDef{
		Repo:     repo,
		Name:     name,
		Versions: make(map[string]string),
	}
}

func (p *PackageDef) SetGit(url string) *PackageDef {
	p.GitURL = url
	return p
}

func (p *PackageDef) SetPackage(pkg *api.Package) *PackageDef {
	p.Package = pkg
	p.GitURL = pkg.Git()
	p.Homepage = pkg.Homepage()
	p.Description = pkg.Description()
	p.License = pkg.License()
	p.Versions = pkg.Versions()
	return p
}

func (p *PackageDef) GetRef(version string) string {
	if ref, ok := p.Versions[version]; ok {
		return ref
	}
	return ""
}

func (p *PackageDef) GetVersions() []string {
	versions := make([]string, 0, len(p.Versions))
	for v := range p.Versions {
		versions = append(versions, v)
	}
	return versions
}

func (p *PackageDef) SelectVersion(constraint string) (string, error) {
	versions := p.GetVersions()
	if len(versions) == 0 {
		return "", fmt.Errorf("no versions available for %s", p.FullName())
	}

	version, ok := MatchVersion(versions, constraint)
	if !ok {
		return "", fmt.Errorf("no version matching %s for %s (available: %v)", constraint, p.FullName(), versions)
	}
	return version, nil
}

func (p *PackageDef) FullName() string {
	return p.Repo + "/" + p.Name
}
