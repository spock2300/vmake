package build

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type TargetSize struct {
	Text  int64 `json:"text"`
	Data  int64 `json:"data"`
	Bss   int64 `json:"bss"`
	Total int64 `json:"total"`
}

type BuildStats struct {
	mu                sync.Mutex
	compiled          int
	linked            int
	upToDate          int
	plannedCompiles   int
	plannedLinks      int
	plannedCallbacks  int
	plannedGenerators int
	compiledFiles     map[string]bool
	changedTargets    map[string]bool
	plannedTargets    map[string]bool
	compilePlanned    map[string]bool
	generatorTargets  map[string]bool
	targetDuration    map[string]int64
	targetSize        map[string]*TargetSize
}

type StatsSnapshot struct {
	Compiled          int
	Linked            int
	UpToDate          int
	PlannedCompiles   int
	PlannedLinks      int
	PlannedCallbacks  int
	PlannedGenerators int
	CompiledFiles     []string
	ChangedTargets    []string
}

func NewBuildStats() *BuildStats {
	return &BuildStats{
		compiledFiles:    make(map[string]bool),
		changedTargets:   make(map[string]bool),
		plannedTargets:   make(map[string]bool),
		compilePlanned:   make(map[string]bool),
		generatorTargets: make(map[string]bool),
		targetDuration:   make(map[string]int64),
		targetSize:       make(map[string]*TargetSize),
	}
}

func (s *BuildStats) RecordCompile(target, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compiled++
	s.compiledFiles[qualifiedChangedFile(target, source)] = true
	s.changedTargets[target] = true
}

func qualifiedChangedFile(target, source string) string {
	file := filepath.ToSlash(source)
	if filepath.IsAbs(source) || strings.Contains(file, ":") {
		return file
	}
	pkg, _, ok := strings.Cut(target, ":")
	if !ok {
		pkg = target
	}
	return pkg + ":" + file
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

func (s *BuildStats) RecordPlannedCompile(target, _ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plannedCompiles++
	s.compilePlanned[target] = true
	s.plannedTargets[target] = true
}

func (s *BuildStats) RecordPlannedLink(target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plannedLinks++
	s.plannedTargets[target] = true
}

func (s *BuildStats) RecordPlannedCallback(target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plannedCallbacks++
}

func (s *BuildStats) RecordPlannedGenerator(target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generatorTargets[target] {
		return
	}
	s.generatorTargets[target] = true
	s.plannedGenerators++
}

func (s *BuildStats) CompilePlanned(target string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compilePlanned[target]
}

func (s *BuildStats) TargetPlanned(target string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.plannedTargets[target]
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

func (s *BuildStats) RecordTargetSize(target string, size *TargetSize) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targetSize[target] = size
}

func (s *BuildStats) TargetSize(target string) *TargetSize {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.targetSize[target]
}

func (s *BuildStats) Snapshot() StatsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return StatsSnapshot{
		Compiled:          s.compiled,
		Linked:            s.linked,
		UpToDate:          s.upToDate,
		PlannedCompiles:   s.plannedCompiles,
		PlannedLinks:      s.plannedLinks,
		PlannedCallbacks:  s.plannedCallbacks,
		PlannedGenerators: s.plannedGenerators,
		CompiledFiles:     sortedKeys(s.compiledFiles),
		ChangedTargets:    sortedKeys(s.changedTargets),
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
