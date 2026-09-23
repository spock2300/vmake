package flock

import (
	"context"
	"os"
	"path/filepath"
	"time"

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
	return AcquireContext(context.Background(), lockFile)
}

func AcquireShared(lockFile string) (*FileLock, error) {
	return AcquireSharedContext(context.Background(), lockFile)
}

func AcquireContext(ctx context.Context, lockFile string) (*FileLock, error) {
	return acquire(ctx, lockFile, false)
}

func AcquireSharedContext(ctx context.Context, lockFile string) (*FileLock, error) {
	return acquire(ctx, lockFile, true)
}

func acquire(ctx context.Context, lockFile string, shared bool) (*FileLock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := fs.EnsureDir(filepath.Dir(lockFile)); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	for {
		state, err := lockFileMode(f, shared, ctx.Done() != nil)
		if err == nil {
			if err := ctx.Err(); err != nil {
				unlockFile(f, state)
				f.Close()
				return nil, err
			}
			return &FileLock{file: f, state: state}, nil
		}
		if !lockRetryable(err) {
			f.Close()
			return nil, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		if ctx.Done() == nil {
			<-timer.C
			continue
		}
		select {
		case <-ctx.Done():
			timer.Stop()
			f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *FileLock) Release() error {
	unlockFile(l.file, l.state)
	return l.file.Close()
}
