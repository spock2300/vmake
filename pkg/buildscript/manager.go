package buildscript

import (
	"path/filepath"
	"plugin"
	"sync"
)

type BuildscriptManager struct {
	mu      sync.RWMutex
	scripts map[string]*plugin.Plugin
	errors  map[string]error
}

var GlobalScript = &BuildscriptManager{
	scripts: make(map[string]*plugin.Plugin),
	errors:  make(map[string]error),
}

func (m *BuildscriptManager) Open(path string) (*plugin.Plugin, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	if p, ok := m.scripts[absPath]; ok {
		m.mu.RUnlock()
		return p, nil
	}
	if cachedErr, ok := m.errors[absPath]; ok {
		m.mu.RUnlock()
		return nil, cachedErr
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if p, ok := m.scripts[absPath]; ok {
		return p, nil
	}
	if cachedErr, ok := m.errors[absPath]; ok {
		return nil, cachedErr
	}

	loaded, loadErr := plugin.Open(absPath)
	if loadErr != nil {
		m.errors[absPath] = loadErr
		return nil, loadErr
	}
	m.scripts[absPath] = loaded
	return loaded, nil
}
