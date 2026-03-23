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

func NewBuildState(tc *toolchain.Toolchain) *BuildState {
	ccPath, _ := toolchain.ResolveToolPath(tc.Tools.CC, tc.InstallPath)
	cxxPath, _ := toolchain.ResolveToolPath(tc.Tools.CXX, tc.InstallPath)
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
	if err := jsonio.Load(filepath.Join("build", tcName, "state.json"), &state); err != nil {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}
	return &state, nil
}

func (s *BuildState) Save(tcName string) error {
	return jsonio.Save(filepath.Join("build", tcName, "state.json"), s)
}

func (s *BuildState) NeedFullRebuild(tc *toolchain.Toolchain) bool {
	ccPath, _ := toolchain.ResolveToolPath(tc.Tools.CC, tc.InstallPath)
	cxxPath, _ := toolchain.ResolveToolPath(tc.Tools.CXX, tc.InstallPath)

	return s.Toolchain.Name != tc.Name ||
		s.Toolchain.CCPath != ccPath ||
		s.Toolchain.CXXPath != cxxPath
}

func CleanObjects(tcName string) error {
	return os.RemoveAll(filepath.Join("build", tcName, "objects"))
}
