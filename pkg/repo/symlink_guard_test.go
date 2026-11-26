package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	internalfs "github.com/spock2300/vmake/internal/fs"
)

func TestEnsureSourceRejectsSymlinkedSrc(t *testing.T) {
	depDir := t.TempDir()
	target := t.TempDir()
	root := filepath.Join(depDir, "native", "pkg")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "src")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	mgr := NewSourceManager(depDir, t.TempDir())
	_, err := mgr.EnsureSource(SourceRequest{Key: "native/pkg", URLs: []string{"file:///nonexistent"}})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("EnsureSource error = %v, want symlink rejection", err)
	}
}

func TestValidateManagedPathRejectsSymlinkedTreeRoot(t *testing.T) {
	depDir := t.TempDir()
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(depDir, "native", "pkg")), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(depDir, "native", "pkg")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := internalfs.ValidateManagedPath(depDir, filepath.Join(depDir, "native", "pkg")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ValidateManagedPath error = %v, want symlink rejection", err)
	}
}
