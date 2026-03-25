package toolchain

import (
	"fmt"
	"os/exec"
	"path/filepath"
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
var onToolMissing func(name string) error

func SetOnToolMissing(fn func(string) error) {
	onToolMissing = fn
}

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

func (m *Manager) EnsureToolPath(tc *Toolchain, tool string) (string, error) {
	if filepath.IsAbs(tool) {
		return tool, nil
	}

	if tc.InstallPath != "" {
		absPath := filepath.Join(tc.InstallPath, "bin", tool)
		if _, err := exec.LookPath(absPath); err == nil {
			return absPath, nil
		}
	}

	if _, err := exec.LookPath(tool); err == nil {
		return tool, nil
	}

	if onToolMissing != nil && tc.Name != "" {
		if dlErr := onToolMissing(tc.Name); dlErr == nil {
			if tc.InstallPath != "" {
				absPath := filepath.Join(tc.InstallPath, "bin", tool)
				if _, err := exec.LookPath(absPath); err == nil {
					return absPath, nil
				}
			}
			if _, err := exec.LookPath(tool); err == nil {
				return tool, nil
			}
		}
	}

	return "", fmt.Errorf("tool '%s' not found", tool)
}
