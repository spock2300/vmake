package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func LoadPluginInfo(pluginDir string) (*Info, error) {
	path := filepath.Join(pluginDir, "plugin.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin.json: %w", err)
	}

	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to parse plugin.json: %w", err)
	}

	return &info, nil
}

func SavePluginInfo(pluginDir string, info *Info) error {
	path := filepath.Join(pluginDir, "plugin.json")
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plugin.json: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write plugin.json: %w", err)
	}

	return nil
}

func PluginInfoExists(pluginDir string) bool {
	path := filepath.Join(pluginDir, "plugin.json")
	_, err := os.Stat(path)
	return err == nil
}

func CreateDefaultPluginInfo(pluginDir, name string) *Info {
	return &Info{
		Name:        name,
		Version:     "1.0.0",
		Description: "",
		Entry:       "src/main.go",
		Enabled:     true,
	}
}
