package toolchain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

type OnMissingToolchain func(name string) (*Toolchain, error)

type Manager struct {
	builtin          *Toolchain
	extensions       map[string]*Toolchain
	onMissing        map[string]OnMissingToolchain
	definitionErrors map[string]*DefinitionError
	globalCFlags     []string
	globalCxxFlags   []string
	globalLdFlags    []string
	globalLinks      []string
	mu               sync.RWMutex
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
	if err := m.toolchainError(name); err != nil {
		return nil, err
	}
	if name == "" || name == "host" {
		return validatedToolchain(m.builtin)
	}

	m.mu.RLock()
	tc, ok := m.extensions[name]
	m.mu.RUnlock()

	if ok && tc.InstallPath != "" {
		return validatedToolchain(tc)
	}

	m.mu.RLock()
	onMissing, hasHandler := m.onMissing[name]
	m.mu.RUnlock()

	if hasHandler {
		tc, err := onMissing(name)
		if err != nil {
			return nil, err
		}
		return validatedToolchain(tc)
	}

	if ok {
		return validatedToolchain(tc)
	}

	return nil, m.toolchainNotFound(name)
}

func (m *Manager) GetToolchain(name string) (*Toolchain, error) {
	if err := m.toolchainError(name); err != nil {
		return nil, err
	}
	if name == "" || name == "host" {
		return m.builtin, nil
	}

	m.mu.RLock()
	tc, ok := m.extensions[name]
	m.mu.RUnlock()

	if !ok {
		return nil, m.toolchainNotFound(name)
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
	for _, err := range m.definitionErrors {
		delete(result, err.Name())
	}
	return result, nil
}

func (m *Manager) RegisterToolchainError(name, sourcePath string, err error) *Manager {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.definitionErrors == nil {
		m.definitionErrors = make(map[string]*DefinitionError)
	}
	var defErr *DefinitionError
	if errors.As(err, &defErr) && defErr.Path() == sourcePath && defErr.Name() == name {
		m.definitionErrors[sourcePath] = defErr
	} else {
		m.definitionErrors[sourcePath] = &DefinitionError{name: name, path: sourcePath, err: err}
	}
	return m
}

func (m *Manager) ToolchainErrors() []*DefinitionError {
	m.mu.RLock()
	defer m.mu.RUnlock()
	errs := make([]*DefinitionError, 0, len(m.definitionErrors))
	for _, err := range m.definitionErrors {
		errs = append(errs, err)
	}
	slices.SortFunc(errs, func(a, b *DefinitionError) int { return strings.Compare(a.Path(), b.Path()) })
	return errs
}

func (m *Manager) toolchainError(name string) error {
	if name == "" {
		name = "host"
	}
	var errs []error
	for _, err := range m.ToolchainErrors() {
		if err.Name() == name {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) toolchainNotFound(name string) error {
	errs := []error{fmt.Errorf("toolchain '%s' not found", name)}
	for _, err := range m.ToolchainErrors() {
		if err.Name() == "" {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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

func (m *Manager) RegisterDef(def *ToolchainDef, toolchainsDir string) error {
	tc, err := def.ToToolchain(toolchainsDir)
	if err != nil {
		return err
	}
	m.RegisterToolchain(def.Name, tc)
	return nil
}

func (m *Manager) SetOnMissing(name string, fn OnMissingToolchain) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onMissing[name] = fn
}

func validatedToolchain(tc *Toolchain) (*Toolchain, error) {
	if errs := ValidateToolchain(tc); len(errs) > 0 {
		return nil, fmt.Errorf("invalid toolchain: %w", errors.Join(errs...))
	}
	return tc, nil
}
