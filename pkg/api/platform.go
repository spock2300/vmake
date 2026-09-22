package api

import "runtime"

const (
	TargetOSOptionName     = "target_os"
	TargetTripleOptionName = "target_triple"
)

type Platform struct {
	OS     string
	Triple string
}

func (p Platform) OSOrHost() string {
	if p.OS == "" {
		return runtime.GOOS
	}
	return p.OS
}

func (p *Package) SetPlatform(platform Platform) *Package {
	p.platform = platform
	return p
}

func (p *Package) TargetOS() string     { return p.platform.OSOrHost() }
func (p *Package) TargetTriple() string { return p.platform.Triple }
