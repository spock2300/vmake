package resolver

import (
	"fmt"
	"strings"

	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/repo"
)

// LockSection identifies which section of vmake.lock v2 a LockQuery reads.
type LockSection string

const (
	LockSectionContainer LockSection = "container"
	LockSectionSource    LockSection = "source"
)

// LockedEntry is the normalized lock record returned by LockQuery for either a
// container or an independent SetGit source.
type LockedEntry struct {
	Version string
	Commit  string
	Source  string
	URL     string
}

// LockQuery is the pure input to version/commit/lock precedence decisions:
// the command's lock policy, the tracked config pins and the current lock.
type LockQuery struct {
	Policy  LockPolicy
	Pins    map[string]string
	Lock    *lockfile.Lock
	Section LockSection
}

// Containers returns a copy of q scoped to vmake.lock containers.
func (q LockQuery) Containers() LockQuery {
	q.Section = LockSectionContainer
	return q
}

// Sources returns a copy of q scoped to vmake.lock independent sources.
func (q LockQuery) Sources() LockQuery {
	q.Section = LockSectionSource
	return q
}

func (q LockQuery) effectiveSection() LockSection {
	if q.Section == LockSectionContainer {
		return LockSectionContainer
	}
	return LockSectionSource
}

// Provenance identifies which rule produced a Decision.
type Provenance int

const (
	ProvenanceCaller Provenance = iota
	ProvenanceTargetCommit
	ProvenanceTargetVersion
	ProvenanceConfigPin
	ProvenanceLock
	ProvenanceNative
)

// Decision is the single precedence result for a container or source: target
// pin > float (which suppresses config pins and lock) > config pin > lock >
// native-selected version. Version is the pinned/locked version or, for
// ProvenanceTargetCommit, the pinned commit (Commit repeats it).
type Decision struct {
	Provenance Provenance
	Version    string
	Commit     string
	FromTarget bool
}

// PinFor applies the target > float > config-pin precedence for one entry.
func (q LockQuery) PinFor(name string) (value string, fromTarget, ok bool) {
	if q.Policy.Targets != nil {
		if t := strings.TrimSpace(q.Policy.Targets[name]); t != "" {
			return t, true, true
		}
	}
	if q.Policy.Float != nil && q.Policy.Float[name] {
		return "", false, false
	}
	if q.Pins != nil {
		if p := strings.TrimSpace(q.Pins[name]); p != "" {
			return p, false, true
		}
	}
	return "", false, false
}

// IgnoredFor reports whether the lock entry for name must be ignored.
func (q LockQuery) IgnoredFor(name string) bool {
	if q.Policy.IgnoreAll {
		return true
	}
	if q.Policy.Float != nil && q.Policy.Float[name] {
		return true
	}
	if q.Policy.Targets != nil && strings.TrimSpace(q.Policy.Targets[name]) != "" {
		return true
	}
	return false
}

// LockEntry returns the usable lock entry for name in q's section.
func (q LockQuery) LockEntry(name string) (*LockedEntry, bool) {
	if q.Lock == nil || q.IgnoredFor(name) {
		return nil, false
	}
	return q.rawLockEntry(name)
}

// Decide applies the shared version/commit precedence. nativeSelected and
// nativeCommit are the resolver's already-selected values, if any.
func (q LockQuery) Decide(name, nativeSelected, nativeCommit string) Decision {
	if pin, fromTarget, ok := q.PinFor(name); ok {
		if repo.IsCommitID(pin) {
			return Decision{Provenance: ProvenanceTargetCommit, Version: pin, Commit: pin, FromTarget: fromTarget}
		}
		commit := ""
		if locked, lok := q.LockEntry(name); lok && locked.Version == pin {
			commit = locked.Commit
		}
		provenance := ProvenanceConfigPin
		if fromTarget {
			provenance = ProvenanceTargetVersion
		}
		return Decision{Provenance: provenance, Version: pin, Commit: commit, FromTarget: fromTarget}
	}
	if locked, ok := q.LockEntry(name); ok {
		return Decision{Provenance: ProvenanceLock, Version: locked.Version, Commit: locked.Commit}
	}
	if nativeSelected != "" || nativeCommit != "" {
		return Decision{Provenance: ProvenanceNative, Version: nativeSelected, Commit: nativeCommit}
	}
	return Decision{Provenance: ProvenanceCaller}
}

// LockProblem is a pin/lock disagreement or an unusable lock entry. The zero
// Pin means the problem is entry-only; a nil Locked means the entry is missing.
type LockProblem struct {
	Section    LockSection
	Pin        string
	FromTarget bool
	Locked     *LockedEntry
	Missing    bool
}

// PinLockMismatch reports whether the entry's effective pin disagrees with its
// lock entry. The enforcer compares target pins too: a target pin that differs
// from the lock means the build would not reproduce the lock, even though the
// pin takes precedence over it during resolution. Floats have no effective pin
// (PinFor answers false), so they are not a mismatch here.
func (q LockQuery) PinLockMismatch(name string) (LockProblem, bool) {
	pin, fromTarget, ok := q.PinFor(name)
	if !ok {
		return LockProblem{}, false
	}
	section := q.effectiveSection()
	locked, hasEntry := q.rawLockEntry(name)
	if !hasEntry {
		return LockProblem{Section: section, Pin: pin, FromTarget: fromTarget, Missing: true}, true
	}
	mismatch := (repo.IsCommitID(pin) && locked.Commit != pin) || (!repo.IsCommitID(pin) && locked.Version != pin)
	if !mismatch {
		return LockProblem{}, false
	}
	return LockProblem{Section: section, Pin: pin, FromTarget: fromTarget, Locked: locked}, true
}

// rawLockEntry returns the lock entry regardless of the policy's ignore rules.
// The mismatch check compares pins against the recorded lock, which is what
// --locked enforces; IgnoredFor governs resolution, not lock freshness.
func (q LockQuery) rawLockEntry(name string) (*LockedEntry, bool) {
	if q.Lock == nil {
		return nil, false
	}
	if q.effectiveSection() == LockSectionContainer {
		c, ok := q.Lock.GetContainer(name)
		if !ok || c == nil || (c.Version == "" && c.Commit == "") {
			return nil, false
		}
		return &LockedEntry{Version: c.Version, Commit: c.Commit, Source: c.Source}, true
	}
	s, ok := q.Lock.GetSource(name)
	if !ok || s == nil || (s.Version == "" && s.Commit == "") {
		return nil, false
	}
	return &LockedEntry{Version: s.Version, Commit: s.Commit, URL: s.URL}, true
}

// LockEntryProblem reports a missing or unusable lock entry. It reads the raw
// entry: --locked enforces lock freshness, so a float or target pin that makes
// the entry unusable for resolution must not hide it here. requireCommit is the
// resolver's stricter reading (an entry without a commit is not locked);
// callers that accept version-only entries pass false.
func (q LockQuery) LockEntryProblem(name string, requireCommit bool) (LockProblem, bool) {
	section := q.effectiveSection()
	locked, ok := q.rawLockEntry(name)
	if !ok {
		return LockProblem{Section: section, Missing: true}, true
	}
	if requireCommit && locked.Commit == "" {
		return LockProblem{Section: section, Locked: locked, Missing: true}, true
	}
	return LockProblem{}, false
}

// Error renders the one user-facing message for this problem. A missing lock
// file is a phase-level condition and is reported by the caller instead.
func (p LockProblem) Error(name string) error {
	kind := "source"
	if p.Section == LockSectionContainer {
		kind = "container"
	}
	if p.Missing {
		return fmt.Errorf("--locked: %s %s is not locked in vmake.lock; run 'vmake deps sync' or 'vmake deps update'", kind, name)
	}
	if p.Pin != "" && p.Locked != nil {
		return fmt.Errorf("--locked: %s %s is pinned to %s in .vmake/project.json, but vmake.lock has %s; run 'vmake deps sync'",
			kind, name, p.Pin, lockfile.DisplayVersion(p.Locked.Version, p.Locked.Commit))
	}
	return nil
}

// NeedsSync reports whether deps sync/update must touch name: the command has
// an explicit target pin, floats the entry, or carries a config pin that the
// lock does not reflect. Floats are touched because their resolved version is
// deliberately unreproducible and the lock has to follow it.
func (q LockQuery) NeedsSync(name string) bool {
	if _, ok := q.Policy.Targets[name]; ok {
		return true
	}
	if q.Policy.Float != nil && q.Policy.Float[name] {
		return true
	}
	pin := ""
	if q.Pins != nil {
		pin = strings.TrimSpace(q.Pins[name])
	}
	if pin == "" {
		return false
	}
	locked, ok := q.rawLockEntry(name)
	if !ok {
		return true
	}
	return (repo.IsCommitID(pin) && locked.Commit != pin) || (!repo.IsCommitID(pin) && locked.Version != pin)
}
