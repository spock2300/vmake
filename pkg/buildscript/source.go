package buildscript

import (
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

type Source struct {
	Path      string
	Name      string
	Dir       string
	OutputDir string
	Origin    api.SourceOrigin
	Force     bool
}

func NewSource(name, path, dir, outputDir string, origin api.SourceOrigin, force bool) *Source {
	return &Source{
		Name:      name,
		Path:      path,
		Dir:       dir,
		OutputDir: outputDir,
		Origin:    origin,
		Force:     force,
	}
}

func (s Source) IsLocal() bool  { return s.Origin == api.SourceLocal }
func (s Source) IsRemote() bool { return s.Origin == api.SourceRemote }

func (s Source) GetOutputDir() string {
	if s.OutputDir != "" {
		return s.OutputDir
	}
	return filepath.Join(s.Dir, "build")
}
