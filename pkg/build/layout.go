package build

import "github.com/spock2300/vmake/pkg/api"

// PackageLayout is the single source of truth for a package's build and
// install directories and its stable build key. Ready is false when the
// layout cannot be materialized yet, for example for a local SetGit package
// whose managed source tree has not been created.
type PackageLayout struct {
	Dirs     api.PkgDirs
	BuildKey string
	Ready    bool
}
