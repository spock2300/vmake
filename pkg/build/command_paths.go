package build

import (
	"path/filepath"
	"runtime"
	"strings"
)

func commandPathFor(hostOS, dir, path string) string {
	if hostOS != "windows" || path == "" {
		return path
	}
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(dir, path); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

func commandPath(dir, path string) string {
	return commandPathFor(runtime.GOOS, dir, path)
}

func commandPathsFor(hostOS, dir string, paths []string) []string {
	result := make([]string, len(paths))
	for i, path := range paths {
		result[i] = commandPathFor(hostOS, dir, path)
	}
	return result
}

func commandPaths(dir string, paths []string) []string {
	return commandPathsFor(runtime.GOOS, dir, paths)
}

func commandFlagsFor(hostOS, dir string, flags []string) []string {
	result := append([]string{}, flags...)
	if hostOS != "windows" {
		return result
	}
	for i, flag := range result {
		if filepath.IsAbs(flag) {
			result[i] = commandPathFor(hostOS, dir, flag)
			continue
		}
		for _, prefix := range []string{"-I", "-L", "-T", "-Wl,--version-script=", "-Wl,-Map="} {
			if strings.HasPrefix(flag, prefix) && filepath.IsAbs(strings.TrimPrefix(flag, prefix)) {
				result[i] = prefix + commandPathFor(hostOS, dir, strings.TrimPrefix(flag, prefix))
				break
			}
		}
	}
	return result
}

func commandFlags(dir string, flags []string) []string {
	return commandFlagsFor(runtime.GOOS, dir, flags)
}
