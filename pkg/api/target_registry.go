package api

type TargetRegistry struct {
	targets map[string]*Target
}

func NewTargetRegistry() *TargetRegistry {
	return &TargetRegistry{
		targets: make(map[string]*Target),
	}
}

func (r *TargetRegistry) Target(name string) *Target {
	if t, ok := r.targets[name]; ok {
		return t
	}
	t := &Target{name: name, isDefault: true}
	r.targets[name] = t
	return t
}

func (r *TargetRegistry) GetTargets() map[string]*Target {
	return r.targets
}
