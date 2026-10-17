package toolchain

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
)

func ResolveToolPath(tool string, installPath string) (string, error) {
	if filepath.IsAbs(tool) {
		return iexec.LookPath(tool)
	}

	if installPath != "" {
		return iexec.LookPath(filepath.Join(installPath, "bin", tool))
	}

	resolved, err := iexec.LookPath(tool)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func ValidateToolchain(tc *Toolchain) []error {
	var errs []error
	if tc == nil {
		return []error{errors.New("toolchain is nil")}
	}

	for _, tool := range tc.Tools.Entries() {
		name := strings.ToLower(tool.Name)
		if *tool.Value == "" {
			if tool.Required {
				errs = append(errs, errors.New(name+" is not configured"))
			}
			continue
		}
		_, err := ResolveToolPath(*tool.Value, tc.InstallPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %q: %w", name, *tool.Value, err))
		}
	}

	return errs
}
