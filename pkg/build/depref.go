package build

import "strings"

// dependencyRef is the single parse of a target dependency string:
//
//	target          -> Package=target, HasTarget=false
//	pkg:target      -> Package=pkg, Target=target, HasTarget=true
//	pkg/sub:target  -> Package=pkg/sub
//	pkg/sub         -> Package=pkg/sub, HasTarget=false
//	pkg:*           -> Package=pkg, Target="*"
type dependencyRef struct {
	Package   string
	Target    string
	HasTarget bool
}

func parseDependencyRef(dep string) dependencyRef {
	if pkg, target, ok := strings.Cut(dep, ":"); ok {
		return dependencyRef{Package: pkg, Target: target, HasTarget: true}
	}
	return dependencyRef{Package: dep}
}

func (r dependencyRef) packagePath() bool {
	return strings.Contains(r.Package, "/")
}
