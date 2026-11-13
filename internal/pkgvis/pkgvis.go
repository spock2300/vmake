package pkgvis

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const localRootPrefix = "@local"

func evalNearestAncestor(path string) string {
	cur := filepath.Clean(path)
	var tail []string
	for cur != "" {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return filepath.Clean(resolved)
		}
		if _, err := os.Lstat(cur); err != nil {
			parent := filepath.Dir(cur)
			if parent == cur {
				break
			}
			tail = append(tail, filepath.Base(cur))
			cur = parent
			continue
		}
		break
	}
	return filepath.Clean(path)
}

func VisibilityPath(isLocal bool, pkgID, projectRoot, pkgDir string) string {
	trimmedID := strings.Trim(filepath.ToSlash(pkgID), "/")
	if !isLocal {
		return trimmedID
	}
	fallback := localRootPrefix
	if trimmedID != "" {
		fallback = localRootPrefix + "/" + trimmedID
	}
	if projectRoot == "" || pkgDir == "" {
		return fallback
	}
	cleanRoot := filepath.Clean(projectRoot)
	cleanPkg := filepath.Clean(pkgDir)
	rel, err := filepath.Rel(cleanRoot, cleanPkg)
	if err != nil || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
		realRoot := evalNearestAncestor(cleanRoot)
		realPkg := evalNearestAncestor(cleanPkg)
		rel, err = filepath.Rel(realRoot, realPkg)
	}
	if err != nil {
		return fallback
	}
	slash := filepath.ToSlash(rel)
	if slash == "." || slash == "" {
		return localRootPrefix
	}
	if slash == ".." || strings.HasPrefix(slash, "../") {
		return fallback
	}
	return localRootPrefix + "/" + slash
}

func CheckInternal(fromID, fromVisPath, toID, toVisPath string) error {
	if fromID == toID || toVisPath == "" || fromVisPath == "" {
		return nil
	}
	normTo := strings.Trim(toVisPath, "/")
	parts := strings.Split(normTo, "/")
	minInternalIdx := 2
	if parts[0] == localRootPrefix {
		minInternalIdx = 1
	}
	lastInternal := -1
	for i := minInternalIdx; i < len(parts); i++ {
		if parts[i] == "internal" {
			lastInternal = i
		}
	}
	if lastInternal < 0 {
		return nil
	}
	parentPrefix := strings.Join(parts[:lastInternal], "/")
	normFrom := strings.Trim(fromVisPath, "/")
	if parentPrefix == "" {
		return nil
	}
	if normFrom == parentPrefix || strings.HasPrefix(normFrom, parentPrefix+"/") {
		return nil
	}
	return fmt.Errorf("use of internal package %q not allowed from package %q", toID, fromID)
}
