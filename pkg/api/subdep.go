package api

import "strings"

// ResolveSubPackageName resolves a short dependency name relative to
// currentPkg, walking up to its native owner. subParents maps every discovered
// sub-package to that owner (not necessarily its direct directory parent).
// exists is supplied by the caller because the authoritative package set
// differs by phase (resolved sources, build graph, or prepared directories).
// Names containing "/" are absolute and pass through unchanged.
func ResolveSubPackageName(currentPkg, depName string, subParents map[string]string, exists func(string) bool) string {
	if c := SubPackageCandidates(currentPkg, depName, subParents); c != nil {
		for _, candidate := range c {
			if exists(candidate) {
				return candidate
			}
		}
		return depName
	}
	if !strings.Contains(depName, "/") && len(subParents) > 0 {
		if unique := uniqueSubPackageByLeaf(depName, "", subParents); unique != "" {
			if !exists(depName) && exists(unique) {
				return unique
			}
		}
	}
	return depName
}

// SubPackageCandidates returns the nearest-first candidate names for a short
// dependency; nil means depName is absolute or currentPkg has no native owner.
func SubPackageCandidates(currentPkg, depName string, subParents map[string]string) []string {
	if strings.Contains(depName, "/") {
		return nil
	}
	rootParent, hasParent := subParents[currentPkg]
	if !hasParent {
		return nil
	}
	var candidates []string
	current := currentPkg
	for {
		candidates = append(candidates, current+"/"+depName)
		if current == rootParent {
			break
		}
		idx := strings.LastIndex(current, "/")
		if idx == -1 {
			break
		}
		current = current[:idx]
	}
	if unique := uniqueSubPackageByLeaf(depName, rootParent, subParents); unique != "" {
		alreadyPresent := false
		for _, c := range candidates {
			if c == unique {
				alreadyPresent = true
				break
			}
		}
		if !alreadyPresent {
			candidates = append(candidates, unique)
		}
	}
	return candidates
}

func uniqueSubPackageByLeaf(depName, rootParent string, subParents map[string]string) string {
	suffix := "/" + depName
	match := ""
	for sub, owner := range subParents {
		if rootParent != "" && owner != rootParent {
			continue
		}
		if !strings.HasSuffix(sub, suffix) {
			continue
		}
		if match != "" && match != sub {
			return ""
		}
		match = sub
	}
	return match
}
