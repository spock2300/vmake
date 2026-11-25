package api

import (
	"fmt"
	"strings"
)

type DepRef struct {
	Raw       string
	Pkg       string
	Target    string
	Wildcard  bool
	Qualified bool
}

func ParseDepRef(ref string) (*DepRef, error) {
	if ref == "" {
		return nil, fmt.Errorf("empty dependency reference")
	}
	if strings.ContainsAny(ref, " \t\r\n") {
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
		if err := ValidatePackageRef(pkg); err != nil {
			return nil, fmt.Errorf("invalid dependency reference %q: %w", ref, err)
		}
		return &DepRef{Raw: ref, Pkg: pkg, Target: target, Wildcard: target == "*", Qualified: true}, nil
	}

	if ref == LocalContainerID || strings.HasPrefix(ref, LocalContainerID) || strings.Contains(ref, "/") {
		if err := ValidatePackageRef(ref); err != nil {
			return nil, fmt.Errorf("invalid dependency reference %q: %w", ref, err)
		}
		return &DepRef{Raw: ref, Pkg: ref, Qualified: false}, nil
	}

	return &DepRef{Raw: ref, Pkg: ref, Qualified: false}, nil
}
