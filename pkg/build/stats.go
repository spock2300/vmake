package build

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

type TargetSize struct {
	Text  int64 `json:"text"`
	Data  int64 `json:"data"`
	Bss   int64 `json:"bss"`
	Total int64 `json:"total"`
}

type BuildStats struct {
	mu            sync.Mutex
	compiled      int
	linked        int
	callbacks     int
	upToDate      int
	compiledFiles map[string]bool
	targetSize    map[string]*TargetSize
}

type StatsSnapshot struct {
	Compiled      int
	Linked        int
	Callbacks     int
	UpToDate      int
	CompiledFiles []string
}

func NewBuildStats() *BuildStats {
	return &BuildStats{
		compiledFiles: make(map[string]bool),
		targetSize:    make(map[string]*TargetSize),
	}
}

func (s *BuildStats) RecordCompile(target, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compiled++
	s.compiledFiles[qualifiedChangedFile(target, source)] = true
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
}

func (s *BuildStats) RecordCallback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callbacks++
}

func (s *BuildStats) RecordUpToDate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upToDate++
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
		Compiled:      s.compiled,
		Linked:        s.linked,
		Callbacks:     s.callbacks,
		UpToDate:      s.upToDate,
		CompiledFiles: sortedKeys(s.compiledFiles),
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
