//go:build !windows

package flock

import (
	"os"
	"syscall"
)

type lockState struct{}

func lockFileExclusive(f *os.File) (lockState, error) {
	return lockState{}, syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func unlockFile(f *os.File, _ lockState) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
