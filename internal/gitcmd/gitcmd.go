// Package gitcmd builds git argument lists carrying the configuration vmake
// requires so that cached checkouts are byte-identical to their repositories
// on every platform.
package gitcmd

import (
	"github.com/spock2300/vmake/internal/fs"
)

// Args prepends vmake's git configuration to args.
//
//   - core.autocrlf=false and core.eol=lf keep checkouts byte-identical to the
//     repository. The Git for Windows installer defaults autocrlf to true,
//     which rewrites line endings on checkout and would break content hashes
//     (pkg/build/stamp.go) and multi-patch series applied with 'git apply'.
//   - core.longpaths=true allows the deep ~/.vmake/cache/<repo>/<pkg>/...
//     paths on Windows.
//   - core.symlinks=true is passed only when this process can create symbolic
//     links; otherwise git falls back to checking symlinks out as plain files.
func Args(args ...string) []string {
	out := make([]string, 0, len(args)+8)
	out = append(out,
		"-c", "core.autocrlf=false",
		"-c", "core.eol=lf",
		"-c", "core.longpaths=true",
	)
	if fs.SymlinksSupported() {
		out = append(out, "-c", "core.symlinks=true")
	}
	return append(out, args...)
}
