package buildscript

import "github.com/spock2300/vmake/internal/gosrc"

func ListGoFiles(dir string) ([]string, error) {
	return gosrc.ListGoFiles(dir)
}
