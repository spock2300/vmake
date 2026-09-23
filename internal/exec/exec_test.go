package exec

import (
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCommandHelper(t *testing.T) {
	mode := os.Getenv("VMAKE_EXEC_HELPER")
	if mode == "" {
		return
	}
	if mode == "wait" {
		time.Sleep(time.Hour)
	}
	program, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}
	dir, err := os.Getwd()
	if err != nil {
		os.Exit(3)
	}
	output := fmt.Sprintf("%s\n%s\n%s\n%s", program, dir, os.Args[0], os.Getenv("PATH"))
	if dest := os.Getenv("VMAKE_EXEC_OUTPUT"); dest != "" {
		if err := os.WriteFile(dest, []byte(output), 0644); err != nil {
			os.Exit(4)
		}
	} else {
		fmt.Print(output)
	}
	os.Exit(0)
}

func copyCommandHelper(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		name += ".exe"
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(program)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCommandEnvironmentPath(t *testing.T) {
	host, selected, work := t.TempDir(), t.TempDir(), t.TempDir()
	copyCommandHelper(t, host, "vmake-helper")
	want := copyCommandHelper(t, selected, "vmake-helper")
	t.Setenv("PATH", host)
	for _, method := range []string{"options", "captured", "streamed"} {
		t.Run(method, func(t *testing.T) {
			env := map[string]string{"PATH": selected, "VMAKE_EXEC_HELPER": "report"}
			args := []string{"-test.run=^TestCommandHelper$"}
			var output []byte
			var err error
			switch method {
			case "options":
				output, err = RunWithOptions("vmake-helper", args, RunOptions{Dir: work, Env: env, Quiet: true})
			case "captured":
				output, err = RunWithEnvCaptured(work, env, "vmake-helper", args...)
			case "streamed":
				env["VMAKE_EXEC_OUTPUT"] = filepath.Join(work, "output")
				err = RunWithEnv(work, env, "vmake-helper", args...)
				if err == nil {
					output, err = os.ReadFile(env["VMAKE_EXEC_OUTPUT"])
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(string(output), "\n")
			if len(lines) != 4 || !sameCommandPath(lines[0], want) || !sameCommandPath(lines[1], work) || lines[2] != "vmake-helper" || lines[3] != selected {
				t.Fatalf("command report = %q, want program %q, dir %q, original argv[0], PATH %q", output, want, work, selected)
			}
			if got := os.Getenv("PATH"); got != host {
				t.Fatalf("process PATH changed to %q", got)
			}
		})
	}
}

func sameCommandPath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func TestCommandPathOverrideDoesNotFallBack(t *testing.T) {
	host := t.TempDir()
	copyCommandHelper(t, host, "vmake-helper")
	t.Setenv("PATH", host)
	for _, path := range []string{"", t.TempDir()} {
		_, err := RunWithEnvCaptured("", map[string]string{"PATH": path, "VMAKE_EXEC_HELPER": "report"}, "vmake-helper", "-test.run=^TestCommandHelper$")
		var lookupErr *osexec.Error
		if !errors.Is(err, osexec.ErrNotFound) || !errors.As(err, &lookupErr) || lookupErr.Name != "vmake-helper" {
			t.Errorf("PATH %q: got %v, want named exec.ErrNotFound", path, err)
		}
	}
}

func TestCommandExplicitPathsAndDirectory(t *testing.T) {
	work := t.TempDir()
	absolute := copyCommandHelper(t, work, "vmake-helper")
	for _, name := range []string{absolute, "." + string(filepath.Separator) + filepath.Base(absolute)} {
		output, err := RunWithEnvCaptured(work, map[string]string{"PATH": "", "VMAKE_EXEC_HELPER": "report"}, name, "-test.run=^TestCommandHelper$")
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(output), "\n")
		if len(lines) != 4 || !sameCommandPath(lines[0], absolute) || !sameCommandPath(lines[1], work) || lines[2] != name {
			t.Fatalf("explicit %q report = %q", name, output)
		}
	}
}

func TestCommandRelativePathProtection(t *testing.T) {
	work := t.TempDir()
	copyCommandHelper(t, work, "vmake-helper")
	t.Chdir(work)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GODEBUG", "execerrdot=1")
	_, err := RunWithEnvCaptured("", map[string]string{"PATH": ".", "VMAKE_EXEC_HELPER": "report"}, "vmake-helper", "-test.run=^TestCommandHelper$")
	if !errors.Is(err, osexec.ErrDot) {
		t.Fatalf("relative PATH error = %v, want ErrDot", err)
	}
}

func TestCommandRejectsNonExecutablePathEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not use executable permission bits")
	}
	bin := t.TempDir()
	path := copyCommandHelper(t, bin, "vmake-helper")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := RunWithEnvCaptured("", map[string]string{"PATH": bin, "VMAKE_EXEC_HELPER": "report"}, "vmake-helper", "-test.run=^TestCommandHelper$")
	if !errors.Is(err, osexec.ErrNotFound) {
		t.Fatalf("non-executable command error = %v", err)
	}
}

func TestCommandEnvironmentContext(t *testing.T) {
	bin := t.TempDir()
	copyCommandHelper(t, bin, "vmake-helper")
	t.Setenv("PATH", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RunWithOptions("vmake-helper", []string{"-test.run=^TestCommandHelper$"}, RunOptions{
		Env: map[string]string{"PATH": bin, "VMAKE_EXEC_HELPER": "wait"}, Context: ctx, Quiet: true,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled command error = %v", err)
	}
	start := time.Now()
	_, err = RunWithOptions("vmake-helper", []string{"-test.run=^TestCommandHelper$"}, RunOptions{
		Env: map[string]string{"PATH": bin, "VMAKE_EXEC_HELPER": "wait"}, Timeout: 100 * time.Millisecond, Quiet: true,
	})
	var exitErr *osexec.ExitError
	if !errors.As(err, &exitErr) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed out command error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout took %v", elapsed)
	}
}

func TestCommandContextAndTimeoutBothApply(t *testing.T) {
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, parentFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(parentFirst), func(t *testing.T) {
			parentTimeout, commandTimeout := 2*time.Second, 100*time.Millisecond
			if parentFirst {
				parentTimeout, commandTimeout = commandTimeout, parentTimeout
			}
			ctx, cancel := context.WithTimeout(context.Background(), parentTimeout)
			defer cancel()
			start := time.Now()
			_, err := RunWithOptions(program, []string{"-test.run=^TestCommandHelper$"}, RunOptions{
				Env: map[string]string{"VMAKE_EXEC_HELPER": "wait"}, Context: ctx, Timeout: commandTimeout, Quiet: true,
			})
			if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
				t.Fatalf("earlier timeout not honored: %v (%s)", err, time.Since(start))
			}
		})
	}
}
