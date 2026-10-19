package build

import (
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/pkg/api"
)

func postLinkOutputPaths(target *api.Target, sourceDir, outputPath string) []string {
	absOutput := filepath.Clean(resolveWorkPath(sourceDir, outputPath))
	var paths []string
	for _, expanded := range expandPostLinkArgs(target.PostLinkOutputs(), sourceDir, outputPath) {
		path := filepath.Clean(resolveWorkPath(sourceDir, expanded))
		if path != absOutput {
			paths = append(paths, path)
		}
	}
	return unique(paths)
}

func expandPostLinkArgs(templates []string, sourceDir, outputPath string) []string {
	buildDir := ""
	if outputPath != "" {
		buildDir = filepath.Dir(outputPath)
	}
	outputPath = commandPath(sourceDir, outputPath)
	args := make([]string, len(templates))
	for index, template := range templates {
		expanded := strings.ReplaceAll(template, "{output}", outputPath)
		if flag, val, ok := strings.Cut(expanded, "="); ok && strings.HasPrefix(flag, "-") {
			args[index] = flag + "=" + expandBuildDirToken(buildDir, val)
		} else {
			args[index] = expandBuildDirToken(buildDir, expanded)
		}
	}
	return args
}
