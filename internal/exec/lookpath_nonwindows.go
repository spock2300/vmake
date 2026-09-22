//go:build !windows

package exec

import (
	"os/exec"
	"path/filepath"
)

func lookPathEnv(name string, env []string) (string, error) {
	if name == "" || name == "." || name == ".." {
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	path, _ := envValue(env, "PATH")
	for _, dir := range filepath.SplitList(path) {
		candidate := filepath.Join(dir, name)
		if filepath.Base(candidate) == candidate {
			candidate = "." + string(filepath.Separator) + candidate
		}
		resolved, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(resolved) && !allowRelativeExecutable() {
			return resolved, &exec.Error{Name: name, Err: exec.ErrDot}
		}
		return resolved, nil
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}
