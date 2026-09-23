package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spock2300/vmake/internal/flock"
)

func TestCanceledOwnerAcquisitionReleasesPartialLocks(t *testing.T) {
	cache := t.TempDir()
	held, err := flock.Acquire(filepath.Join(cache, "_locks", "owner_"+OwnerKey("repo/b")+".lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	s, err := Acquire("", cache, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err = s.AcquireOwnersContext(ctx, []string{"repo/b", "repo/a"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("owner cancellation = %v", err)
	}
	if len(s.owners) != 0 || s.sealed {
		t.Fatalf("canceled owner set retained: %v", s.owners)
	}
	retry, err := Acquire("", cache, false)
	if err != nil {
		t.Fatal(err)
	}
	defer retry.Close()
	retryCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := retry.AcquireOwnersContext(retryCtx, []string{"repo/a"}); err != nil {
		t.Fatalf("partial owner lock remained held: %v", err)
	}
}

func TestCanceledLifecycleAcquisitionReleasesProjectLock(t *testing.T) {
	cache, project := t.TempDir(), t.TempDir()
	held, err := Acquire("", cache, true)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	s, err := AcquireContext(ctx, project, cache, false)
	if s != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lifecycle cancellation = %v, %v", s, err)
	}
	retryCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	lock, err := flock.AcquireContext(retryCtx, filepath.Join(project, ".vmake", "_locks", "project.lock"))
	if err != nil {
		t.Fatalf("project lock remained held: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}

type childRequest struct {
	Project   string
	Cache     string
	Exclusive bool
	Owners    []string
	Started   string
}

func TestStorageChild(t *testing.T) {
	data := os.Getenv("VMAKE_STORAGE_CHILD")
	if data == "" {
		t.Skip("subprocess")
	}
	var req childRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(req.Started, nil, 0644); err != nil {
		t.Fatal(err)
	}
	s, err := Acquire(req.Project, req.Cache, req.Exclusive)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AcquireOwners(req.Owners); err != nil {
		t.Fatal(err)
	}
}

func startStorageChild(t *testing.T, req childRequest) <-chan error {
	t.Helper()
	req.Started = filepath.Join(t.TempDir(), "started")
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStorageChild$")
	cmd.Env = append(os.Environ(), "VMAKE_STORAGE_CHILD="+string(data))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(req.Started); err == nil {
			return done
		}
		select {
		case err := <-done:
			if _, startedErr := os.Stat(req.Started); startedErr == nil {
				done <- err
				return done
			}
			t.Fatalf("child exited before attempting lock: %v", err)
		case <-deadline:
			t.Fatal("child did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func assertBlockedThenRelease(t *testing.T, done <-chan error, s *Session) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("child acquired a conflicting lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child failed after release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child stayed blocked after release")
	}
}

func TestLifecycleProtectsWholeSession(t *testing.T) {
	cache := t.TempDir()
	s, err := Acquire("", cache, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	reader := startStorageChild(t, childRequest{Cache: cache})
	select {
	case err := <-reader:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("independent readers blocked each other")
	}
	cleaner := startStorageChild(t, childRequest{Cache: cache, Exclusive: true})
	assertBlockedThenRelease(t, cleaner, s)
}

func TestOwnersAreOrderedAndReentrant(t *testing.T) {
	cache := t.TempDir()
	s, err := Acquire("", cache, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.AcquireOwners([]string{"repo/b", "repo/a", "repo/b"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AcquireOwners([]string{"repo/a"}); err != nil {
		t.Fatalf("synchronous child could not reuse owners: %v", err)
	}
	if err := s.AcquireOwners([]string{"repo/c"}); err == nil {
		t.Fatal("late owner acquisition was accepted")
	}
	child := startStorageChild(t, childRequest{Cache: cache, Owners: []string{"repo/a", "repo/b"}})
	assertBlockedThenRelease(t, child, s)
}

func TestProjectLockProtectsSourceLinks(t *testing.T) {
	project, cache := t.TempDir(), t.TempDir()
	s, err := Acquire(project, cache, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	child := startStorageChild(t, childRequest{Project: project, Cache: cache})
	assertBlockedThenRelease(t, child, s)
}
