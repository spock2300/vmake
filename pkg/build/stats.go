package build

import (
	"slices"
	"sync"
	"time"
)

type BuildStats struct {
	mu             sync.Mutex
	compiled       int
	linked         int
	upToDate       int
	compiledFiles  map[string]bool
	changedTargets map[string]bool
	targetDuration map[string]int64
}

type StatsSnapshot struct {
	Compiled       int
	Linked         int
	UpToDate       int
	CompiledFiles  []string
	ChangedTargets []string
}

func NewBuildStats() *BuildStats {
	return &BuildStats{
		compiledFiles:  make(map[string]bool),
		changedTargets: make(map[string]bool),
		targetDuration: make(map[string]int64),
	}
}

func (s *BuildStats) RecordCompile(target, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compiled++
	s.compiledFiles[source] = true
	s.changedTargets[target] = true
}

func (s *BuildStats) RecordLink(target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.linked++
	s.changedTargets[target] = true
}

func (s *BuildStats) RecordUpToDate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upToDate++
}

func (s *BuildStats) ChangedTarget(target string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changedTargets[target]
}

func (s *BuildStats) RecordTargetDuration(target string, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targetDuration[target] = duration.Milliseconds()
}

func (s *BuildStats) TargetDuration(target string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.targetDuration[target]
}

func (s *BuildStats) Snapshot() StatsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return StatsSnapshot{
		Compiled:       s.compiled,
		Linked:         s.linked,
		UpToDate:       s.upToDate,
		CompiledFiles:  sortedKeys(s.compiledFiles),
		ChangedTargets: sortedKeys(s.changedTargets),
	}
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
