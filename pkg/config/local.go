package config

import (
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/internal/jsonio"
)

const (
	LocalVersion  = "1"
	LocalFileName = "local.json"
)

// LocalConfig is machine-local project state. It is separate from ConfigFile
// and vmake.lock so source paths are never distributed.
type LocalConfig struct {
	Version string            `json:"version"`
	Config  string            `json:"config,omitempty"`
	Links   map[string]string `json:"links,omitempty"`
}

func newLocalConfig() *LocalConfig {
	return &LocalConfig{
		Version: LocalVersion,
		Links:   make(map[string]string),
	}
}

func LocalPath(projectDir string) string {
	return filepath.Join(projectDir, ".vmake", LocalFileName)
}

func LoadLocal(projectDir string) (*LocalConfig, error) {
	cfg := newLocalConfig()
	if err := jsonio.Load(LocalPath(projectDir), cfg); err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if cfg.Links == nil {
		cfg.Links = make(map[string]string)
	}
	return cfg, nil
}

func SaveLocal(projectDir string, cfg *LocalConfig) error {
	if cfg.Version == "" {
		cfg.Version = LocalVersion
	}
	if cfg.Links == nil {
		cfg.Links = make(map[string]string)
	}
	return jsonio.Save(LocalPath(projectDir), cfg)
}
