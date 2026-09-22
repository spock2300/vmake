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
	outputPath = commandPath(sourceDir, outputPath)
	args := make([]string, len(templates))
	for index, template := range templates {
		args[index] = strings.ReplaceAll(template, "{output}", outputPath)
	}
	return args
}
