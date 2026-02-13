package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const ConfigVersion = "1"

type ConfigFile struct {
	Version  string                    `json:"version"`
	Packages map[string]*PackageConfig `json:"packages"`
}

type PackageConfig struct {
	Options map[string]any `json:"options"`
}

func newConfigFile() *ConfigFile {
	return &ConfigFile{
		Version:  ConfigVersion,
		Packages: make(map[string]*PackageConfig),
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

	var cfg ConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Packages == nil {
		cfg.Packages = make(map[string]*PackageConfig)
	}

	return &cfg, nil
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
