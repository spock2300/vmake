package exec

import (
	"errors"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandWindowsPathExtensions(t *testing.T) {
	bin := t.TempDir()
	copyCommandHelper(t, bin, "vmake-helper.exe")
	want := copyCommandHelper(t, bin, "vmake-helper.tool")
	t.Setenv("PATH", "")
	t.Setenv("PATHEXT", ".EXE")
	output, err := RunWithEnvCaptured("", map[string]string{
		"Path": bin, "PathExt": ".TOOL;.EXE", "VMAKE_EXEC_HELPER": "report",
	}, "vmake-helper", "-test.run=^TestCommandHelper$")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(string(output), "\n")[0]; !sameCommandPath(got, want) {
		t.Fatalf("program = %q, want %q", got, want)
	}
	for _, ext := range []string{"", ".EXE"} {
		_, err := RunWithEnvCaptured("", map[string]string{
			"path": bin, "pathext": ext, "VMAKE_EXEC_HELPER": "report",
		}, "vmake-helper", "-test.run=^TestCommandHelper$")
		if err != nil {
			t.Fatalf("PATHEXT %q: %v", ext, err)
		}
	}
	copyCommandHelper(t, bin, "missing.tool.exe")
	_, err = RunWithEnvCaptured("", map[string]string{"PATH": bin, "PATHEXT": ".TOOL"}, "missing")
	if !errors.Is(err, osexec.ErrNotFound) {
		t.Fatalf("unexpected inherited PATHEXT fallback: %v", err)
	}
}

func TestCommandWindowsCurrentDirectory(t *testing.T) {
	work := t.TempDir()
	copyCommandHelper(t, work, "vmake-helper")
	t.Chdir(work)
	t.Setenv("PATH", "")
	t.Setenv("GODEBUG", "execerrdot=1")
	t.Setenv("NoDefaultCurrentDirectoryInExePath", "")
	if err := os.Unsetenv("NoDefaultCurrentDirectoryInExePath"); err != nil {
		t.Fatal(err)
	}
	_, err := RunWithEnvCaptured("", map[string]string{"PATH": t.TempDir()}, "vmake-helper")
	if !errors.Is(err, osexec.ErrDot) {
		t.Fatalf("implicit current directory error = %v", err)
	}
	_, err = RunWithEnvCaptured("", map[string]string{"PATH": t.TempDir(), "NoDefaultCurrentDirectoryInExePath": ""}, "vmake-helper")
	if !errors.Is(err, osexec.ErrNotFound) {
		t.Fatalf("disabled current directory error = %v", err)
	}
	_, err = RunWithEnvCaptured("", map[string]string{"PATH": work, "VMAKE_EXEC_HELPER": "report"}, "vmake-helper", "-test.run=^TestCommandHelper$")
	if err != nil {
		t.Fatalf("explicit current directory PATH %q: %v", filepath.Clean(work), err)
	}
}

func TestCommandWindowsRejectsExtensionlessSelection(t *testing.T) {
	bin := t.TempDir()
	sibling := copyCommandHelper(t, bin, "vmake-extensionless.exe")
	data, err := os.ReadFile(sibling)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "vmake-extensionless"), data, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	t.Setenv("PATHEXT", ".EXE")
	dest := filepath.Join(bin, "output")
	_, err = RunWithEnvCaptured("", map[string]string{
		"PATH": bin, "PATHEXT": ";", "VMAKE_EXEC_HELPER": "report", "VMAKE_EXEC_OUTPUT": dest,
	}, "vmake-extensionless", "-test.run=^TestCommandHelper$")
	var lookupErr *osexec.Error
	if !errors.As(err, &lookupErr) || !strings.Contains(err.Error(), "extensionless") {
		t.Fatalf("extensionless lookup error = %v", err)
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sibling executable ran: %v", err)
	}
}
