package resolver

import (
	"strings"
	"testing"
	"time"

	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/repo"
)

func TestCloseReleasesRepositoryLocks(t *testing.T) {
	mgr := repo.NewRepoManager(t.TempDir())
	r := NewResolver(mgr, t.TempDir())

	shared, err := mgr.Lock("demo", false)
	if err != nil {
		t.Fatal(err)
	}
	r.repoLocks["demo"] = shared

	r.Close()
	r.Close()

	acquired := make(chan error, 1)
	go func() {
		exclusive, err := mgr.Lock("demo", true)
		if err == nil {
			err = exclusive.Release()
		}
		acquired <- err
	}()
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exclusive lock still blocked after Resolver.Close")
	}
}

func TestCheckLockedEntryUsesLockQueryPredicates(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	lockedLock := func() *lockfile.Lock {
		l := lockfile.New()
		l.Set("pkg", &lockfile.LockedPkg{Version: "1.0.0", Commit: commit, Source: "native"})
		l.Set("other", &lockfile.LockedPkg{Version: "9.0.0", Commit: commit, Source: "native"})
		return l
	}
	versionOnly := lockfile.New()
	versionOnly.Set("pkg", &lockfile.LockedPkg{Version: "1.0.0", Source: "native"})

	tests := []struct {
		name    string
		policy  LockPolicy
		pins    map[string]string
		lock    *lockfile.Lock
		wantErr string
	}{
		{name: "not locked policy skips the check", lock: lockedLock()},
		{
			name:    "empty lock is a phase-level failure",
			policy:  LockPolicy{Locked: true},
			lock:    lockfile.New(),
			wantErr: "vmake.lock is out of date or missing",
		},
		{
			name:    "entry without a commit is not locked",
			policy:  LockPolicy{Locked: true},
			lock:    versionOnly,
			wantErr: "is not locked in vmake.lock",
		},
		{
			name:    "missing entry",
			policy:  LockPolicy{Locked: true},
			lock:    lockedLock(),
			pins:    map[string]string{"absent": "1.0.0"},
			wantErr: "absent is not locked in vmake.lock",
		},
		{
			name:   "matching config pin",
			policy: LockPolicy{Locked: true},
			lock:   lockedLock(),
			pins:   map[string]string{"pkg": "1.0.0"},
		},
		{
			name:    "mismatching config pin",
			policy:  LockPolicy{Locked: true},
			lock:    lockedLock(),
			pins:    map[string]string{"pkg": "2.0.0"},
			wantErr: "pinned to 2.0.0 in .vmake/project.json, but vmake.lock has 1.0.0",
		},
		{
			name:    "mismatching target pin is now compared",
			policy:  LockPolicy{Locked: true, Targets: map[string]string{"pkg": "3.0.0"}},
			lock:    lockedLock(),
			wantErr: "pinned to 3.0.0 in .vmake/project.json, but vmake.lock has 1.0.0",
		},
		{
			name:   "matching target pin",
			policy: LockPolicy{Locked: true, Targets: map[string]string{"pkg": "1.0.0"}},
			lock:   lockedLock(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewResolver(nil, t.TempDir())
			r.SetLockfile(tc.lock, false)
			r.SetLockPolicy(tc.policy)
			r.SetConfigPins(tc.pins)
			// missing-entry case targets a package absent from the lock
			id := "pkg"
			if tc.wantErr != "" && strings.Contains(tc.wantErr, "absent") {
				id = "absent"
			}
			err := r.checkLockedEntry(id)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("checkLockedEntry: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
