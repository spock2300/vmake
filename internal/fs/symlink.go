package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	symlinkSupportOnce sync.Once
	symlinkSupportOK   bool
)

// SymlinksSupported reports whether this process may create symbolic links.
// The probe runs once per process; on Unix it is always true.
func SymlinksSupported() bool {
	symlinkSupportOnce.Do(func() {
		symlinkSupportOK = probeSymlinks()
	})
	return symlinkSupportOK
}

func EnsureSymlink(linkPath, target string) error {
	target = filepath.FromSlash(target)
	targetPath := target
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(filepath.Dir(linkPath), targetPath)
	}
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		return fmt.Errorf("stat symlink target %s: %w", targetPath, err)
	}
	if err := EnsureParentDir(linkPath); err != nil {
		return err
	}
	info, err := os.Lstat(linkPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("cannot replace %s with a symlink: existing path is not a symbolic link", linkPath)
		}
		existing, err := os.Readlink(linkPath)
		if err != nil {
			return fmt.Errorf("read symlink %s: %w", linkPath, err)
		}
		if filepath.Clean(existing) == filepath.Clean(target) && symlinkKindMatches(info, targetInfo) {
			return nil
		}
		if err := os.Remove(linkPath); err != nil {
			return fmt.Errorf("remove symlink %s: %w", linkPath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect symlink %s: %w", linkPath, err)
	}
	if err := os.Symlink(target, linkPath); err != nil {
		return symlinkError(err)
	}
	info, err = os.Lstat(linkPath)
	if err != nil {
		return fmt.Errorf("inspect new symlink %s: %w", linkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 || !symlinkKindMatches(info, targetInfo) {
		return fmt.Errorf("symlink %s has the wrong link kind for %s", linkPath, targetPath)
	}
	return nil
}
