//go:build !windows

package fs

import "os"

// RemoveAllRetry removes path and everything below it. Outside Windows no
// retry is needed: open files do not block deletion.
func RemoveAllRetry(path string) error { return os.RemoveAll(path) }

// RenameRetry renames oldpath to newpath. Outside Windows no retry is needed.
func RenameRetry(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }
