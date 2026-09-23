package flock

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireContextCancelsWithoutReplacingHeldFile(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "exclusive", true: "shared"}[shared], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "held.lock")
			held, err := Acquire(path)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			acquire := AcquireContext
			if shared {
				acquire = AcquireSharedContext
			}
			lock, err := acquire(ctx, path)
			if lock != nil || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("waiting lock = %v, %v", lock, err)
			}
			after, err := os.Stat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("held lock file replaced: %v", err)
			}
			if err := held.Release(); err != nil {
				t.Fatal(err)
			}
			retryCtx, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			lock, err = acquire(retryCtx, path)
			if err != nil {
				t.Fatal(err)
			}
			if err := lock.Release(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

const childEnvKey = "VMAKE_FLOCK_CHILD"

func TestAcquireRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.lock")

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	again, err := Acquire(path)
	if err != nil {
		t.Fatalf("re-Acquire after Release: %v", err)
	}
	if err := again.Release(); err != nil {
		t.Fatalf("re-Release: %v", err)
	}
}

// TestLockChild is the subprocess half of TestLockExcludesOtherProcess. It is
// not a test on its own: the parent runs this binary with childEnvKey set.
func TestLockChild(t *testing.T) {
	path := os.Getenv(childEnvKey)
	if path == "" {
		t.Skip("subprocess helper; driven by TestLockExcludesOtherProcess")
	}

	done := make(chan error, 1)
	go func() {
		l, err := Acquire(path)
		if err == nil {
			err = l.Release()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child Acquire: %v", err)
		}
		// Acquired while the parent claimed to hold the lock.
		os.Exit(10)
	case <-time.After(1500 * time.Millisecond):
		// Blocked, as an exclusive lock requires.
		os.Exit(0)
	}
}

func runChild(t *testing.T, path string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestLockChild")
	cmd.Env = append(os.Environ(), childEnvKey+"="+path)
	err := cmd.Run()
	if err == nil {
		return 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	t.Fatalf("run child: %v", err)
	return -1
}

func TestLockExcludesOtherProcess(t *testing.T) {
	if os.Getenv(childEnvKey) != "" {
		t.Skip("running as the child helper")
	}

	path := filepath.Join(t.TempDir(), "contended.lock")

	held, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if code := runChild(t, path); code != 0 {
		held.Release()
		t.Fatalf("child exit = %d while the lock was held, want 0 (blocked); 10 means it acquired the lock concurrently", code)
	}

	if err := held.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if code := runChild(t, path); code != 10 {
		t.Fatalf("child exit = %d after Release, want 10 (acquired)", code)
	}
}
