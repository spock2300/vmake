package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestEnsureSymlinkRepairsWrongWindowsKind(t *testing.T) {
	if !SymlinksSupported() {
		t.Skip(SymlinkHint)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSymlink(link, target); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(link); err != nil || !info.IsDir() {
		t.Fatalf("file link was not repaired to directory link: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSymlink(link, target); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(link); err != nil || string(data) != "file" {
		t.Fatalf("directory link was not repaired to file link: %q, %v", data, err)
	}
}

func TestWindowsSymlinkPrivilegeHint(t *testing.T) {
	for _, tc := range []struct {
		name  string
		errno syscall.Errno
		hint  bool
	}{
		{"missing privilege", syscall.ERROR_PRIVILEGE_NOT_HELD, true},
		{"access denied", syscall.ERROR_ACCESS_DENIED, false},
		{"already exists", syscall.ERROR_ALREADY_EXISTS, false},
		{"path missing", syscall.ERROR_PATH_NOT_FOUND, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			linkErr := &os.LinkError{Op: "symlink", Old: "target", New: "link", Err: tc.errno}
			operation := fmt.Errorf("create link: %w", linkErr)
			err := symlinkError(operation)
			var original *os.LinkError
			if !errors.Is(err, tc.errno) || !errors.As(err, &original) || original != linkErr {
				t.Fatalf("lost original Windows link error: %v", err)
			}
			if got := strings.Contains(err.Error(), "Developer Mode"); got != tc.hint {
				t.Fatalf("Developer Mode hint = %v, want %v: %v", got, tc.hint, err)
			}
			if !tc.hint && err != operation {
				t.Fatalf("unrelated error changed: %v", err)
			}
		})
	}
}
