//go:build windows

package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// SymlinkHint describes what to do when symbolic links are unavailable.
const SymlinkHint = "enable Developer Mode (Settings > System > For developers) or run from an elevated shell"

func probeSymlinks() bool {
	dir, err := os.MkdirTemp("", "vmake-symlink-probe")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)

	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0755); err != nil {
		return false
	}
	return os.Symlink(target, filepath.Join(dir, "link")) == nil
}

func symlinkError(err error) error {
	return fmt.Errorf("%w (%s)", err, SymlinkHint)
}

func symlinkKindMatches(link, target os.FileInfo) bool {
	attrs := link.Sys().(*syscall.Win32FileAttributeData).FileAttributes
	return (attrs&syscall.FILE_ATTRIBUTE_DIRECTORY != 0) == target.IsDir()
}
