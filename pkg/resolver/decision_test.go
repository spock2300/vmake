package resolver

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/lockfile"
)

func decisionLock(entries map[string]*lockfile.LockedSource) *lockfile.Lock {
	l := lockfile.New()
	for name, entry := range entries {
		l.SetSource(name, entry)
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
	lock := decisionLock(map[string]*lockfile.LockedSource{
		"versioned":   {Version: "1.0.0", Commit: "c1"},
		"commit-only": {Commit: "c2"},
		"empty":       {},
	})
	lock.SetContainer("hl/sdk", &lockfile.LockedContainer{Version: "0.3.0", Commit: "sdk1", Source: "native"})
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
	if entry, ok := (LockQuery{Lock: lock}).Containers().LockEntry("hl/sdk"); !ok || entry.Version != "0.3.0" || entry.Commit != "sdk1" {
		t.Fatalf("Containers().LockEntry = (%+v, %v), want 0.3.0/sdk1", entry, ok)
	}
	if _, ok := (LockQuery{Lock: lock}).Sources().LockEntry("hl/sdk"); ok {
		t.Fatal("Sources() must not read container section")
	}
}

func TestDecidePrecedence(t *testing.T) {
	lock := decisionLock(map[string]*lockfile.LockedSource{
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

func TestLockQueryPinLockMismatch(t *testing.T) {
	const shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	lock := decisionLock(map[string]*lockfile.LockedSource{
		"pinned":   {Version: "1.0.0", Commit: shaA},
		"commit":   {Commit: shaA},
		"unpinned": {Version: "2.0.0", Commit: shaB},
		"empty":    {},
	})
	tests := []struct {
		name     string
		query    LockQuery
		pkg      string
		wantOK   bool
		wantPin  string
		wantMiss bool
	}{
		{"pin matches version", LockQuery{Lock: lock, Pins: map[string]string{"pinned": "1.0.0"}}, "pinned", false, "", false},
		{"pin mismatches version", LockQuery{Lock: lock, Pins: map[string]string{"pinned": "1.1.0"}}, "pinned", true, "1.1.0", false},
		{"pin matches commit", LockQuery{Lock: lock, Pins: map[string]string{"commit": shaA}}, "commit", false, "", false},
		{"pin mismatches commit", LockQuery{Lock: lock, Pins: map[string]string{"commit": shaB}}, "commit", true, shaB, false},
		{"pinned but entry missing", LockQuery{Lock: lock, Pins: map[string]string{"missing": "1.0.0"}}, "missing", true, "1.0.0", true},
		{"no pin", LockQuery{Lock: lock}, "unpinned", false, "", false},
		{"float suppresses the check", LockQuery{Lock: lock, Policy: LockPolicy{Float: map[string]bool{"pinned": true}}, Pins: map[string]string{"pinned": "1.1.0"}}, "pinned", false, "", false},
		{"target pin overrides the config pin and is compared", LockQuery{Lock: lock, Pins: map[string]string{"pinned": "1.1.0"}, Policy: LockPolicy{Targets: map[string]string{"pinned": "9.9.9"}}}, "pinned", true, "9.9.9", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			problem, ok := tc.query.PinLockMismatch(tc.pkg)
			if ok != tc.wantOK {
				t.Fatalf("PinLockMismatch ok = %v, want %v (problem %+v)", ok, tc.wantOK, problem)
			}
			if problem.Pin != tc.wantPin || problem.Missing != tc.wantMiss {
				t.Fatalf("problem = %+v, want pin %q missing %v", problem, tc.wantPin, tc.wantMiss)
			}
		})
	}
}

func TestLockQueryLockEntryProblem(t *testing.T) {
	lock := decisionLock(map[string]*lockfile.LockedSource{
		"versioned":    {Version: "1.0.0", Commit: "c1"},
		"version-only": {Version: "1.0.0"},
	})
	for _, tc := range []struct {
		name          string
		query         LockQuery
		pkg           string
		requireCommit bool
		want          bool
	}{
		{"versioned accepts version-only reading", LockQuery{Lock: lock}, "versioned", false, false},
		{"version-only entry is a problem for the resolver", LockQuery{Lock: lock}, "version-only", true, true},
		{"version-only entry is fine for callers accepting it", LockQuery{Lock: lock}, "version-only", false, false},
		{"missing entry", LockQuery{Lock: lock}, "missing", false, true},
		{"ignore-all does not hide the entry from enforcement", LockQuery{Lock: lock, Policy: LockPolicy{IgnoreAll: true}}, "versioned", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, got := tc.query.LockEntryProblem(tc.pkg, tc.requireCommit); got != tc.want {
				t.Fatalf("LockEntryProblem = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLockProblemError(t *testing.T) {
	missing := LockProblem{Section: LockSectionContainer, Missing: true}
	if got := missing.Error("hl/sdk"); got == nil || !strings.Contains(got.Error(), "container hl/sdk is not locked in vmake.lock") {
		t.Fatalf("missing error = %v", got)
	}
	mismatch := LockProblem{Section: LockSectionSource, Pin: "2.0.0", Locked: &LockedEntry{Version: "1.0.0", Commit: "c1"}}
	got := mismatch.Error("official/pkg")
	if got == nil || !strings.Contains(got.Error(), "pinned to 2.0.0") || !strings.Contains(got.Error(), "vmake deps sync") {
		t.Fatalf("mismatch error = %v", got)
	}
	if err := (LockProblem{}).Error("pkg"); err != nil {
		t.Fatalf("zero problem error = %v, want nil", err)
	}
}

func TestLockQueryNeedsSync(t *testing.T) {
	const shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	lock := decisionLock(map[string]*lockfile.LockedSource{
		"pinned":   {Version: "1.0.0", Commit: shaA},
		"commit":   {Commit: shaA},
		"unlocked": {Version: "9.0.0", Commit: shaA},
	})
	tests := []struct {
		name  string
		query LockQuery
		pkg   string
		want  bool
	}{
		{"no pin and no policy", LockQuery{Lock: lock}, "pinned", false},
		{"target pin is always touched", LockQuery{Lock: lock, Policy: LockPolicy{Targets: map[string]string{"pinned": "1.0.0"}}}, "pinned", true},
		{"float is always touched", LockQuery{Lock: lock, Policy: LockPolicy{Float: map[string]bool{"pinned": true}}}, "pinned", true},
		{"matching config pin", LockQuery{Lock: lock, Pins: map[string]string{"pinned": "1.0.0"}}, "pinned", false},
		{"mismatching config pin", LockQuery{Lock: lock, Pins: map[string]string{"pinned": "2.0.0"}}, "pinned", true},
		{"matching commit pin", LockQuery{Lock: lock, Pins: map[string]string{"commit": shaA}}, "commit", false},
		{"mismatching commit pin", LockQuery{Lock: lock, Pins: map[string]string{"commit": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}, "commit", true},
		{"pin without a lock entry", LockQuery{Lock: lock, Pins: map[string]string{"missing": "1.0.0"}}, "missing", true},
		{"pin without a lock file", LockQuery{Pins: map[string]string{"pinned": "1.0.0"}}, "pinned", true},
		{"pin without a lock file and no pin", LockQuery{}, "pinned", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.query.NeedsSync(tc.pkg); got != tc.want {
				t.Fatalf("NeedsSync = %v, want %v", got, tc.want)
			}
		})
	}
}
