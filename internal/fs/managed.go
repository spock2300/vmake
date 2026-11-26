package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateManagedPath refuses paths outside baseDir and any symlink component
// inside the managed tree.
func ValidateManagedPath(baseDir, path string) error {
	rel, err := filepath.Rel(baseDir, path)
	if err != nil {
		return fmt.Errorf("resolve managed path %s: %w", path, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("managed path %s is outside %s", path, baseDir)
	}
	current := baseDir
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("inspect managed path %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("managed path %s is a symlink; use 'vmake pkg link' for local sources", current)
		}
	}
	return nil
}
