package build

import (
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/pkg/api"
)

func resolveWorkPath(workDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workDir, path)
}

func expandBuildDirToken(buildDir, path string) string {
	if buildDir == "" || path == "" {
		return path
	}
	slashed := filepath.ToSlash(path)
	if slashed == api.BuildDirToken {
		return buildDir
	}
	prefix := api.BuildDirToken + "/"
	if strings.HasPrefix(slashed, prefix) {
		return filepath.Join(buildDir, filepath.FromSlash(strings.TrimPrefix(slashed, prefix)))
	}
	return path
}

func resolvePkgPath(sourceDir, buildDir, path string) string {
	if path == "" {
		return ""
	}
	return resolveWorkPath(sourceDir, expandBuildDirToken(buildDir, path))
}
