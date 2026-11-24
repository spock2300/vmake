package config

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
)

type Preview struct {
	Roots          []string
	ActivePackages map[string]bool
	ActiveDeps     map[string][]string
	DerivedOptions map[string]map[string]any
	DerivedOwners  map[string]map[string]string
}

type Workbench struct {
	Packages         []buildscript.Source
	Deps             map[string][]string
	Options          map[string]map[string]*api.Option
	Values           map[string]map[string]any
	DerivedOptions   map[string]map[string]any
	FocusActive      bool
	InitialPreview   Preview
	BaseConfig       *ConfigFile
	Preview          func(cfg *ConfigFile) (Preview, bool)
	CurrentToolchain string
	GlobalOptions    map[string]*api.Option
	GlobalValues     map[string]any
	ChipProfiles     map[string]api.ChipProfile
	KConfigs         map[string][]*api.KConfigEntry
	Description      string
	DisplayDefaults  func(toolchain string) map[string]any
}
