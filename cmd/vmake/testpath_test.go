package main

import (
	"path/filepath"
	"testing"
)

// canonicalTempDir returns a temp dir with symlinks resolved. On macOS
// t.TempDir() returns /var/... while vmake canonicalizes the same directory to
// /private/var/..., so path-comparing tests must start from the canonical form.
func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}
