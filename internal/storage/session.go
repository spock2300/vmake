package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/spock2300/vmake/internal/flock"
)

type Session struct {
	cacheDir  string
	exclusive bool
	locks     []*flock.FileLock
	owners    map[string]bool
	sealed    bool
	closed    bool
}

func CacheDir(root string) string {
	return filepath.Join(root, "v2")
}

func OwnerKey(name string) string {
	h := sha256.Sum256([]byte(name))
	return hex.EncodeToString(h[:])
}

func Acquire(projectDir, cacheDir string, exclusive bool) (*Session, error) {
	return AcquireContext(context.Background(), projectDir, cacheDir, exclusive)
}

func AcquireContext(ctx context.Context, projectDir, cacheDir string, exclusive bool) (*Session, error) {
	cacheDir, err := filepath.Abs(cacheDir)
	if err != nil {
		return nil, err
	}
	s := &Session{cacheDir: cacheDir, exclusive: exclusive, owners: make(map[string]bool)}
	if projectDir != "" {
		lock, err := flock.AcquireContext(ctx, filepath.Join(projectDir, ".vmake", "_locks", "project.lock"))
		if err != nil {
			return nil, err
		}
		s.locks = append(s.locks, lock)
	}
	acquire := flock.AcquireSharedContext
	if exclusive {
		acquire = flock.AcquireContext
	}
	lock, err := acquire(ctx, filepath.Join(cacheDir, "_locks", "lifecycle.lock"))
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	s.locks = append(s.locks, lock)
	return s, nil
}

func (s *Session) AcquireOwners(owners []string) error {
	return s.AcquireOwnersContext(context.Background(), owners)
}

func (s *Session) AcquireOwnersContext(ctx context.Context, owners []string) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return fmt.Errorf("storage session is closed")
	}
	names := slices.Clone(owners)
	slices.Sort(names)
	names = slices.Compact(names)
	if s.sealed {
		for _, name := range names {
			if !s.owners[name] {
				return fmt.Errorf("storage owner %s was not acquired before execution", name)
			}
		}
		return nil
	}
	start := len(s.locks)
	defer func() {
		if err == nil {
			return
		}
		for i := len(s.locks) - 1; i >= start; i-- {
			err = errors.Join(err, s.locks[i].Release())
		}
		s.locks = s.locks[:start]
		for _, name := range names {
			delete(s.owners, name)
		}
	}()
	for _, name := range names {
		lock, err := flock.AcquireContext(ctx, filepath.Join(s.cacheDir, "_locks", "owner_"+OwnerKey(name)+".lock"))
		if err != nil {
			return fmt.Errorf("acquire storage owner %s: %w", name, err)
		}
		s.locks = append(s.locks, lock)
		s.owners[name] = true
	}
	s.sealed = true
	return nil
}

func (s *Session) Exclusive() bool {
	return s != nil && !s.closed && s.exclusive
}

func (s *Session) CheckAccess(cacheDir string, exclusive bool) error {
	if s.closed {
		return fmt.Errorf("storage session is closed")
	}
	cacheDir, err := filepath.Abs(cacheDir)
	if err != nil {
		return err
	}
	if cacheDir != s.cacheDir {
		return fmt.Errorf("storage session guards %s, not %s", s.cacheDir, cacheDir)
	}
	if exclusive && !s.exclusive {
		return fmt.Errorf("cache deletion or update requires an exclusive storage session")
	}
	return nil
}

func (s *Session) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	var errs []error
	for i := len(s.locks) - 1; i >= 0; i-- {
		errs = append(errs, s.locks[i].Release())
	}
	s.locks = nil
	return errors.Join(errs...)
}
