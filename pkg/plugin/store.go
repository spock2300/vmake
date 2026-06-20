package plugin

import (
	"fmt"
	"path/filepath"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/jsonio"
)

func LoadPluginInfo(pluginDir string) (*Info, error) {
	path := filepath.Join(pluginDir, "plugin.json")
	var info Info
	if err := jsonio.Load(path, &info); err != nil {
		return nil, fmt.Errorf("plugin info: %w", err)
	}
	return &info, nil
}

func PluginInfoExists(pluginDir string) bool {
	return fs.FileExists(filepath.Join(pluginDir, "plugin.json"))
}
