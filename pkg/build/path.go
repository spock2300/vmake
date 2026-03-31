package build

import "path/filepath"

func BuildPath(baseDir, buildKey, subdir string) string {
	return filepath.Join(baseDir, "build", buildKey, subdir)
}
