package build

import (
	"path/filepath"
	"runtime"
	"strings"
)

func commandPath(dir, path string) string {
	if runtime.GOOS != "windows" || path == "" {
		return path
	}
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(dir, path); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

func commandPaths(dir string, paths []string) []string {
	result := make([]string, len(paths))
	for i, path := range paths {
		result[i] = commandPath(dir, path)
	}
	return result
}

func commandFlags(dir string, flags []string) []string {
	result := append([]string{}, flags...)
	if runtime.GOOS != "windows" {
		return result
	}
	for i, flag := range result {
		if filepath.IsAbs(flag) {
			result[i] = commandPath(dir, flag)
			continue
		}
		for _, prefix := range []string{"-I", "-L", "-T", "-Wl,--version-script=", "-Wl,-Map="} {
			if strings.HasPrefix(flag, prefix) && filepath.IsAbs(strings.TrimPrefix(flag, prefix)) {
				result[i] = prefix + commandPath(dir, strings.TrimPrefix(flag, prefix))
				break
			}
		}
	}
	return result
}

func compileArgs(opts *CompileOptions, objPath, src string, flags []string, depPath, dir string) []string {
	commandOpts := *opts
	commandOpts.Includes = commandPaths(dir, opts.Includes)
	return BuildCompileArgs(&commandOpts, commandPath(dir, objPath), commandPath(dir, src), commandFlags(dir, flags), commandPath(dir, depPath))
}
