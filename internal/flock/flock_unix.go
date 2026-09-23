//go:build !windows

package flock

import (
	"errors"
	"os"
	"syscall"
)

type lockState struct{}

func lockFileMode(f *os.File, shared, nonblocking bool) (lockState, error) {
	mode := syscall.LOCK_EX
	if shared {
		mode = syscall.LOCK_SH
	}
	if nonblocking {
		mode |= syscall.LOCK_NB
	}
	return lockState{}, syscall.Flock(int(f.Fd()), mode)
}

func lockRetryable(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EINTR)
}

func unlockFile(f *os.File, _ lockState) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
