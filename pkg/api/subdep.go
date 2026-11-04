package api

import "strings"

// ResolveSubPackageName resolves a short dependency name relative to
// currentPkg, walking up to its native owner. A caller that is not a
// sub-package resolves the unique sub-package with the same trailing name, and
// keeps an existing bare package of that name. subParents maps every
// discovered sub-package to that owner (not necessarily its direct directory
// parent). exists is supplied by the caller because the authoritative package
// set differs by phase (resolved sources, build graph, or prepared
// directories). Names containing "/" are absolute and pass through unchanged.
func ResolveSubPackageName(currentPkg, depName string, subParents map[string]string, exists func(string) bool) string {
	candidates := SubPackageCandidates(currentPkg, depName, subParents)
	if candidates == nil {
		return depName
	}
	if _, isSub := subParents[currentPkg]; !isSub && exists(depName) {
		return depName
	}
	for _, candidate := range candidates {
		if exists(candidate) {
			return candidate
		}
	}
	return depName
}

// SubPackageCandidates returns the nearest-first candidate names for a short
// dependency. A sub-package walks its native ancestors; a non-sub-package gets
// the unique sub-package with the same trailing name, if any. nil means
// depName is absolute or no candidate can be formed.
func SubPackageCandidates(currentPkg, depName string, subParents map[string]string) []string {
	if strings.Contains(depName, "/") {
		return nil
	}
	rootParent, hasParent := subParents[currentPkg]
	if !hasParent {
		if unique := uniqueSubPackageByLeaf(depName, "", subParents); unique != "" {
			return []string{unique}
		}
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
