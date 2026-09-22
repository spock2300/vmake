//go:build !windows

package fs

import "os"

// SymlinkHint describes what to do when symbolic links are unavailable.
const SymlinkHint = "symbolic links should always work on this platform"

func probeSymlinks() bool { return true }

func symlinkError(err error) error { return err }

func symlinkKindMatches(link, target os.FileInfo) bool { return true }
