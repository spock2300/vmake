package api

import "strings"

func ResolveSubPackageName(currentPkg, depName string, subParents map[string]string, exists func(string) bool) string {
	if c := SubPackageCandidates(currentPkg, depName, subParents); c != nil {
		for _, candidate := range c {
			if exists(candidate) {
				return candidate
			}
		}
	}
	return depName
}

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
	return candidates
}
