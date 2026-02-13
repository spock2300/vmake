package toolchain

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
)

func ResolveToolPath(tool string, installPath string) (string, error) {
	if filepath.IsAbs(tool) {
		return tool, nil
	}

	if installPath != "" {
		absPath := filepath.Join(installPath, "bin", tool)
		if _, err := exec.LookPath(absPath); err == nil {
			return absPath, nil
		}
	}

	resolved, err := exec.LookPath(tool)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func ValidateToolchain(tc *Toolchain) []error {
	var errs []error

	tools := []struct {
		name string
		path string
	}{
		{"cc", tc.Tools.CC},
		{"cxx", tc.Tools.CXX},
		{"ar", tc.Tools.AR},
		{"ld", tc.Tools.LD},
	}

	for _, t := range tools {
		if t.path == "" {
			errs = append(errs, errors.New(t.name+" is not configured"))
			continue
		}
		_, err := ResolveToolPath(t.path, tc.InstallPath)
		if err != nil {
			errs = append(errs, errors.New(t.name+": "+t.path+" not found"))
		}
	}

	return errs
}

func GetToolchainHost(tc *Toolchain) string {
	if tc.Host != "" {
		return tc.Host
	}

	cc, err := ResolveToolPath(tc.Tools.CC, tc.InstallPath)
	if err != nil {
		return "unknown"
	}

	cmd := exec.Command(cc, "-dumpmachine")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	return strings.TrimSpace(string(output))
}
