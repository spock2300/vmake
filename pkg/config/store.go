package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	Version string         `json:"version,omitempty"`
	Options map[string]any `json:"options,omitempty"`
}

func newConfigFile() *ConfigFile {
	return &ConfigFile{
		Version: ConfigVersion,
		Global:  &GlobalConfig{Options: make(map[string]any)},
		Entries: make(map[string]*EntryConfig),
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

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	cfg := newConfigFile()
	if v, ok := raw["version"].(string); ok {
		cfg.Version = v
	}

	if g, ok := raw["global"].(map[string]any); ok {
		cfg.Global = &GlobalConfig{Options: make(map[string]any)}
		if tc, ok := g["toolchain"].(string); ok {
			cfg.Global.Toolchain = tc
		}
		if mode, ok := g["mode"].(string); ok {
			cfg.Global.Mode = mode
		}
		if opts, ok := g["options"].(map[string]any); ok {
			cfg.Global.Options = opts
		}
	} else {
		if tc, ok := raw["toolchain"].(string); ok {
			cfg.Global.Toolchain = tc
		}
	}

	if entries, ok := raw["entries"].(map[string]any); ok {
		for name, e := range entries {
			if em, ok := e.(map[string]any); ok {
				entry := &EntryConfig{Options: make(map[string]any)}
				if ver, ok := em["version"].(string); ok {
					entry.Version = ver
				}
				if opts, ok := em["options"].(map[string]any); ok {
					entry.Options = opts
				}
				cfg.Entries[name] = entry
			}
		}
	}

	if cfg.Global == nil {
		cfg.Global = &GlobalConfig{Options: make(map[string]any)}
	}

	return cfg, nil
}

func Save(path string, cfg *ConfigFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func GetEntry(cfg *ConfigFile, name string) *EntryConfig {
	if cfg.Entries == nil {
		return &EntryConfig{Options: make(map[string]any)}
	}
	entry, ok := cfg.Entries[name]
	if !ok {
		return &EntryConfig{Options: make(map[string]any)}
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

func GetGlobalOption(cfg *ConfigFile, name string) any {
	if cfg.Global == nil || cfg.Global.Options == nil {
		return nil
	}
	return cfg.Global.Options[name]
}

func SetGlobalOption(cfg *ConfigFile, name string, value any) {
	if cfg.Global == nil {
		cfg.Global = &GlobalConfig{Options: make(map[string]any)}
	}
	if cfg.Global.Options == nil {
		cfg.Global.Options = make(map[string]any)
	}
	cfg.Global.Options[name] = value
}
