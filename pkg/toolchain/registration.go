package toolchain

import "fmt"

type Registration struct {
	manager    *Manager
	toolchains map[string]*Toolchain
	onMissing  map[string]OnMissingToolchain
	cflags     []string
	cxxflags   []string
	ldflags    []string
	committed  bool
	aborted    bool
	err        error
}

func NewRegistration(manager *Manager) *Registration {
	return &Registration{
		manager:    manager,
		toolchains: make(map[string]*Toolchain),
		onMissing:  make(map[string]OnMissingToolchain),
	}
}

func (r *Registration) RegisterToolchain(name string, tc *Toolchain) error {
	if r.aborted {
		return fmt.Errorf("plugin registration was aborted")
	}
	if r.committed {
		if tc == nil {
			return fmt.Errorf("invalid toolchain registration %q", name)
		}
		snapshot := *tc
		return r.manager.RegisterToolchain(name, &snapshot)
	}
	if !validPathComponent(name) || name == "host" || tc == nil || tc.Name != name {
		return fmt.Errorf("invalid toolchain registration %q", name)
	}
	if _, exists := r.toolchains[name]; exists {
		return fmt.Errorf("toolchain %q is already registered", name)
	}
	r.manager.mu.RLock()
	_, exists := r.manager.extensions[name]
	r.manager.mu.RUnlock()
	if exists {
		return fmt.Errorf("toolchain %q is already registered", name)
	}
	snapshot := *tc
	r.toolchains[name] = &snapshot
	return nil
}

func (r *Registration) SetOnMissing(name string, fn OnMissingToolchain) {
	if r.aborted {
		return
	}
	if !validPathComponent(name) || name == "host" || fn == nil {
		r.err = fmt.Errorf("invalid missing-toolchain registration %q", name)
		return
	}
	if r.committed {
		r.manager.SetOnMissing(name, fn)
		return
	}
	r.onMissing[name] = fn
}

func (r *Registration) GetToolchains() map[string]*Toolchain {
	tcs, _ := r.manager.ListToolchains()
	for name, tc := range r.toolchains {
		tcs[name] = tc
	}
	for name, tc := range tcs {
		if tc != nil {
			snapshot := *tc
			tcs[name] = &snapshot
		}
	}
	return tcs
}

func (r *Registration) AddGlobalCFlags(flags ...string) {
	if r.committed {
		r.manager.AddGlobalCFlags(flags...)
	} else if !r.aborted {
		r.cflags = append(r.cflags, flags...)
	}
}

func (r *Registration) AddGlobalCxxFlags(flags ...string) {
	if r.committed {
		r.manager.AddGlobalCxxFlags(flags...)
	} else if !r.aborted {
		r.cxxflags = append(r.cxxflags, flags...)
	}
}

func (r *Registration) AddGlobalLdFlags(flags ...string) {
	if r.committed {
		r.manager.AddGlobalLdFlags(flags...)
	} else if !r.aborted {
		r.ldflags = append(r.ldflags, flags...)
	}
}

func (r *Registration) Abort() {
	if r.committed {
		return
	}
	r.aborted = true
	r.toolchains = nil
	r.onMissing = nil
	r.cflags, r.cxxflags, r.ldflags = nil, nil, nil
}

func (r *Registration) Commit() error {
	if r.aborted {
		return fmt.Errorf("plugin registration was aborted")
	}
	if r.err != nil {
		return r.err
	}
	if r.committed {
		return nil
	}
	m := r.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	for name := range r.toolchains {
		if _, exists := m.extensions[name]; exists {
			return fmt.Errorf("toolchain %q is already registered", name)
		}
	}
	if m.extensions == nil {
		m.extensions = make(map[string]*Toolchain)
	}
	if m.onMissing == nil {
		m.onMissing = make(map[string]OnMissingToolchain)
	}
	for name, tc := range r.toolchains {
		m.extensions[name] = tc
	}
	for name, fn := range r.onMissing {
		m.onMissing[name] = fn
	}
	m.globalCFlags = append(m.globalCFlags, r.cflags...)
	m.globalCxxFlags = append(m.globalCxxFlags, r.cxxflags...)
	m.globalLdFlags = append(m.globalLdFlags, r.ldflags...)
	r.toolchains = nil
	r.onMissing = nil
	r.cflags, r.cxxflags, r.ldflags = nil, nil, nil
	r.committed = true
	return nil
}
