package lockfile

import "testing"

func TestDiffAndDisplayVersion(t *testing.T) {
	oldLock := New()
	oldLock.Set("a/pkg", &LockedPkg{Version: "1.0.0", Commit: "1111111111111111", Source: "native"})
	oldLock.Set("b/pkg", &LockedPkg{Version: "2.0.0", Commit: "2222222222222222", Source: "registry"})
	oldLock.Set("same/pkg", &LockedPkg{Version: "1.0.0", Commit: "aaaaaaaaaaaaaaaa", Source: "native"})
	oldLock.Set("wrap/pkg", &LockedPkg{Version: "1.0.0", Commit: "bbbbbbbbbbbbbbbb", Source: "registry", WrapperCommit: "w1"})

	newLock := New()
	newLock.Set("a/pkg", &LockedPkg{Version: "1.1.0", Commit: "3333333333333333", Source: "native"})
	newLock.Set("c/pkg", &LockedPkg{Version: "", Commit: "4444444444444444", Source: "native"})
	newLock.Set("same/pkg", &LockedPkg{Version: "1.0.0", Commit: "aaaaaaaaaaaaaaaa", Source: "native"})
	newLock.Set("wrap/pkg", &LockedPkg{Version: "1.0.0", Commit: "bbbbbbbbbbbbbbbb", Source: "registry", WrapperCommit: "w2"})

	changes := Diff(oldLock, newLock)
	if len(changes) != 4 {
		t.Fatalf("len(changes) = %d, want 4: %+v", len(changes), changes)
	}
	if changes[0].Name != "a/pkg" || changes[0].Kind != "updated" || changes[0].OldVersion != "1.0.0" || changes[0].NewVersion != "1.1.0" {
		t.Fatalf("unexpected change[0]: %+v", changes[0])
	}
	if changes[1].Name != "b/pkg" || changes[1].Kind != "removed" {
		t.Fatalf("unexpected change[1]: %+v", changes[1])
	}
	if changes[2].Name != "c/pkg" || changes[2].Kind != "added" || changes[2].NewCommit != "4444444444444444" {
		t.Fatalf("unexpected change[2]: %+v", changes[2])
	}
	if changes[3].Name != "wrap/pkg" || changes[3].Kind != "updated" || changes[3].Source != "registry" {
		t.Fatalf("unexpected change[3]: %+v", changes[3])
	}

	if got := Diff(nil, nil); len(got) != 0 {
		t.Fatalf("Diff(nil, nil) = %+v, want empty", got)
	}
	if got := Diff(nil, newLock); len(got) != 4 {
		t.Fatalf("Diff(nil, newLock) len = %d, want 4", len(got))
	}
	if got := Diff(oldLock, nil); len(got) != 4 {
		t.Fatalf("Diff(oldLock, nil) len = %d, want 4", len(got))
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
