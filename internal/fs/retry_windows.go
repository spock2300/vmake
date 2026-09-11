//go:build windows

package fs

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

const (
	retryAttempts = 5
	retryBaseWait = 10 * time.Millisecond
)

// RemoveAllRetry removes path and everything below it, retrying while the
// filesystem reports a transient sharing or locking conflict.
func RemoveAllRetry(path string) error {
	return retryTransient(func() error { return os.RemoveAll(path) })
}

// RenameRetry renames oldpath to newpath, retrying while the filesystem
// reports a transient sharing or locking conflict. Windows refuses to rename
// a directory that still holds a file opened by another process (antivirus,
// indexer, a lingering git handle).
func RenameRetry(oldpath, newpath string) error {
	return retryTransient(func() error { return os.Rename(oldpath, newpath) })
}

func retryTransient(fn func() error) error {
	wait := retryBaseWait
	err := fn()
	for attempt := 1; attempt < retryAttempts && isTransient(err); attempt++ {
		time.Sleep(wait)
		wait *= 5
		err = fn()
	}
	return err
}

func isTransient(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_DIR_NOT_EMPTY)
}
