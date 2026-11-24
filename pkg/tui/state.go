package tui

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
)

func resolveInitialState(wb *config.Workbench) *config.Editor {
	derived := wb.DerivedOptions
	if wb.InitialPreview.DerivedOptions != nil {
		derived = wb.InitialPreview.DerivedOptions
	}
	ed := config.NewEditor(config.EditorInput{
		Options:          wb.Options,
		Values:           wb.Values,
		DerivedOptions:   derived,
		CurrentToolchain: wb.CurrentToolchain,
		GlobalOptions:    wb.GlobalOptions,
		GlobalValues:     wb.GlobalValues,
		ChipProfiles:     wb.ChipProfiles,
		KConfigs:         wb.KConfigs,
		Description:      wb.Description,
		DisplayDefaults:  wb.DisplayDefaults,
	})
	if ed.ChipProfiles == nil {
		ed.ChipProfiles = make(map[string]api.ChipProfile)
	}
	return ed
}
