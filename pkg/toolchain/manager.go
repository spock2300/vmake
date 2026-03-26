package toolchain

import (
	"fmt"
	"sync"
)

type OnMissingToolchain func(name string) (*Toolchain, error)

type Manager struct {
	builtin        *Toolchain
	extensions     map[string]*Toolchain
	onMissing      OnMissingToolchain
	globalCFlags   []string
	globalCxxFlags []string
	mu             sync.RWMutex
}

var defaultManager *Manager
var managerOnce sync.Once
var onToolMissing func(name string) error

func SetOnToolMissing(fn func(string) error) {
	onToolMissing = fn
}

func GetManager() *Manager {
	managerOnce.Do(func() {
		defaultManager = &Manager{
			builtin:    GetBuiltinHost(),
			extensions: make(map[string]*Toolchain),
		}
	})
	return defaultManager
}

func (m *Manager) SelectToolchain(name string) (*Toolchain, error) {
	if name == "" || name == "host" {
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
	if name == "host" {
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
	result["host"] = m.builtin
	for name, tc := range m.extensions {
		result[name] = tc
	}
	return result, nil
}

func (m *Manager) GetDefaultToolchain() string {
	return "host"
}

func (m *Manager) GetBuiltin() *Toolchain {
	return m.builtin
}

func (m *Manager) AddGlobalFlags(cflags, cxxflags []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.globalCFlags = append(m.globalCFlags, cflags...)
	m.globalCxxFlags = append(m.globalCxxFlags, cxxflags...)
}

func (m *Manager) GetGlobalCFlags() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string{}, m.globalCFlags...)
}

func (m *Manager) GetGlobalCxxFlags() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string{}, m.globalCxxFlags...)
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

func (m *Manager) EnsureToolPath(tc *Toolchain, tool string) (string, error) {
	path, err := ResolveToolPath(tool, tc.InstallPath)
	if err == nil {
		return path, nil
	}

	if onToolMissing != nil && tc.Name != "" {
		if dlErr := onToolMissing(tc.Name); dlErr == nil {
			return ResolveToolPath(tool, tc.InstallPath)
		}
	}

	return "", fmt.Errorf("tool '%s' not found", tool)
}
