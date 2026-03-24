package buildscript

import "gitee.com/spock2300/vmake/pkg/api"

type Source struct {
	Path      string
	Name      string
	Dir       string
	OutputDir string
	Origin    api.SourceOrigin
	Force     bool
}
