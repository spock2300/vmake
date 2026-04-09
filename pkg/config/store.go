package config

import (
	"encoding/json"
	"os"

	"gitee.com/spock2300/vmake/internal/jsonio"
)

const ConfigVersion = "1"

type ConfigFile struct {
	Version string                  `json:"version"`
	Global  *GlobalConfig           `json:"global,omitempty"`
	Entries map[string]*EntryConfig `json:"entries"`
}

type GlobalConfig struct {
	Toolchain string         `json:"toolchain,omitempty"`
	Mode      string         `json:"mode,omitempty"`
	Options   map[string]any `json:"options,omitempty"`
}

type EntryConfig struct {
	Version        string         `json:"version,omitempty"`
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
		Version   string                     `json:"version"`
		Toolchain string                     `json:"toolchain"`
		Global    *GlobalConfig              `json:"global,omitempty"`
		Entries   map[string]json.RawMessage `json:"entries"`
	}

	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*cfg = *newConfigFile()
	cfg.Version = raw.Version

	if raw.Global != nil {
		cfg.Global = raw.Global
		if cfg.Global.Options == nil {
			cfg.Global.Options = make(map[string]any)
		}
	} else if raw.Toolchain != "" {
		cfg.Global.Toolchain = raw.Toolchain
	}

	for name, rawEntry := range raw.Entries {
		entry := &EntryConfig{Options: make(map[string]any)}
		if err := json.Unmarshal(rawEntry, entry); err == nil {
			cfg.Entries[name] = entry
		}
	}

	return nil
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
