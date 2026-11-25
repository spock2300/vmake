package build

import (
	"strings"

	"github.com/spock2300/vmake/pkg/api"
)

type DependencyRef struct {
	Package   string
	Target    string
	HasTarget bool
}

func ParseDependencyRef(dep string) DependencyRef {
	if pkg, target, ok := strings.Cut(dep, ":"); ok {
		return DependencyRef{Package: pkg, Target: target, HasTarget: true}
	}
	return DependencyRef{Package: dep}
}

func (r DependencyRef) IsPackagePath() bool {
	return api.IsLocalPackageID(r.Package) || strings.Contains(r.Package, "/")
}
