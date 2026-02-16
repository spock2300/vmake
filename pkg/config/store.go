package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const ConfigVersion = "1"

type ConfigFile struct {
	Version  string                    `json:"version"`
	Global   *GlobalConfig             `json:"global,omitempty"`
	Packages map[string]*PackageConfig `json:"packages"`
	Requires map[string]*RequireConfig `json:"requires,omitempty"`
}

type GlobalConfig struct {
	Toolchain string         `json:"toolchain,omitempty"`
	Mode      string         `json:"mode,omitempty"`
	Options   map[string]any `json:"options,omitempty"`
}

type PackageConfig struct {
	Options map[string]any `json:"options"`
}

type RequireConfig struct {
	Version string         `json:"version"`
	Options map[string]any `json:"options,omitempty"`
}

func newConfigFile() *ConfigFile {
	return &ConfigFile{
		Version:  ConfigVersion,
		Global:   &GlobalConfig{Options: make(map[string]any)},
		Packages: make(map[string]*PackageConfig),
		Requires: make(map[string]*RequireConfig),
	}
}

func newPackageConfig() *PackageConfig {
	return &PackageConfig{
		Options: make(map[string]any),
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

	if pkgs, ok := raw["packages"].(map[string]any); ok {
		for name, p := range pkgs {
			if pm, ok := p.(map[string]any); ok {
				pc := &PackageConfig{Options: make(map[string]any)}
				if opts, ok := pm["options"].(map[string]any); ok {
					pc.Options = opts
				}
				cfg.Packages[name] = pc
			}
		}
	}

	if reqs, ok := raw["requires"].(map[string]any); ok {
		for name, r := range reqs {
			if rm, ok := r.(map[string]any); ok {
				rc := &RequireConfig{Options: make(map[string]any)}
				if ver, ok := rm["version"].(string); ok {
					rc.Version = ver
				}
				if opts, ok := rm["options"].(map[string]any); ok {
					rc.Options = opts
				}
				cfg.Requires[name] = rc
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

func GetPackageConfig(cfg *ConfigFile, pkgName string) *PackageConfig {
	if cfg.Packages == nil {
		return newPackageConfig()
	}

	pc, ok := cfg.Packages[pkgName]
	if !ok {
		return newPackageConfig()
	}

	if pc.Options == nil {
		pc.Options = make(map[string]any)
	}

	return pc
}

func GetOptionValue(pc *PackageConfig, name string) any {
	if pc == nil || pc.Options == nil {
		return nil
	}
	return pc.Options[name]
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

func GetRequireConfig(cfg *ConfigFile, pkgName string) *RequireConfig {
	if cfg.Requires == nil {
		return &RequireConfig{Options: make(map[string]any)}
	}

	rc, ok := cfg.Requires[pkgName]
	if !ok {
		return &RequireConfig{Options: make(map[string]any)}
	}

	if rc.Options == nil {
		rc.Options = make(map[string]any)
	}

	return rc
}

func SetRequireConfig(cfg *ConfigFile, pkgName string, rc *RequireConfig) {
	if cfg.Requires == nil {
		cfg.Requires = make(map[string]*RequireConfig)
	}
	cfg.Requires[pkgName] = rc
}
