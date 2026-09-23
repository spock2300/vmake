package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
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
	env      map[string]string

	CCVersion  string
	CXXVersion string
}

func (t *ResolvedTools) isClangCC() bool {
	return strings.Contains(strings.ToLower(t.CCVersion), "clang version")
}

func (t *ResolvedTools) CCKey() string {
	if t.identity != "" {
		return t.CC + "@" + t.identity
	}
	if t.CCVersion == "" {
		return t.CC
	}
	return t.CC + "@" + t.CCVersion
}

func compilerVersion(path string, env map[string]string) (string, error) {
	return compilerVersionContext(context.Background(), path, env)
}

func compilerVersionContext(ctx context.Context, path string, env map[string]string) (string, error) {
	out, err := iexec.RunWithOptions(path, []string{"--version"}, iexec.RunOptions{Quiet: true, Env: env, Context: ctx})
	if err != nil {
		return "", fmt.Errorf("probe %s version: %w", path, err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("probe %s version: empty output", path)
	}
	return v, nil
}

func ResolveTools(tc *toolchain.Toolchain, platform api.Platform) (*ResolvedTools, error) {
	return resolveTools(context.Background(), tc, platform)
}

func resolveTools(ctx context.Context, tc *toolchain.Toolchain, platform api.Platform) (*ResolvedTools, error) {
	platform.OS = platform.OSOrHost()
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

	env := tc.CommandEnv()
	ccVersion, err := compilerVersionContext(ctx, ccPath, env)
	if err != nil {
		return nil, err
	}
	cxxVersion, err := compilerVersionContext(ctx, cxxPath, env)
	if err != nil {
		return nil, err
	}

	resolved := &ResolvedTools{
		CC: ccPath, CXX: cxxPath, AR: arPath,
		CCVersion: ccVersion, CXXVersion: cxxVersion,
		targetOS: platform.OS,
		env:      env,
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
	digests := make(map[string]string)
	for _, path := range []string{resolved.CC, resolved.CXX, resolved.AR, resolved.OBJCOPY, resolved.SIZE, resolved.OBJDUMP, resolved.NM, resolved.STRIP} {
		if path == "" {
			continue
		}
		if _, ok := digests[path]; ok {
			continue
		}
		hash, err := FileHash(path)
		if err != nil {
			return nil, fmt.Errorf("fingerprint tool %s: %w", path, err)
		}
		digests[path] = hash
	}
	data, err := json.Marshal(struct {
		HostOS    string
		HostArch  string
		Toolchain *toolchain.Toolchain
		Resolved  *ResolvedTools
		Platform  api.Platform
		Digests   map[string]string
	}{runtime.GOOS, runtime.GOARCH, tc, resolved, platform, digests})
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
