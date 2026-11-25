package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// runCLI executes one command in-process and returns its streams and exit code.
// It restores the working directory before returning, matching the behaviour of
// the subprocess helpers it replaces.
func runCLI(t *testing.T, workDir, vmakeDir string, env []string, args ...string) (string, string, int) {
	t.Helper()
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			t.Fatalf("malformed env entry %q", kv)
		}
		t.Setenv(name, value)
	}
	if workDir != "" {
		previous, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(workDir); err != nil {
			t.Fatal(err)
		}
		defer os.Chdir(previous)
	}
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr, runEnv{VmakeDir: vmakeDir})
	return stdout.String(), stderr.String(), code
}

// runSubprocess is the only place a test may spawn a real vmake process. The
// plugin, trust and interrupt tests need it because they mutate process-wide
// toolchain state that DD-40 leaves process-scoped.
func runSubprocess(t *testing.T, workDir, vmakeDir string, env []string, args ...string) (string, string, int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commandArgs := append([]string{"-test.run=^TestExtensionBootstrapChild$", "--"}, args...)
	cmd := exec.Command(exe, commandArgs...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "VMAKE_TEST_EXT_DIR="+vmakeDir)
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %v: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

// TestCommandProcessBoundary pins the one thing an in-process call cannot
// cover: a real exit status through os.Exit, and real file descriptors. The
// snapshot suite covers the shipped binary itself.
func TestCommandProcessBoundary(t *testing.T) {
	workDir := t.TempDir()
	stdout, stderr, code := runSubprocess(t, workDir, canonicalTempDir(t), nil, "version")
	if code != 0 {
		t.Fatalf("version exit = %d, stderr = %q", code, stderr)
	}
	if stdout == "" {
		t.Fatal("version printed nothing to the real stdout")
	}
	_, stderr, code = runSubprocess(t, workDir, canonicalTempDir(t), nil, "build", "--no-such-flag")
	if code != 2 {
		t.Fatalf("usage error exit = %d, want 2 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stderr, "unknown flag") {
		t.Fatalf("usage error did not reach the real stderr: %q", stderr)
	}
}
