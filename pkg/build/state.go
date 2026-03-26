package build

import (
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/jsonio"
	"gitee.com/spock2300/vmake/pkg/toolchain"
)

const StateVersion = 1

type BuildState struct {
	Version   int           `json:"version"`
	Toolchain ToolchainMeta `json:"toolchain"`
	Mode      string        `json:"mode"`
}

type ToolchainMeta struct {
	Name    string `json:"name"`
	CCPath  string `json:"cc_path"`
	CXXPath string `json:"cxx_path"`
	Host    string `json:"host"`
}

func (s *BuildState) statePath(tcName string) string {
	return filepath.Join("build", tcName, "state.json")
}

func resolveCCAndCXX(tc *toolchain.Toolchain) (string, string) {
	cc, _ := toolchain.ResolveToolPath(tc.Tools.CC, tc.InstallPath)
	cxx, _ := toolchain.ResolveToolPath(tc.Tools.CXX, tc.InstallPath)
	return cc, cxx
}

func NewBuildState(tc *toolchain.Toolchain) *BuildState {
	ccPath, cxxPath := resolveCCAndCXX(tc)
	host := tc.Host
	if host == "" {
		host = toolchain.GetToolchainHost(tc)
	}

	return &BuildState{
		Version: StateVersion,
		Toolchain: ToolchainMeta{
			Name:    tc.Name,
			CCPath:  ccPath,
			CXXPath: cxxPath,
			Host:    host,
		},
	}
}

func LoadState(tcName string) (*BuildState, error) {
	var state BuildState
	if err := jsonio.Load((&BuildState{}).statePath(tcName), &state); err != nil {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}
	return &state, nil
}

func (s *BuildState) Save(tcName string) error {
	return jsonio.Save(s.statePath(tcName), s)
}

func (s *BuildState) NeedFullRebuild(tc *toolchain.Toolchain) bool {
	ccPath, cxxPath := resolveCCAndCXX(tc)

	return s.Toolchain.Name != tc.Name ||
		s.Toolchain.CCPath != ccPath ||
		s.Toolchain.CXXPath != cxxPath
}

func CleanObjects(tcName string) error {
	return os.RemoveAll(filepath.Join("build", tcName, "objects"))
}
