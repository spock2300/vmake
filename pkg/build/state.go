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

func NewBuildState(tc *toolchain.Toolchain) *BuildState {
	tools, _ := ResolveTools(tc)
	host := tc.Host
	if host == "" {
		host = toolchain.GetToolchainHost(tc)
	}

	return &BuildState{
		Version: StateVersion,
		Toolchain: ToolchainMeta{
			Name:    tc.Name,
			CCPath:  tools.CC,
			CXXPath: tools.CXX,
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
	tools, _ := ResolveTools(tc)

	return s.Toolchain.Name != tc.Name ||
		s.Toolchain.CCPath != tools.CC ||
		s.Toolchain.CXXPath != tools.CXX
}

func CleanObjects(tcName string) error {
	return os.RemoveAll(filepath.Join("build", tcName, "objects"))
}
