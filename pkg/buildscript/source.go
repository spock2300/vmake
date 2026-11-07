package buildscript

import (
	"github.com/spock2300/vmake/pkg/api"
)

// SourceLink points a package at a user-managed working tree instead of a
// materialized remote checkout.
type SourceLink struct {
	// Path is the effective source root. For native packages it also supplies
	// build.go; for registry packages the wrapper build.go still comes from the
	// registry repository and Path is the upstream source tree.
	Path string
	// Commit is the git HEAD of Path.
	Commit string
	// Native reports that build.go itself is loaded from Path.
	Native bool
	// LocalSource reports that Path is the package source. A native subpackage
	// with its own SetGit source sets this to false: its build.go is linked but
	// its remote third-party source is still materialized.
	LocalSource bool
}

type Source struct {
	Path   string
	Name   string
	Dir    string
	Origin api.SourceOrigin
	// Repo is the remote repository name the script originates from
	// ("official", "mynative"). Empty for local project scripts.
	Repo string
	// URL identifies the remote script's source. Commit records the exact
	// revision being loaded; trust is bound to the repository identity instead.
	URL    string
	Commit string
	// Link is set when the package source is a local working tree.
	Link *SourceLink
	// Script binds local Go imports to this source's own project or checkout.
	Script ScriptOptions
}

func NewSource(name, path, dir string, origin api.SourceOrigin) *Source {
	return &Source{
		Name:   name,
		Path:   path,
		Dir:    dir,
		Origin: origin,
	}
}

func (s Source) IsLocal() bool  { return s.Origin == api.SourceLocal }
func (s Source) IsRemote() bool { return s.Origin == api.SourceRemote }
