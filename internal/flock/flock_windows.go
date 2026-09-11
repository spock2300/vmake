//go:build windows

package flock

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

type lockState struct {
	overlapped windows.Overlapped
}

func lockFileExclusive(f *os.File) (lockState, error) {
	st := lockState{}
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &st.overlapped)
	if err != nil {
		return lockState{}, fmt.Errorf("LockFileEx: %w", err)
	}
	return st, nil
}

func unlockFile(f *os.File, st lockState) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &st.overlapped)
}
