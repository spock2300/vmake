package scriptfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestScriptSymlinkPrivilegeHint(t *testing.T) {
	base := t.TempDir()
	create := New(base).Exports()["os/os"]["Symlink"].Interface().(func(string, string) error)
	link := filepath.Join(base, "link")
	err := create("missing-target", "link")
	if err != nil {
		var detail *os.LinkError
		if !errors.Is(err, syscall.ERROR_PRIVILEGE_NOT_HELD) || !errors.As(err, &detail) ||
			detail.Old != "missing-target" || detail.New != link || !strings.Contains(err.Error(), "Developer Mode") {
			t.Fatalf("creating a script link: %v", err)
		}
	} else {
		if target, err := os.Readlink(link); err != nil || target != "missing-target" {
			t.Fatalf("dangling relative target = %q, %v", target, err)
		}
		if err := create("different", "link"); !errors.Is(err, os.ErrExist) || strings.Contains(err.Error(), "Developer Mode") {
			t.Fatalf("creating an occupied script link: %v", err)
		}
	}
	if err := create("bad\x00target", "invalid"); err == nil || strings.Contains(err.Error(), "Developer Mode") {
		t.Fatalf("invalid script link target: %v", err)
	}
}
