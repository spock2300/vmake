package api

import (
	cmake "github.com/spock2300/vmake/internal/cmake"
)

func (p *Package) cmakeConfigureStampPath() string {
	return cmake.StampPath(p.cmakeSettings())
}

func (p *Package) cmakeConfigureState(args []string) (string, bool) {
	return cmake.State(p.cmakeSettings(), args)
}

func (p *Package) saveCMakeConfigureStamp(args []string, fingerprint string) error {
	return cmake.SaveStamp(p.cmakeSettings(), args, fingerprint)
}
