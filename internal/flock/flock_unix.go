//go:build !windows

package flock

import "syscall"

func lockFileExclusive(fd int) error {
	return syscall.Flock(fd, syscall.LOCK_EX)
}

func unlockFile(fd int) error {
	return syscall.Flock(fd, syscall.LOCK_UN)
}
