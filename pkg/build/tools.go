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

	ccPath, err := resolveRequired(mgr, tc, tc.Tools.CC, "CC")
	if err != nil {
		return nil, err
	}

	cxxPath, err := resolveRequired(mgr, tc, tc.Tools.CXX, "CXX")
	if err != nil {
		return nil, err
	}

	arPath, err := resolveRequired(mgr, tc, tc.Tools.AR, "AR")
	if err != nil {
		return nil, err
	}

	return &ResolvedTools{
		CC:      ccPath,
		CXX:     cxxPath,
		AR:      arPath,
		OBJCOPY: resolveOptionalTool(mgr, tc, tc.Tools.OBJCOPY, "OBJCOPY"),
		SIZE:    resolveOptionalTool(mgr, tc, tc.Tools.SIZE, "SIZE"),
		OBJDUMP: resolveOptionalTool(mgr, tc, tc.Tools.OBJDUMP, "OBJDUMP"),
		NM:      resolveOptionalTool(mgr, tc, tc.Tools.NM, "NM"),
	}, nil
}

func resolveRequired(mgr *toolchain.Manager, tc *toolchain.Toolchain, tool, name string) (string, error) {
	path, err := mgr.EnsureToolPath(tc, tool)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", name, err)
	}
	return path, nil
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
