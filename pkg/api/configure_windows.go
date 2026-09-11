//go:build windows

package api

import "path/filepath"

// configureCommand returns the command and leading arguments used to run an
// autotools configure script. Windows cannot execute a '#!/bin/sh' script
// directly, so it is handed to sh (provided by Git for Windows). The path is
// slash-separated because the MSYS runtime de-quotes backslashes in argv, and
// autoconf derives srcdir from $0 with dirname, which only understands '/'.
func configureCommand(srcDir string) (string, []string) {
	return "sh", []string{filepath.ToSlash(filepath.Join(srcDir, "configure"))}
}
