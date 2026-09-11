//go:build !windows

// Package gitusr locates the Unix userland bundled with Git for Windows and
// makes it resolvable by name. On other platforms the userland is already on
// PATH, so this package does nothing.
package gitusr

// SetupProcessEnv is a no-op outside Windows.
func SetupProcessEnv() []string { return nil }

// UsrBin reports no bundled userland outside Windows.
func UsrBin() (string, bool) { return "", false }
