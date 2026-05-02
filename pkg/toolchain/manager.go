package toolchain

import (
	"fmt"
	"slices"
	"sync"
)

type OnMissingToolchain func(name string) (*Toolchain, error)

type Manager struct {
	builtin        *Toolchain
	extensions     map[string]*Toolchain
	onMissing      map[string]OnMissingToolchain
	globalCFlags   []string
	globalCxxFlags []string
	globalLdFlags  []string
	globalLinks    []string
	mu             sync.RWMutex
}

var defaultManager *Manager
var managerOnce sync.Once

func GetManager() *Manager {
	managerOnce.Do(func() {
		defaultManager = &Manager{
			builtin:    GetBuiltinHost(),
			extensions: make(map[string]*Toolchain),
			onMissing:  make(map[string]OnMissingToolchain),
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

	if ok && tc.InstallPath != "" {
		return tc, nil
	}

	m.mu.RLock()
	onMissing, hasHandler := m.onMissing[name]
	m.mu.RUnlock()

	if hasHandler {
		return onMissing(name)
	}

	if ok {
		return tc, nil
	}

	return nil, fmt.Errorf("toolchain '%s' not found", name)
}

func (m *Manager) GetToolchain(name string) (*Toolchain, error) {
	if name == "" || name == "host" {
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

func (m *Manager) AddGlobalCFlags(flags ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range flags {
		if !slices.Contains(m.globalCFlags, f) {
			m.globalCFlags = append(m.globalCFlags, f)
		}
	}
}

func (m *Manager) AddGlobalCxxFlags(flags ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range flags {
		if !slices.Contains(m.globalCxxFlags, f) {
			m.globalCxxFlags = append(m.globalCxxFlags, f)
		}
	}
}

func (m *Manager) AddGlobalLdFlags(flags ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range flags {
		if !slices.Contains(m.globalLdFlags, f) {
			m.globalLdFlags = append(m.globalLdFlags, f)
		}
	}
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

func (m *Manager) GetGlobalLdFlags() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string{}, m.globalLdFlags...)
}

func (m *Manager) AddGlobalLinks(links ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range links {
		if !slices.Contains(m.globalLinks, l) {
			m.globalLinks = append(m.globalLinks, l)
		}
	}
}

func (m *Manager) GetGlobalLinks() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string{}, m.globalLinks...)
}

func (m *Manager) ResolveToolPath(tc *Toolchain, tool string) (string, error) {
	return ResolveToolPath(tool, tc.InstallPath)
}

func (m *Manager) RegisterToolchain(name string, tc *Toolchain) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.extensions[name] = tc
}

func (m *Manager) RegisterDef(def *ToolchainDef, toolchainsDir string) {
	tc := def.ToToolchain(toolchainsDir)
	m.RegisterToolchain(def.Name, tc)
}

func (m *Manager) SetOnMissing(name string, fn OnMissingToolchain) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onMissing[name] = fn
}
