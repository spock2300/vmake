//go:build !windows

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/storage"
)

func waitSourceInterruptReady(t *testing.T, ready string, done <-chan error, output *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("child exited before ready: %v\n%s", err, output.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("source preparation did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBuildSourceInterruptReapsGitAndRetries(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip(err)
	}
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip(err)
	}
	ar, err := exec.LookPath("ar")
	if err != nil {
		t.Skip(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			dir := t.TempDir()
			project, upstream := filepath.Join(dir, "project"), filepath.Join(dir, "upstream")
			writeExtensionFile(t, filepath.Join(upstream, "source.txt"), "seed")
			for _, args := range [][]string{{"init", "-q"}, {"add", "source.txt"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "seed"}} {
				cmd := exec.Command(realGit, args...)
				cmd.Dir = upstream
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git fixture: %v\n%s", err, out)
				}
			}
			writeExtensionFile(t, filepath.Join(project, "build.go"), fmt.Sprintf(`package main
import ("os"; "github.com/spock2300/vmake/pkg/api")
func Main(p *api.Package) {
 p.SetRoot(true)
 p.OnPackage(func(p *api.Package) { p.SetGit(%q) })
 p.OnBuild(func(ctx *api.BuildContext) { ctx.Target("ready").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error { return os.WriteFile("later", []byte("ran"), 0644) }) })
}`, upstream))
			writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), `{"version":"1","global":{"toolchain":"gcc"},"entries":{}}`)
			writeExtensionFile(t, filepath.Join(dir, "home", "extensions", "fixture", "native", "toolchain.json"), fmt.Sprintf(`{"name":"gcc","tools":{"cc":%q,"cxx":%q,"ar":%q,"ld":%q}}`, cc, cc, ar, cc))
			writeExtensionFile(t, filepath.Join(dir, "block"), "block")
			wrapper := `#!/bin/sh
for arg in "$@"; do
 if [ "$arg" = clone ] && [ -f "$VMAKE_SOURCE_INTERRUPT_DIR/block" ]; then
  echo ready > "$VMAKE_SOURCE_INTERRUPT_DIR/ready"
  /bin/sh -c 'sleep 2; echo late > "$VMAKE_SOURCE_INTERRUPT_DIR/late"' &
  wait
 fi
done
exec REAL "$@"
`
			wrapper = strings.ReplaceAll(wrapper, "REAL", "'"+strings.ReplaceAll(realGit, "'", "'\\''")+"'")
			if err := os.WriteFile(filepath.Join(dir, "git"), []byte(wrapper), 0755); err != nil {
				t.Fatal(err)
			}
			args := []string{"-test.run=^TestExtensionBootstrapChild$", "--", "build", "-k"}
			env := append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "VMAKE_TEST_EXT_DIR="+filepath.Join(dir, "home"), "VMAKE_CACHE="+filepath.Join(dir, "cache"), "VMAKE_TRUST_ALL=1", "VMAKE_SOURCE_INTERRUPT_DIR="+dir)
			cmd := exec.Command(exe, args...)
			cmd.Dir, cmd.Env = project, env
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			waitSourceInterruptReady(t, filepath.Join(dir, "ready"), done, &output)
			if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				var exited *exec.ExitError
				if !errors.As(err, &exited) || exited.ExitCode() != 128+int(sig) {
					t.Fatalf("interrupt exit: %v\n%s", err, output.String())
				}
			case <-time.After(time.Second):
				t.Fatal("CLI did not cancel source preparation promptly")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			locks, err := storage.AcquireContext(ctx, project, filepath.Join(dir, "cache"), true)
			if err != nil {
				t.Fatalf("storage locks remained held: %v", err)
			}
			if err := locks.Close(); err != nil {
				t.Fatal(err)
			}
			time.Sleep(2200 * time.Millisecond)
			for _, path := range []string{filepath.Join(dir, "late"), filepath.Join(project, "later"), filepath.Join(project, "src")} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("canceled source preparation continued: %s, %v", path, err)
				}
			}
			if err := os.Remove(filepath.Join(dir, "block")); err != nil {
				t.Fatal(err)
			}
			retry := exec.Command(exe, args...)
			retry.Dir, retry.Env = project, env
			if out, err := retry.CombinedOutput(); err != nil {
				t.Fatalf("retry: %v\n%s", err, out)
			}
			if _, err := os.Stat(filepath.Join(project, "later")); err != nil {
				t.Fatalf("retry did not build target: %v", err)
			}
		})
	}
}

func TestOwnerInterruptChild(t *testing.T) {
	dir := os.Getenv("VMAKE_OWNER_INTERRUPT_DIR")
	if dir == "" {
		return
	}
	locks, err := storage.Acquire(filepath.Join(dir, "project"), filepath.Join(dir, "cache"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer locks.Close()
	ctx := &RuntimeContext{Locks: locks}
	err = withBuildContext(ctx, func() error {
		if err := os.WriteFile(filepath.Join(dir, "ready"), nil, 0644); err != nil {
			return err
		}
		return locks.AcquireOwnersContext(ctx.Context, []string{"official/shared"})
	})
	var interrupted *buildInterrupted
	if !errors.As(err, &interrupted) || (interrupted.code != 130 && interrupted.code != 143) {
		t.Fatalf("owner wait interruption: %v", err)
	}
}

func TestOwnerLockWaitInterruptsBeforeOwnerReleases(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			dir := t.TempDir()
			held, err := flock.Acquire(filepath.Join(dir, "cache", "_locks", "owner_"+storage.OwnerKey("official/shared")+".lock"))
			if err != nil {
				t.Fatal(err)
			}
			defer held.Release()
			cmd := exec.Command(os.Args[0], "-test.run=^TestOwnerInterruptChild$")
			cmd.Env = append(os.Environ(), "VMAKE_OWNER_INTERRUPT_DIR="+dir)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			waitSourceInterruptReady(t, filepath.Join(dir, "ready"), done, &output)
			if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("owner wait interruption: %v\n%s", err, output.String())
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation waited for unrelated owner lock release")
			}
		})
	}
}
