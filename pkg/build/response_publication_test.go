package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spock2300/vmake/internal/flock"
)

func TestResponseFileAcrossProcesses(t *testing.T) {
	const dirEnv = "VMAKE_TEST_SHARED_RESPONSE_DIRECTORY"
	if dir := os.Getenv(dirEnv); dir != "" {
		runResponsePublisher(t, dir, os.Getenv("VMAKE_TEST_RESPONSE_WORKER"))
		return
	}

	segment := strings.Repeat("long-path-", 8)
	dir := filepath.Join(t.TempDir(), segment, segment, segment, segment, "vmake-rsp")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	file := responseFileFor(dir, []string{"-Iinclude", "-DROUND=0"})
	if err := os.WriteFile(file.path, []byte("short"), 0644); err != nil {
		t.Fatal(err)
	}
	lock, err := flock.AcquireContext(t.Context(), dir+".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if lock != nil {
			lock.Release()
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const workers = 3
	completed := make(chan error, workers)
	for i := range workers {
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestResponseFileAcrossProcesses$")
		cmd.Env = append(os.Environ(), dirEnv+"="+dir, fmt.Sprintf("VMAKE_TEST_RESPONSE_WORKER=%d", i))
		go func() {
			output, err := cmd.CombinedOutput()
			if err != nil {
				err = fmt.Errorf("response publisher: %w: %s", err, output)
			}
			completed <- err
		}()
	}
	// Each child must observe cancellation while a different process owns the
	// lock. Only then let all children repair and publish to the shared cache.
	for {
		ready, err := filepath.Glob(filepath.Join(dir, "ready-*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(ready) == workers {
			break
		}
		select {
		case err := <-completed:
			t.Fatalf("publisher exited before release: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	lock = nil
	for range workers {
		if err := <-completed; err != nil {
			t.Fatal(err)
		}
	}
}

func runResponsePublisher(t *testing.T, dir, worker string) {
	t.Helper()
	file := responseFileFor(dir, []string{"-Iinclude", "-DROUND=0"})
	wait, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	err := ensureResponseFile(wait, file)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting for the shared cache returned %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ready-"+worker), nil, 0644); err != nil {
		t.Fatal(err)
	}
	for i := range 24 {
		file := responseFileFor(dir, []string{"-Iinclude", fmt.Sprintf("-DROUND=%d", i)})
		if err := ensureResponseFile(t.Context(), file); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(file.path)
		if err != nil || string(data) != file.content {
			t.Fatalf("newly published response file is unreadable or incorrect: %q: %v", data, err)
		}
	}
}

func TestResponseFileRejectsNonRegularCacheEntry(t *testing.T) {
	file := responseFileFor(t.TempDir(), []string{"-Iinclude"})
	if err := os.Mkdir(file.path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ensureResponseFile(t.Context(), file); err == nil {
		t.Fatal("accepted a directory as a compiler response file")
	}
	if info, err := os.Stat(file.path); err != nil || !info.IsDir() {
		t.Fatalf("invalid cache entry was overwritten: %v", err)
	}
}

func TestCompilerAcceptsNilContextOnResponseCacheMiss(t *testing.T) {
	dir := t.TempDir()
	sentinel := errors.New("compiler was invoked")
	c := &Compiler{ccPath: "cc", run: func(string, string, ...string) ([]byte, error) { return nil, sentinel }}
	opts := &CompileOptions{Language: "c", Includes: []string{"include"}, rspDir: filepath.Join(dir, "rsp")}
	_, err := c.CompileContext(nil, "probe.c", "probe.o", opts, dir)
	if !errors.Is(err, sentinel) {
		t.Fatalf("CompileContext with nil context returned %v", err)
	}
	if runtime.GOOS == "windows" {
		file := responseFileFor(opts.rspDir, []string{"-Iinclude"})
		if data, err := os.ReadFile(file.path); err != nil || string(data) != file.content {
			t.Fatalf("response cache miss with nil context: %q: %v", data, err)
		}
	}
}

func waitForResponseGateRefs(t *testing.T, dir string, want int) {
	t.Helper()
	key := responseDirGateKey(dir)
	deadline := time.Now().Add(5 * time.Second)
	for {
		responseDirMu.Lock()
		refs := 0
		if gate := responseDirGates[key]; gate != nil {
			refs = gate.refs
		}
		responseDirMu.Unlock()
		if refs == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("response dir gate refs for %s = %d, want %d", key, refs, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestResponseFileInProcessGateSkipsFlockAfterPublish(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rsp")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	readyFile := responseFileFor(dir, []string{"-Iinclude", "-DREADY=1"})
	if err := os.WriteFile(readyFile.path, []byte(readyFile.content), 0644); err != nil {
		t.Fatal(err)
	}
	file := responseFileFor(dir, []string{"-Iinclude", "-DGATE=0"})

	release, err := acquireResponseDirGate(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			release()
		}
	}()

	hitCtx, hitCancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	err = ensureResponseFile(hitCtx, readyFile)
	hitCancel()
	if err != nil {
		t.Fatalf("cache hit blocked on in-process gate: %v", err)
	}

	cancelCtx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	err = ensureResponseFile(cancelCtx, file)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter blocked on in-process gate returned %v, want context.DeadlineExceeded", err)
	}

	waiterDone := make(chan error, 1)
	go func() {
		waiterDone <- ensureResponseFile(t.Context(), file)
	}()
	waitForResponseGateRefs(t, dir, 2)

	if err := os.WriteFile(file.path, []byte(file.content), 0644); err != nil {
		t.Fatal(err)
	}
	release()
	released = true

	select {
	case err := <-waiterDone:
		if err != nil {
			t.Fatalf("queued waiter after publish: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued waiter did not finish after gate release")
	}
	if _, err := os.Stat(dir + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("queued same-process waiter touched flock after peer published: %v", err)
	}
	waitForResponseGateRefs(t, dir, 0)
}

func TestResponseFileInProcessWaitRespectsCancellation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rsp")
	file := responseFileFor(dir, []string{"-Iinclude", "-DGATE=1"})
	held, err := flock.AcquireContext(t.Context(), dir+".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if held != nil {
			_ = held.Release()
		}
	}()

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- ensureResponseFile(t.Context(), file)
	}()
	waitForResponseGateRefs(t, dir, 1)

	waitCtx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	err = ensureResponseFile(waitCtx, file)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second in-process waiter returned %v, want context.DeadlineExceeded", err)
	}

	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	held = nil

	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first waiter after release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first waiter did not finish after lock release")
	}
	data, err := os.ReadFile(file.path)
	if err != nil || string(data) != file.content {
		t.Fatalf("published response file = %q: %v", data, err)
	}
	waitForResponseGateRefs(t, dir, 0)
}

func TestResponseFileConcurrentRepairWhileReading(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rsp")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for round := range 8 {
		file := responseFileFor(dir, []string{"-Iinclude", fmt.Sprintf("-DREPAIR=%d", round)})
		if err := os.WriteFile(file.path, []byte("short"), 0644); err != nil {
			t.Fatal(err)
		}
		const workers = 16
		start := make(chan struct{})
		errs := make(chan error, workers)
		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if err := ensureResponseFile(t.Context(), file); err != nil {
					errs <- err
					return
				}
				data, err := os.ReadFile(file.path)
				if err != nil || string(data) != file.content {
					errs <- fmt.Errorf("read after repair = %q: %v", data, err)
				}
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
	}
}
