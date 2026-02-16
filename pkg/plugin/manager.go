package plugin

import (
	"path/filepath"
	"plugin"
	"sync"
)

type PluginManager struct {
	mu      sync.RWMutex
	plugins map[string]*plugin.Plugin
	errors  map[string]error
}

var GlobalManager = &PluginManager{
	plugins: make(map[string]*plugin.Plugin),
	errors:  make(map[string]error),
}

func (m *PluginManager) Open(path string) (*plugin.Plugin, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	if p, ok := m.plugins[absPath]; ok {
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

	if p, ok := m.plugins[absPath]; ok {
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
	m.plugins[absPath] = loaded
	return loaded, nil
}
