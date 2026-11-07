package tui

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
)

func testModel(
	packages []buildscript.Source,
	deps map[string][]string,
	options map[string]map[string]*api.Option,
	values map[string]map[string]any,
	workDir string,
	currentToolchain string,
	globalOptions map[string]*api.Option,
	globalValues map[string]any,
	kconfigs map[string][]*api.KConfigEntry,
) Model {
	return NewModel(Input{
		Packages:         packages,
		Deps:             deps,
		Options:          options,
		Values:           values,
		WorkDir:          workDir,
		CurrentToolchain: currentToolchain,
		GlobalOptions:    globalOptions,
		GlobalValues:     globalValues,
		KConfigs:         kconfigs,
	})
}
