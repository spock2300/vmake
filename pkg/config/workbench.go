package config

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
)

// Preview is a draft configuration's effect on the project graph and the
// option values derived from it.
type Preview struct {
	Roots          []string
	ActivePackages map[string]bool
	ActiveDeps     map[string][]string
	DerivedOptions map[string]map[string]any
	DerivedOwners  map[string]map[string]string
}

// Workbench is everything the configuration TUI needs about a project: the
// resolved packages, the options and values to edit, and the previewer that
// evaluates a draft configuration.
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
