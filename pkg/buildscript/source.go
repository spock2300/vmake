package buildscript

import (
	"github.com/spock2300/vmake/pkg/api"
)

type Source struct {
	Path   string
	Name   string
	Dir    string
	Origin api.SourceOrigin
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
