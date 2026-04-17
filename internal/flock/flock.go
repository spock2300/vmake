package flock

import (
	"os"
	"syscall"

	"gitee.com/spock2300/vmake/internal/fs"
)

type FileLock struct {
	file *os.File
}

func Acquire(lockDir string) (*FileLock, error) {
	if err := fs.EnsureDir(lockDir); err != nil {
		return nil, err
	}
	lockFile := lockDir + "/.lock"
	f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return &FileLock{file: f}, nil
}

func (l *FileLock) Release() error {
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	return l.file.Close()
}
