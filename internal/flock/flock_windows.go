//go:build windows

package flock

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

type lockState struct {
	overlapped windows.Overlapped
}

func lockFileMode(f *os.File, shared, nonblocking bool) (lockState, error) {
	st := lockState{}
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK)
	if shared {
		flags = 0
	}
	if nonblocking {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &st.overlapped)
	if err != nil {
		return lockState{}, fmt.Errorf("LockFileEx: %w", err)
	}
	return st, nil
}

func lockRetryable(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

func unlockFile(f *os.File, st lockState) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &st.overlapped)
}
