package api

import (
	"fmt"
	"strings"
)

// DepRef is the parsed form of a dependency reference accepted by AddDeps:
//
//	pkg:target  explicit target of another (or the same) package
//	pkg:*       all targets of a package
//	pkg/sub     package path (all targets)
//	target      target of the declaring package (no ":" or "/")
//
// A reference without ":" is a package path when it contains "/", otherwise
// it is a target of the declaring package.
type DepRef struct {
	Raw       string
	Pkg       string
	Target    string
	Wildcard  bool
	Qualified bool
}

// ParseDepRef validates and splits a dependency reference. It rejects empty
// refs, malformed segments (empty package/target), stray separators and
// spaces.
func ParseDepRef(ref string) (*DepRef, error) {
	if ref == "" {
		return nil, fmt.Errorf("empty dependency reference")
	}
	if strings.ContainsAny(ref, " \t\n") {
		return nil, fmt.Errorf("invalid dependency reference %q: whitespace not allowed", ref)
	}

	pkg, target, hasTarget := strings.Cut(ref, ":")
	if hasTarget {
		if pkg == "" {
			return nil, fmt.Errorf("invalid dependency reference %q: empty package before ':'", ref)
		}
		if strings.Contains(pkg, ":") || strings.Contains(target, ":") {
			return nil, fmt.Errorf("invalid dependency reference %q: multiple ':'", ref)
		}
		if target == "" {
			return nil, fmt.Errorf("invalid dependency reference %q: empty target after ':'", ref)
		}
		return &DepRef{Raw: ref, Pkg: pkg, Target: target, Wildcard: target == "*", Qualified: true}, nil
	}

	if strings.HasPrefix(ref, "/") || strings.HasSuffix(ref, "/") || strings.Contains(ref, "//") {
		return nil, fmt.Errorf("invalid dependency reference %q: malformed package path", ref)
	}
	return &DepRef{Raw: ref, Pkg: ref, Qualified: false}, nil
}
