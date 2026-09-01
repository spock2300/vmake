package lockfile

import (
	"fmt"
	"os"
	"sort"

	"github.com/spock2300/vmake/internal/jsonio"
)

const (
	LockVersion  = 1
	LockfileName = "vmake.lock" // lives at <projectRoot>/.vmake/vmake.lock
)

type LockedPkg struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Source  string `json:"source"`
	// WrapperCommit pins the registry repo HEAD that provided the wrapper
	// build.go for registry packages. Native packages are pinned by Commit
	// alone (the wrapper IS the package repo).
	WrapperCommit string `json:"wrapperCommit,omitempty"`
}

type Lock struct {
	LockVersion int                   `json:"lockfileVersion"`
	Packages    map[string]*LockedPkg `json:"packages"`
}

func New() *Lock {
	return &Lock{
		LockVersion: LockVersion,
		Packages:    make(map[string]*LockedPkg),
	}
}

func Load(path string) (*Lock, error) {
	l := New()
	if err := jsonio.Load(path, l); err != nil {
		return nil, err
	}
	if l.Packages == nil {
		l.Packages = make(map[string]*LockedPkg)
	}
	return l, nil
}

// LoadOrCreate loads the lock at path, returning a fresh lock only when the
// file does not exist yet. A corrupt or unreadable lock is an error — falling
// back to an empty lock would silently re-resolve and overwrite the pins.
func LoadOrCreate(path string) (*Lock, error) {
	l, err := Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}
		return nil, fmt.Errorf("lockfile %s is corrupt: %w", path, err)
	}
	return l, nil
}

func (l *Lock) Save(path string) error {
	return jsonio.Save(path, l)
}

func (l *Lock) Get(name string) (*LockedPkg, bool) {
	p, ok := l.Packages[name]
	return p, ok
}

func (l *Lock) Set(name string, p *LockedPkg) {
	l.Packages[name] = p
}

func (l *Lock) Remove(name string) {
	delete(l.Packages, name)
}

func (l *Lock) SortedNames() []string {
	names := make([]string, 0, len(l.Packages))
	for name := range l.Packages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (l *Lock) Equal(other *Lock) bool {
	if len(l.Packages) != len(other.Packages) {
		return false
	}
	for name, p := range l.Packages {
		q, ok := other.Packages[name]
		if !ok || *p != *q {
			return false
		}
	}
	return true
}
