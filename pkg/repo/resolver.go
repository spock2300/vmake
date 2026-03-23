package repo

import "gitee.com/spock2300/vmake/pkg/api"

type DependencyGraph struct {
	Order    []string
	Packages map[string]*ResolvedPackage
}

type ResolvedPackage struct {
	Name       string
	Constraint string
	Options    map[string]any
	Definition *api.Package
	Source     *api.Package
	Deps       []string
	Deferred   bool
}

func (p *ResolvedPackage) IsLocal() bool {
	return p.Source != nil && p.Source.IsLocal()
}
