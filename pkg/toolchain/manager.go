package toolchain

import (
	"fmt"
	"sync"
)

type OnMissingToolchain func(name string) (*Toolchain, error)

type Manager struct {
	builtin    *Toolchain
	extensions map[string]*Toolchain
	onMissing  OnMissingToolchain
	mu         sync.RWMutex
}

var defaultManager *Manager
var managerOnce sync.Once

func GetManager() *Manager {
	managerOnce.Do(func() {
		defaultManager = &Manager{
			builtin:    GetBuiltinGCC(),
			extensions: make(map[string]*Toolchain),
		}
	})
	return defaultManager
}

func (m *Manager) SelectToolchain(name string) (*Toolchain, error) {
	if name == "" || name == "gcc" {
		return m.builtin, nil
	}

	m.mu.RLock()
	tc, ok := m.extensions[name]
	m.mu.RUnlock()

	if ok {
		return tc, nil
	}

	if m.onMissing != nil {
		return m.onMissing(name)
	}

	return nil, fmt.Errorf("toolchain '%s' not found", name)
}

func (m *Manager) GetToolchain(name string) (*Toolchain, error) {
	if name == "gcc" {
		return m.builtin, nil
	}

	m.mu.RLock()
	tc, ok := m.extensions[name]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("toolchain '%s' not found", name)
	}

	return tc, nil
}

func (m *Manager) ListToolchains() (map[string]*Toolchain, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*Toolchain)
	result["gcc"] = m.builtin
	for name, tc := range m.extensions {
		result[name] = tc
	}
	return result, nil
}

func (m *Manager) GetDefaultToolchain() string {
	return "gcc"
}

func (m *Manager) RegisterToolchain(name string, tc *Toolchain) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.extensions[name] = tc
}

func (m *Manager) SetOnMissing(fn OnMissingToolchain) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onMissing = fn
}

func (m *Manager) GetOnMissing() OnMissingToolchain {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.onMissing
}
