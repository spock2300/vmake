package fs

import (
	"os"
)

func EnsureSymlink(linkPath, target string) error {
	if err := EnsureParentDir(linkPath); err != nil {
		return err
	}
	if existing, err := os.Readlink(linkPath); err == nil && existing == target {
		return nil
	}
	_ = os.RemoveAll(linkPath)
	return os.Symlink(target, linkPath)
}
