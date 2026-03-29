package buildscript

import (
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/api"
)

type Source struct {
	Path      string
	Name      string
	Dir       string
	OutputDir string
	Origin    api.SourceOrigin
	Force     bool
}

func (s Source) GetOutputDir() string {
	if s.OutputDir != "" {
		return s.OutputDir
	}
	return filepath.Join(s.Dir, "build")
}
