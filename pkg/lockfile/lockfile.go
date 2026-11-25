package lockfile

import (
	"fmt"
	"os"
	"sort"

	"github.com/spock2300/vmake/internal/jsonio"
)

const (
	LockVersion  = 2
	LockfileName = "vmake.lock"
)

type LockedContainer struct {
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Source  string `json:"source"`
}

type LockedSource struct {
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
	URL     string `json:"url,omitempty"`
}

type Lock struct {
	LockVersion int                         `json:"lockfileVersion"`
	Containers  map[string]*LockedContainer `json:"containers,omitempty"`
	Sources     map[string]*LockedSource    `json:"sources,omitempty"`
}

func New() *Lock {
	return &Lock{
		LockVersion: LockVersion,
		Containers:  make(map[string]*LockedContainer),
		Sources:     make(map[string]*LockedSource),
	}
}

func Load(path string) (*Lock, error) {
	l := &Lock{}
	if err := jsonio.Load(path, l); err != nil {
		return nil, err
	}
	if l.LockVersion != LockVersion {
		return nil, fmt.Errorf("unsupported lockfileVersion %d (expected %d); regenerate with 'vmake deps sync' or remove %s", l.LockVersion, LockVersion, path)
	}
	if l.Containers == nil {
		l.Containers = make(map[string]*LockedContainer)
	}
	if l.Sources == nil {
		l.Sources = make(map[string]*LockedSource)
	}
	return l, nil
}

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
	if l.LockVersion == 0 {
		l.LockVersion = LockVersion
	}
	return jsonio.Save(path, l)
}

func (l *Lock) IsEmpty() bool {
	return l == nil || (len(l.Containers) == 0 && len(l.Sources) == 0)
}

func (l *Lock) GetContainer(name string) (*LockedContainer, bool) {
	if l == nil || l.Containers == nil {
		return nil, false
	}
	c, ok := l.Containers[name]
	return c, ok
}

func (l *Lock) SetContainer(name string, c *LockedContainer) {
	if l.Containers == nil {
		l.Containers = make(map[string]*LockedContainer)
	}
	l.Containers[name] = c
}

func (l *Lock) RemoveContainer(name string) {
	if l != nil && l.Containers != nil {
		delete(l.Containers, name)
	}
}

func (l *Lock) SortedContainerNames() []string {
	if l == nil {
		return nil
	}
	names := make([]string, 0, len(l.Containers))
	for name := range l.Containers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (l *Lock) GetSource(name string) (*LockedSource, bool) {
	if l == nil || l.Sources == nil {
		return nil, false
	}
	s, ok := l.Sources[name]
	return s, ok
}

func (l *Lock) SetSource(name string, s *LockedSource) {
	if l.Sources == nil {
		l.Sources = make(map[string]*LockedSource)
	}
	l.Sources[name] = s
}

func (l *Lock) RemoveSource(name string) {
	if l != nil && l.Sources != nil {
		delete(l.Sources, name)
	}
}

func (l *Lock) SortedSourceNames() []string {
	if l == nil {
		return nil
	}
	names := make([]string, 0, len(l.Sources))
	for name := range l.Sources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (l *Lock) Equal(other *Lock) bool {
	if l == nil && other == nil {
		return true
	}
	if l == nil || other == nil {
		return false
	}
	if len(l.Containers) != len(other.Containers) || len(l.Sources) != len(other.Sources) {
		return false
	}
	for name, c := range l.Containers {
		q, ok := other.Containers[name]
		if !ok || *c != *q {
			return false
		}
	}
	for name, s := range l.Sources {
		q, ok := other.Sources[name]
		if !ok || *s != *q {
			return false
		}
	}
	return true
}

type Change struct {
	Section    string `json:"section"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	OldVersion string `json:"oldVersion,omitempty"`
	OldCommit  string `json:"oldCommit,omitempty"`
	NewVersion string `json:"newVersion,omitempty"`
	NewCommit  string `json:"newCommit,omitempty"`
	Source     string `json:"source,omitempty"`
}

func Diff(old, updated *Lock) []Change {
	if old == nil {
		old = New()
	}
	if updated == nil {
		updated = New()
	}
	var changes []Change
	changes = append(changes, diffSection("container", old.Containers, updated.Containers,
		func(c *LockedContainer) (string, string, string) { return c.Version, c.Commit, c.Source },
		func(a, b *LockedContainer) bool { return *a == *b },
	)...)
	changes = append(changes, diffSection("source", old.Sources, updated.Sources,
		func(s *LockedSource) (string, string, string) { return s.Version, s.Commit, "git" },
		func(a, b *LockedSource) bool { return *a == *b },
	)...)
	return changes
}

func diffSection[T any](
	section string,
	oldMap, newMap map[string]T,
	fields func(T) (version, commit, source string),
	equal func(a, b T) bool,
) []Change {
	seen := make(map[string]bool, len(oldMap)+len(newMap))
	for name := range oldMap {
		seen[name] = true
	}
	for name := range newMap {
		seen[name] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)

	var changes []Change
	for _, name := range names {
		oldV, inOld := oldMap[name]
		newV, inNew := newMap[name]
		switch {
		case !inOld && inNew:
			ver, commit, src := fields(newV)
			changes = append(changes, Change{
				Section: section, Name: name, Kind: "added",
				NewVersion: ver, NewCommit: commit, Source: src,
			})
		case inOld && !inNew:
			ver, commit, src := fields(oldV)
			changes = append(changes, Change{
				Section: section, Name: name, Kind: "removed",
				OldVersion: ver, OldCommit: commit, Source: src,
			})
		case inOld && inNew && !equal(oldV, newV):
			oldVer, oldCommit, _ := fields(oldV)
			newVer, newCommit, src := fields(newV)
			changes = append(changes, Change{
				Section: section, Name: name, Kind: "updated",
				OldVersion: oldVer, OldCommit: oldCommit,
				NewVersion: newVer, NewCommit: newCommit, Source: src,
			})
		}
	}
	return changes
}

func DisplayVersion(version, commit string) string {
	if version != "" {
		return version
	}
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
