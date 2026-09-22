package exec

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func lookPathEnv(name string, env []string) (string, error) {
	if name == "" || name == "." || name == ".." {
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	extensions, _ := envValue(env, "PATHEXT")
	if extensions == "" {
		extensions = ".COM;.EXE;.BAT;.CMD"
	}
	var suffixes []string
	for _, extension := range strings.Split(extensions, ";") {
		if extension == "" {
			continue
		}
		if extension[0] != '.' {
			extension = "." + extension
		}
		suffixes = append(suffixes, extension)
	}
	find := func(candidate string) string {
		candidates := make([]string, 0, len(suffixes)+1)
		if filepath.Ext(candidate) != "" || len(suffixes) == 0 {
			candidates = append(candidates, candidate)
		}
		for _, suffix := range suffixes {
			candidates = append(candidates, candidate+suffix)
		}
		for _, path := range candidates {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path
			}
		}
		return ""
	}
	resolved := func(path string) (string, error) {
		if len(suffixes) == 0 && filepath.Ext(path) == "" {
			return "", &exec.Error{Name: name, Err: fmt.Errorf("cannot execute extensionless program %q with a command-specific PATHEXT", path)}
		}
		return path, nil
	}
	var relative string
	if _, disabled := envValue(env, "NoDefaultCurrentDirectoryInExePath"); !disabled {
		if found := find("." + string(filepath.Separator) + name); found != "" {
			if allowRelativeExecutable() {
				return resolved(found)
			}
			relative = found
		}
	}
	path, _ := envValue(env, "PATH")
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		found := find(filepath.Join(dir, name))
		if found == "" {
			continue
		}
		if relative != "" {
			relativeInfo, relativeErr := os.Lstat(relative)
			foundInfo, foundErr := os.Lstat(found)
			if relativeErr != nil || foundErr != nil || !os.SameFile(relativeInfo, foundInfo) {
				return relative, &exec.Error{Name: name, Err: exec.ErrDot}
			}
		}
		if !filepath.IsAbs(found) && !allowRelativeExecutable() {
			if relative == "" {
				relative = found
			}
			continue
		}
		return resolved(found)
	}
	if relative != "" {
		return relative, &exec.Error{Name: name, Err: exec.ErrDot}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}
