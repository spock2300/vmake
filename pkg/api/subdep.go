package api

import "strings"

func ResolveSubPackageName(currentPkg, depName string, subParents map[string]string, exists func(string) bool) string {
	if strings.Contains(depName, "/") {
		return depName
	}
	rootParent, hasParent := subParents[currentPkg]
	if !hasParent {
		return depName
	}
	current := currentPkg
	for {
		candidate := current + "/" + depName
		if exists(candidate) {
			return candidate
		}
		if current == rootParent {
			break
		}
		idx := strings.LastIndex(current, "/")
		if idx == -1 {
			break
		}
		current = current[:idx]
	}
	return depName
}
