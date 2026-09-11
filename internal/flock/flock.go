package flock

import (
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/internal/fs"
)

type FileLock struct {
	file  *os.File
	state lockState
}

// Acquire takes an exclusive flock on the given lock FILE path. The file's
// parent directory is created if needed. Lock files must live outside the
// directories they guard so they are never removed while held.
func Acquire(lockFile string) (*FileLock, error) {
	if err := fs.EnsureDir(filepath.Dir(lockFile)); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	state, err := lockFileExclusive(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &FileLock{file: f, state: state}, nil
}

func (l *FileLock) Release() error {
	unlockFile(l.file, l.state)
	return l.file.Close()
}
