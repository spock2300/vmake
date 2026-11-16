package tui

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
)

func resolveInitialState(in Input) *config.Editor {
	ed := config.NewEditor(config.EditorInput{
		Options:          in.Options,
		Values:           in.Values,
		DerivedOptions:   in.DerivedOptions,
		CurrentToolchain: in.CurrentToolchain,
		GlobalOptions:    in.GlobalOptions,
		GlobalValues:     in.GlobalValues,
		ChipProfiles:     in.ChipProfiles,
		KConfigs:         in.KConfigs,
		Description:      in.Description,
		DisplayDefaults:  in.DisplayDefaults,
	})
	if ed.ChipProfiles == nil {
		ed.ChipProfiles = make(map[string]api.ChipProfile)
	}
	return ed
}
