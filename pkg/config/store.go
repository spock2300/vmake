package config

import (
	"encoding/json"
	"os"

	"github.com/spock2300/vmake/internal/jsonio"
)

const ConfigVersion = "1"

type ConfigFile struct {
	Version     string                  `json:"version"`
	Description string                  `json:"description,omitempty"`
	Root        string                  `json:"root,omitempty"`
	Global      *GlobalConfig           `json:"global,omitempty"`
	Entries     map[string]*EntryConfig `json:"entries"`
}

type GlobalConfig struct {
	Toolchain string         `json:"toolchain,omitempty"`
	Mode      string         `json:"mode,omitempty"`
	Options   map[string]any `json:"options,omitempty"`
}

type EntryConfig struct {
	Options        map[string]any `json:"options,omitempty"`
	KConfig        string         `json:"kconfig,omitempty"`
	SelectedPreset string         `json:"selected_preset,omitempty"`
}

func newConfigFile() *ConfigFile {
	return &ConfigFile{
		Version: ConfigVersion,
		Global:  &GlobalConfig{Options: make(map[string]any)},
		Entries: make(map[string]*EntryConfig),
	}
}

func (cfg *ConfigFile) UnmarshalJSON(data []byte) error {
	type rawConfig struct {
		Version     string                     `json:"version"`
		Description string                     `json:"description"`
		Root        string                     `json:"root"`
		Global      *GlobalConfig              `json:"global,omitempty"`
		Entries     map[string]json.RawMessage `json:"entries"`
	}

	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*cfg = *newConfigFile()
	cfg.Version = raw.Version
	cfg.Description = raw.Description
	cfg.Root = raw.Root

	if raw.Global != nil {
		cfg.Global = raw.Global
		if cfg.Global.Options == nil {
			cfg.Global.Options = make(map[string]any)
		}
	}

	for name, rawEntry := range raw.Entries {
		entry := &EntryConfig{Options: make(map[string]any)}
		if err := json.Unmarshal(rawEntry, entry); err == nil {
			cfg.Entries[name] = entry
		}
	}

	return nil
}

func PruneEmptyEntries(cfg *ConfigFile) {
	if cfg == nil {
		return
	}
	for name, entry := range cfg.Entries {
		if entry == nil || (len(entry.Options) == 0 && entry.KConfig == "" && entry.SelectedPreset == "") {
			delete(cfg.Entries, name)
		}
	}
	if cfg.Entries == nil {
		cfg.Entries = make(map[string]*EntryConfig)
	}
	if cfg.Global != nil && cfg.Global.Toolchain == "" && cfg.Global.Mode == "" && len(cfg.Global.Options) == 0 {
		cfg.Global = nil
	}
}

func Load(path string) (*ConfigFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newConfigFile(), nil
		}
		return nil, err
	}

	var cfg ConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func Save(path string, cfg *ConfigFile) error {
	return jsonio.Save(path, cfg)
}

func GetEntry(cfg *ConfigFile, name string) *EntryConfig {
	if cfg.Entries == nil {
		return newEntryConfig()
	}
	entry, ok := cfg.Entries[name]
	if !ok {
		return newEntryConfig()
	}
	if entry.Options == nil {
		entry.Options = make(map[string]any)
	}
	return entry
}

func SetEntry(cfg *ConfigFile, name string, entry *EntryConfig) {
	if cfg.Entries == nil {
		cfg.Entries = make(map[string]*EntryConfig)
	}
	cfg.Entries[name] = entry
}

func BuildGlobalValues(cfg *ConfigFile) map[string]any {
	vals := make(map[string]any)
	if cfg.Global != nil {
		if cfg.Global.Toolchain != "" {
			vals["toolchain"] = cfg.Global.Toolchain
		}
		if cfg.Global.Mode != "" {
			vals["mode"] = cfg.Global.Mode
		}
		for k, v := range cfg.Global.Options {
			vals[k] = v
		}
	}
	return vals
}

func newEntryConfig() *EntryConfig {
	return &EntryConfig{Options: make(map[string]any)}
}
