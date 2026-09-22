package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type ResolvedTools struct {
	CC       string
	CXX      string
	AR       string
	OBJCOPY  string
	SIZE     string
	OBJDUMP  string
	NM       string
	STRIP    string
	identity string
	targetOS string

	CCVersion  string
	CXXVersion string
}

func (t *ResolvedTools) isClangCC() bool {
	return strings.Contains(strings.ToLower(t.CCVersion), "clang version")
}

// CCKey combines the C compiler path with its reported version. An in-place
// compiler upgrade keeps the path but changes the version, which must rotate
// build keys instead of reusing stale objects.
func (t *ResolvedTools) CCKey() string {
	if t.identity != "" {
		return t.CC + "@" + t.identity
	}
	if t.CCVersion == "" {
		return t.CC
	}
	return t.CC + "@" + t.CCVersion
}

func compilerVersion(path string) (string, error) {
	out, err := iexec.RunWithOptions(path, []string{"--version"}, iexec.RunOptions{Quiet: true})
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

	resolved := &ResolvedTools{
		CC: ccPath, CXX: cxxPath, AR: arPath,
		CCVersion: ccVersion, CXXVersion: cxxVersion,
		targetOS: tc.TargetOSOrDefault(),
	}
	for _, item := range []struct {
		name, configured string
		dest             *string
	}{
		{"OBJCOPY", tc.Tools.OBJCOPY, &resolved.OBJCOPY},
		{"SIZE", tc.Tools.SIZE, &resolved.SIZE},
		{"OBJDUMP", tc.Tools.OBJDUMP, &resolved.OBJDUMP},
		{"NM", tc.Tools.NM, &resolved.NM},
		{"STRIP", tc.Tools.STRIP, &resolved.STRIP},
	} {
		if item.configured == "" {
			continue
		}
		path, err := resolveRequired(mgr, tc, item.configured, item.name)
		if err != nil {
			return nil, err
		}
		*item.dest = path
	}
	data, err := json.Marshal(struct {
		HostOS    string
		HostArch  string
		Toolchain *toolchain.Toolchain
		Resolved  *ResolvedTools
	}{runtime.GOOS, runtime.GOARCH, tc, resolved})
	if err != nil {
		return nil, fmt.Errorf("toolchain identity: %w", err)
	}
	hash := sha256.Sum256(data)
	resolved.identity = hex.EncodeToString(hash[:])
	return resolved, nil
}

func resolveRequired(mgr *toolchain.Manager, tc *toolchain.Toolchain, tool, name string) (string, error) {
	path, err := mgr.ResolveToolPath(tc, tool)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", name, err)
	}
	return path, nil
}
