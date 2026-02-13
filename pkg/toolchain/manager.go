package toolchain

import (
	"fmt"
	"sync"
)

type Manager struct {
	globalCfg *GlobalConfig
	once      sync.Once
	initErr   error
}

var defaultManager *Manager
var managerOnce sync.Once

func GetManager() *Manager {
	managerOnce.Do(func() {
		defaultManager = &Manager{}
	})
	return defaultManager
}

func (m *Manager) loadOnce() {
	m.once.Do(func() {
		m.globalCfg, m.initErr = LoadGlobal()
	})
}

func (m *Manager) SelectToolchain(projectToolchain string) (*Toolchain, error) {
	m.loadOnce()
	if m.initErr != nil {
		return nil, m.initErr
	}

	name := projectToolchain
	if name == "" {
		name = m.globalCfg.DefaultToolchain
	}
	if name == "" {
		name = "gcc"
	}

	tc, ok := m.globalCfg.Toolchains[name]
	if !ok {
		return nil, fmt.Errorf("toolchain '%s' not found", name)
	}

	return tc, nil
}

func (m *Manager) GetToolchain(name string) (*Toolchain, error) {
	m.loadOnce()
	if m.initErr != nil {
		return nil, m.initErr
	}

	tc, ok := m.globalCfg.Toolchains[name]
	if !ok {
		return nil, fmt.Errorf("toolchain '%s' not found", name)
	}

	return tc, nil
}

func (m *Manager) ListToolchains() (map[string]*Toolchain, error) {
	m.loadOnce()
	if m.initErr != nil {
		return nil, m.initErr
	}

	return m.globalCfg.Toolchains, nil
}

func (m *Manager) GetDefaultToolchain() string {
	m.loadOnce()
	if m.initErr != nil {
		return "gcc"
	}
	return m.globalCfg.DefaultToolchain
}
