package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiffAndDisplayVersion(t *testing.T) {
	oldLock := New()
	oldLock.SetContainer("hl/sdk", &LockedContainer{Version: "1.0.0", Commit: "1111111111111111", Source: "native"})
	oldLock.SetContainer("official", &LockedContainer{Commit: "2222222222222222", Source: "registry"})
	oldLock.SetContainer("same/mod", &LockedContainer{Version: "1.0.0", Commit: "aaaaaaaaaaaaaaaa", Source: "native"})
	oldLock.SetSource("official/tinyexpr", &LockedSource{Version: "1.0.0", Commit: "bbbbbbbbbbbbbbbb", URL: "https://example.com/tinyexpr.git"})

	newLock := New()
	newLock.SetContainer("hl/sdk", &LockedContainer{Version: "1.1.0", Commit: "3333333333333333", Source: "native"})
	newLock.SetContainer("new/mod", &LockedContainer{Commit: "4444444444444444", Source: "native"})
	newLock.SetContainer("same/mod", &LockedContainer{Version: "1.0.0", Commit: "aaaaaaaaaaaaaaaa", Source: "native"})
	newLock.SetSource("official/tinyexpr", &LockedSource{Version: "1.1.0", Commit: "cccccccccccccccc", URL: "https://example.com/tinyexpr.git"})
	newLock.SetSource("hl/sdk/lvgl", &LockedSource{Version: "9.6.0", Commit: "dddddddddddddddd"})

	changes := Diff(oldLock, newLock)
	if len(changes) != 5 {
		t.Fatalf("len(changes) = %d, want 5: %+v", len(changes), changes)
	}
	if changes[0].Section != "container" || changes[0].Name != "hl/sdk" || changes[0].Kind != "updated" || changes[0].OldVersion != "1.0.0" || changes[0].NewVersion != "1.1.0" {
		t.Fatalf("unexpected change[0]: %+v", changes[0])
	}
	if changes[1].Section != "container" || changes[1].Name != "new/mod" || changes[1].Kind != "added" {
		t.Fatalf("unexpected change[1]: %+v", changes[1])
	}
	if changes[2].Section != "container" || changes[2].Name != "official" || changes[2].Kind != "removed" {
		t.Fatalf("unexpected change[2]: %+v", changes[2])
	}
	if changes[3].Section != "source" || changes[3].Name != "hl/sdk/lvgl" || changes[3].Kind != "added" {
		t.Fatalf("unexpected change[3]: %+v", changes[3])
	}
	if changes[4].Section != "source" || changes[4].Name != "official/tinyexpr" || changes[4].Kind != "updated" {
		t.Fatalf("unexpected change[4]: %+v", changes[4])
	}

	if got := Diff(nil, nil); len(got) != 0 {
		t.Fatalf("Diff(nil, nil) = %+v, want empty", got)
	}
	if got := DisplayVersion("1.2.3", "abcdef0123456789"); got != "1.2.3" {
		t.Fatalf("DisplayVersion = %q, want 1.2.3", got)
	}
	if got := DisplayVersion("", "abcdef0123456789"); got != "abcdef012345" {
		t.Fatalf("DisplayVersion = %q, want abcdef012345", got)
	}
	if got := DisplayVersion("", ""); got != "" {
		t.Fatalf("DisplayVersion(\"\", \"\") = %q, want empty", got)
	}
}

func TestLoadRejectsV1Lockfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), LockfileName)
	if err := os.WriteFile(path, []byte(`{"lockfileVersion":1,"packages":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load should reject lockfileVersion 1")
	}
}

func TestSaveAndLoadV2Lockfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), LockfileName)
	l := New()
	l.SetContainer("hl/sdk", &LockedContainer{Version: "1.0.0", Commit: "abc", Source: "native"})
	l.SetSource("hl/sdk/lvgl", &LockedSource{Version: "9.6.0", Commit: "def", URL: "https://example.com/lvgl.git"})
	if err := l.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !l.Equal(loaded) {
		t.Fatalf("loaded lock mismatch:\ngot  %+v\nwant %+v", loaded, l)
	}
}
