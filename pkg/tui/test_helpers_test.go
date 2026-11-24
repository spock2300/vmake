package tui

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
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
		WorkDir: workDir,
		Workbench: &config.Workbench{
			Packages:         packages,
			Deps:             deps,
			Options:          options,
			Values:           values,
			CurrentToolchain: currentToolchain,
			GlobalOptions:    globalOptions,
			GlobalValues:     globalValues,
			KConfigs:         kconfigs,
		},
	})
}

func overlayViewForTest(m *Model) string {
	switch m.overlay {
	case overlayChoice, overlayLanguage:
		layout, _ := m.choiceDialogLayout()
		return layout.view
	case overlayDetail:
		layout, _, _, _ := m.detailDialogLayout()
		return layout.view
	}
	return ""
}

func detailOverlayViewForTest(m *Model) string {
	layout, _, _, _ := m.detailDialogLayout()
	return layout.view
}
