package resolver

import (
	"testing"

	"github.com/spock2300/vmake/pkg/lockfile"
)

func decisionLock(entries map[string]*lockfile.LockedPkg) *lockfile.Lock {
	l := lockfile.New()
	for name, entry := range entries {
		l.Set(name, entry)
	}
	return l
}

func TestLockQueryPinPrecedence(t *testing.T) {
	tests := []struct {
		name        string
		query       LockQuery
		wantValue   string
		wantTarget  bool
		wantOK      bool
		wantIgnored bool
	}{
		{
			name:        "target wins over config pin",
			query:       LockQuery{Policy: LockPolicy{Targets: map[string]string{"pkg": "1.2.3"}}, Pins: map[string]string{"pkg": "9.9.9"}},
			wantValue:   "1.2.3",
			wantTarget:  true,
			wantOK:      true,
			wantIgnored: true,
		},
		{
			name:        "float suppresses config pin and lock",
			query:       LockQuery{Policy: LockPolicy{Float: map[string]bool{"pkg": true}}, Pins: map[string]string{"pkg": "9.9.9"}},
			wantIgnored: true,
		},
		{
			name:      "config pin",
			query:     LockQuery{Pins: map[string]string{"pkg": "9.9.9"}},
			wantValue: "9.9.9",
			wantOK:    true,
		},
		{
			name:        "ignore all",
			query:       LockQuery{Policy: LockPolicy{IgnoreAll: true}},
			wantIgnored: true,
		},
		{
			name:  "no pin",
			query: LockQuery{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			value, fromTarget, ok := tc.query.PinFor("pkg")
			if value != tc.wantValue || fromTarget != tc.wantTarget || ok != tc.wantOK {
				t.Fatalf("PinFor = (%q, %v, %v), want (%q, %v, %v)", value, fromTarget, ok, tc.wantValue, tc.wantTarget, tc.wantOK)
			}
			if got := tc.query.IgnoredFor("pkg"); got != tc.wantIgnored {
				t.Fatalf("IgnoredFor = %v, want %v", got, tc.wantIgnored)
			}
		})
	}
}

func TestLockQueryLockEntry(t *testing.T) {
	lock := decisionLock(map[string]*lockfile.LockedPkg{
		"versioned":   {Version: "1.0.0", Commit: "c1"},
		"commit-only": {Commit: "c2"},
		"empty":       {},
	})
	tests := []struct {
		name  string
		query LockQuery
		pkg   string
		want  bool
	}{
		{name: "versioned entry", query: LockQuery{Lock: lock}, pkg: "versioned", want: true},
		{name: "commit-only entry", query: LockQuery{Lock: lock}, pkg: "commit-only", want: true},
		{name: "empty entry", query: LockQuery{Lock: lock}, pkg: "empty"},
		{name: "missing entry", query: LockQuery{Lock: lock}, pkg: "missing"},
		{name: "nil lock", query: LockQuery{}, pkg: "versioned"},
		{name: "ignore all", query: LockQuery{Policy: LockPolicy{IgnoreAll: true}, Lock: lock}, pkg: "versioned"},
		{name: "float", query: LockQuery{Policy: LockPolicy{Float: map[string]bool{"versioned": true}}, Lock: lock}, pkg: "versioned"},
		{name: "target", query: LockQuery{Policy: LockPolicy{Targets: map[string]string{"versioned": "2.0.0"}}, Lock: lock}, pkg: "versioned"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := tc.query.LockEntry(tc.pkg); ok != tc.want {
				t.Fatalf("LockEntry ok = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestDecidePrecedence(t *testing.T) {
	lock := decisionLock(map[string]*lockfile.LockedPkg{
		"pkg":        {Version: "1.0.0", Commit: "lock-commit"},
		"commitonly": {Commit: "commit-only"},
	})
	tests := []struct {
		name       string
		query      LockQuery
		pkg        string
		nativeVer  string
		nativeCom  string
		want       Provenance
		wantVer    string
		wantCommit string
		wantTarget bool
	}{
		{
			name:       "target commit",
			query:      LockQuery{Policy: LockPolicy{Targets: map[string]string{"pkg": pinnedCommitID}}, Lock: lock},
			pkg:        "pkg",
			want:       ProvenanceTargetCommit,
			wantVer:    pinnedCommitID,
			wantCommit: pinnedCommitID,
			wantTarget: true,
		},
		{
			name:       "target version ignores the lock entry",
			query:      LockQuery{Policy: LockPolicy{Targets: map[string]string{"pkg": "1.0.0"}}, Lock: lock},
			pkg:        "pkg",
			want:       ProvenanceTargetVersion,
			wantVer:    "1.0.0",
			wantCommit: "",
			wantTarget: true,
		},
		{
			name:       "config pin reuses matching lock commit",
			query:      LockQuery{Pins: map[string]string{"pkg": "1.0.0"}, Lock: lock},
			pkg:        "pkg",
			want:       ProvenanceConfigPin,
			wantVer:    "1.0.0",
			wantCommit: "lock-commit",
		},
		{
			name:       "config pin without matching lock",
			query:      LockQuery{Pins: map[string]string{"pkg": "2.0.0"}, Lock: lock},
			pkg:        "pkg",
			want:       ProvenanceConfigPin,
			wantVer:    "2.0.0",
			wantCommit: "",
		},
		{
			name:       "ignore all config pin does not reuse lock commit",
			query:      LockQuery{Pins: map[string]string{"pkg": "1.0.0"}, Policy: LockPolicy{IgnoreAll: true}, Lock: lock},
			pkg:        "pkg",
			want:       ProvenanceConfigPin,
			wantVer:    "1.0.0",
			wantCommit: "",
		},
		{
			name:       "lock version",
			query:      LockQuery{Lock: lock},
			pkg:        "pkg",
			want:       ProvenanceLock,
			wantVer:    "1.0.0",
			wantCommit: "lock-commit",
		},
		{
			name:       "lock commit only",
			query:      LockQuery{Lock: lock},
			pkg:        "commitonly",
			want:       ProvenanceLock,
			wantVer:    "",
			wantCommit: "commit-only",
		},
		{
			name:       "float falls through",
			query:      LockQuery{Policy: LockPolicy{Float: map[string]bool{"pkg": true}}, Pins: map[string]string{"pkg": "2.0.0"}, Lock: lock},
			pkg:        "pkg",
			nativeVer:  "3.0.0",
			nativeCom:  "native-commit",
			want:       ProvenanceNative,
			wantVer:    "3.0.0",
			wantCommit: "native-commit",
		},
		{
			name:       "native fallback",
			query:      LockQuery{},
			pkg:        "pkg",
			nativeVer:  "3.0.0",
			nativeCom:  "native-commit",
			want:       ProvenanceNative,
			wantVer:    "3.0.0",
			wantCommit: "native-commit",
		},
		{
			name:  "caller selects",
			query: LockQuery{},
			pkg:   "pkg",
			want:  ProvenanceCaller,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.query.Decide(tc.pkg, tc.nativeVer, tc.nativeCom)
			if got.Provenance != tc.want || got.Version != tc.wantVer || got.Commit != tc.wantCommit || got.FromTarget != tc.wantTarget {
				t.Fatalf("Decide = %+v, want provenance=%v version=%q commit=%q fromTarget=%v", got, tc.want, tc.wantVer, tc.wantCommit, tc.wantTarget)
			}
		})
	}
}

const pinnedCommitID = "0123456789abcdef0123456789abcdef01234567"
