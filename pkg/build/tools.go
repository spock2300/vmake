package build

import (
	"fmt"

	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type ResolvedTools struct {
	CC      string
	CXX     string
	AR      string
	OBJCOPY string
	SIZE    string
	OBJDUMP string
	NM      string
}

func ResolveTools(tc *toolchain.Toolchain) (*ResolvedTools, error) {
	mgr := toolchain.GetManager()

	ccPath, err := mgr.EnsureToolPath(tc, tc.Tools.CC)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve CC: %w", err)
	}

	cxxPath, err := mgr.EnsureToolPath(tc, tc.Tools.CXX)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve CXX: %w", err)
	}

	arPath, err := mgr.EnsureToolPath(tc, tc.Tools.AR)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve AR: %w", err)
	}

	tools := &ResolvedTools{
		CC:  ccPath,
		CXX: cxxPath,
		AR:  arPath,
	}

	tools.OBJCOPY = resolveOptionalTool(mgr, tc, tc.Tools.OBJCOPY, "OBJCOPY")
	tools.SIZE = resolveOptionalTool(mgr, tc, tc.Tools.SIZE, "SIZE")
	tools.OBJDUMP = resolveOptionalTool(mgr, tc, tc.Tools.OBJDUMP, "OBJDUMP")
	tools.NM = resolveOptionalTool(mgr, tc, tc.Tools.NM, "NM")

	return tools, nil
}

func resolveOptionalTool(mgr *toolchain.Manager, tc *toolchain.Toolchain, configured, name string) string {
	if configured != "" {
		if path, err := mgr.EnsureToolPath(tc, configured); err == nil {
			return path
		}
	}
	if tc.Prefix != "" {
		if path, err := mgr.EnsureToolPath(tc, tc.Prefix+name); err == nil {
			return path
		}
	}
	return ""
}
