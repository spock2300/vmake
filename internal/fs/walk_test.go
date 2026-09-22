package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWalkBrokenSymlinkInfo(t *testing.T) {
	if !SymlinksSupported() {
		t.Skip(SymlinkHint)
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(dir, "missing"), link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	calls := 0
	err := Walk(dir, func(path string, info os.FileInfo, err error) error {
		if path != link {
			return err
		}
		calls++
		if info == nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("broken symlink info = %v", info)
		}
		if !os.IsNotExist(err) {
			t.Errorf("broken symlink error = %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("broken symlink callbacks = %d, want 1", calls)
	}
}
