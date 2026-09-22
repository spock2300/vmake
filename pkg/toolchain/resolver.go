package toolchain

import (
	"errors"
	"fmt"
	"path/filepath"

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
	if tc.TargetOS == "" {
		errs = append(errs, errors.New("target_os is not configured"))
	}

	tools := []struct {
		name     string
		path     string
		required bool
	}{
		{"cc", tc.Tools.CC, true},
		{"cxx", tc.Tools.CXX, true},
		{"ar", tc.Tools.AR, true},
		{"ld", tc.Tools.LD, true},
		{"strip", tc.Tools.STRIP, false},
		{"ranlib", tc.Tools.RANLIB, false},
		{"objcopy", tc.Tools.OBJCOPY, false},
		{"size", tc.Tools.SIZE, false},
		{"objdump", tc.Tools.OBJDUMP, false},
		{"nm", tc.Tools.NM, false},
		{"make", tc.Tools.MAKE, false},
	}

	for _, t := range tools {
		if t.path == "" {
			if t.required {
				errs = append(errs, errors.New(t.name+" is not configured"))
			}
			continue
		}
		_, err := ResolveToolPath(t.path, tc.InstallPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %q: %w", t.name, t.path, err))
		}
	}

	return errs
}
