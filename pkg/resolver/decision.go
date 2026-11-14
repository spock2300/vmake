package resolver

import (
	"strings"

	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/repo"
)

// LockQuery is the pure input to version/commit/lock precedence decisions:
// the command's lock policy, the tracked config pins and the current lock.
type LockQuery struct {
	Policy LockPolicy
	Pins   map[string]string
	Lock   *lockfile.Lock
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

// Decision is the single precedence result for a package: target pin >
// float (which suppresses config pins and lock) > config pin > lock >
// native-selected version. Version is the pinned/locked version or, for
// ProvenanceTargetCommit, the pinned commit (Commit repeats it).
type Decision struct {
	Provenance Provenance
	Version    string
	Commit     string
	FromTarget bool
}

// PinFor applies the target > float > config-pin precedence for one package.
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

// LockEntry returns the usable lock entry for name.
func (q LockQuery) LockEntry(name string) (*lockfile.LockedPkg, bool) {
	if q.Lock == nil || q.IgnoredFor(name) {
		return nil, false
	}
	locked, ok := q.Lock.Get(name)
	if !ok || (locked.Version == "" && locked.Commit == "") {
		return nil, false
	}
	return locked, true
}

// Decide applies the shared version/commit precedence. nativeSelected and
// nativeCommit are the resolver's already-selected native values, if any.
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
