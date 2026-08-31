//go:build windows

package flock

import "errors"

func lockFileExclusive(fd int) error {
	return errors.New("flock: unsupported on windows (vmake is Unix-only)")
}

func unlockFile(fd int) error {
	return nil
}
