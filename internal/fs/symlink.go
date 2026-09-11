package fs

import (
	"os"
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
	if err := EnsureParentDir(linkPath); err != nil {
		return err
	}
	if existing, err := os.Readlink(linkPath); err == nil && existing == target {
		return nil
	}
	_ = RemoveAllRetry(linkPath)
	if err := os.Symlink(target, linkPath); err != nil {
		return symlinkError(err)
	}
	return nil
}
