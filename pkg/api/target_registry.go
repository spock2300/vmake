package api

type TargetRegistry struct {
	targets         map[string]*Target
	defaultCFlags   []string
	defaultCxxFlags []string
}

func NewTargetRegistry() *TargetRegistry {
	return &TargetRegistry{
		targets: make(map[string]*Target),
	}
}

func (r *TargetRegistry) SetDefaultFlags(cflags, cxxflags []string) {
	r.defaultCFlags = append([]string{}, cflags...)
	r.defaultCxxFlags = append([]string{}, cxxflags...)
}

func (r *TargetRegistry) Target(name string) *Target {
	if t, ok := r.targets[name]; ok {
		return t
	}
	t := &Target{
		name:      name,
		kind:      TargetBinary,
		isDefault: true,
		cflags:    append([]string{}, r.defaultCFlags...),
		cxxflags:  append([]string{}, r.defaultCxxFlags...),
		ldflags:   nil,
	}
	r.targets[name] = t
	return t
}

func (r *TargetRegistry) GetTargets() map[string]*Target {
	return r.targets
}
