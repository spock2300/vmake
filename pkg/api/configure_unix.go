//go:build !windows

package api

import "path/filepath"

// configureCommand returns the command and leading arguments used to run an
// autotools configure script. Unix executes the script directly through its
// shebang.
func configureCommand(srcDir string) (string, []string) {
	return filepath.Join(srcDir, "configure"), nil
}
