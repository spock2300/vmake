package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

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

func newSubprocess(t *testing.T, child, workDir string, env []string, args ...string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, append([]string{"-test.run=^" + child + "$", "--"}, args...)...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), env...)
	return cmd
}

func runSubprocess(t *testing.T, workDir, vmakeDir string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := newSubprocess(t, "TestExtensionBootstrapChild", workDir, append([]string{"VMAKE_TEST_EXT_DIR=" + vmakeDir}, env...), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %v: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

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
