package build

import (
	"fmt"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type ResolvedTools struct {
	CC      string
	CXX     string
	AR      string
	OBJCOPY string
	SIZE    string
	OBJDUMP string
	NM      string

	CCVersion  string
	CXXVersion string
}

// CCKey combines the C compiler path with its reported version. An in-place
// compiler upgrade keeps the path but changes the version, which must rotate
// build keys instead of reusing stale objects.
func (t *ResolvedTools) CCKey() string {
	if t.CCVersion == "" {
		return t.CC
	}
	return t.CC + "@" + t.CCVersion
}

func compilerVersion(path string) (string, error) {
	out, err := iexec.Run(path, "-dumpversion")
	if err != nil {
		return "", fmt.Errorf("probe %s version: %w", path, err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("probe %s version: empty output", path)
	}
	return v, nil
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

	ccVersion, err := compilerVersion(ccPath)
	if err != nil {
		return nil, err
	}
	cxxVersion, err := compilerVersion(cxxPath)
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

		CCVersion:  ccVersion,
		CXXVersion: cxxVersion,
	}, nil
}

func resolveRequired(mgr *toolchain.Manager, tc *toolchain.Toolchain, tool, name string) (string, error) {
	path, err := mgr.ResolveToolPath(tc, tool)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", name, err)
	}
	return path, nil
}

func resolveOptionalTool(mgr *toolchain.Manager, tc *toolchain.Toolchain, configured, name string) string {
	if configured != "" {
		if path, err := mgr.ResolveToolPath(tc, configured); err == nil {
			return path
		}
	}
	if tc.Prefix != "" {
		if path, err := mgr.ResolveToolPath(tc, tc.Prefix+name); err == nil {
			return path
		}
	}
	return ""
}
