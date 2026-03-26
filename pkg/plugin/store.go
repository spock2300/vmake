package plugin

import (
	"fmt"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/internal/jsonio"
)

func LoadPluginInfo(pluginDir string) (*Info, error) {
	path := filepath.Join(pluginDir, "plugin.json")
	var info Info
	if err := jsonio.Load(path, &info); err != nil {
		return nil, fmt.Errorf("plugin info: %w", err)
	}
	return &info, nil
}

func SavePluginInfo(pluginDir string, info *Info) error {
	path := filepath.Join(pluginDir, "plugin.json")
	if err := jsonio.Save(path, info); err != nil {
		return fmt.Errorf("plugin info: %w", err)
	}
	return nil
}

func PluginInfoExists(pluginDir string) bool {
	return fs.FileExists(filepath.Join(pluginDir, "plugin.json"))
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
