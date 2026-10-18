package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitstore"
	"github.com/spock2300/vmake/internal/jsonio"
)

const APIVersion = 1

func LoadPluginInfo(pluginDir string) (*Info, error) {
	path := filepath.Join(pluginDir, "plugin.json")
	var info Info
	if err := jsonio.Load(path, &info); err != nil {
		return nil, fmt.Errorf("plugin info: %w", err)
	}
	if err := info.Validate(pluginDir); err != nil {
		return nil, err
	}
	return &info, nil
}

func PluginInfoExists(pluginDir string) bool {
	return fs.FileExists(filepath.Join(pluginDir, "plugin.json"))
}

func (i *Info) Validate(pluginDir string) error {
	if err := validatePluginName(i.Name); err != nil {
		return fmt.Errorf("plugin %s: %w", pluginDir, err)
	}
	if i.APIVersion == 0 {
		i.APIVersion = APIVersion
	}
	if i.APIVersion != APIVersion {
		return fmt.Errorf("plugin %s: unsupported apiVersion %d (want %d)", i.Name, i.APIVersion, APIVersion)
	}
	if _, err := i.SourceDirectory(pluginDir); err != nil {
		return fmt.Errorf("plugin %s: %w", i.Name, err)
	}
	return nil
}

func validatePluginName(name string) error {
	if err := gitstore.ValidateName(name); err != nil {
		return fmt.Errorf("invalid plugin name %q", name)
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid plugin name %q", name)
	}
	return nil
}

// SourceDirectory returns the directory whose Go files form the plugin. The
// directory must stay inside pluginDir; entry/sourceDir are not allowed to
// escape the repository via ".." or an absolute path.
func (i *Info) SourceDirectory(pluginDir string) (string, error) {
	value := i.SourceDir
	if value == "" {
		value = i.Entry
	}
	if value == "" {
		return "", fmt.Errorf("missing entry or sourceDir")
	}
	if !filepath.IsLocal(value) {
		return "", fmt.Errorf("entry/sourceDir %q escapes the plugin directory", value)
	}
	joined := filepath.Join(pluginDir, filepath.Clean(value))
	dir := joined
	if i.SourceDir == "" {
		dir = filepath.Dir(joined)
	}
	rel, err := filepath.Rel(pluginDir, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("entry/sourceDir %q escapes the plugin directory", value)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("plugin source directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("plugin source path %s is not a directory", dir)
	}
	resolvedRoot, err := filepath.EvalSymlinks(pluginDir)
	if err != nil {
		return "", fmt.Errorf("resolve plugin directory %s: %w", pluginDir, err)
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve plugin source directory %s: %w", dir, err)
	}
	rel, err = filepath.Rel(resolvedRoot, resolvedDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("entry/sourceDir %q escapes the plugin directory through a symlink", value)
	}
	return dir, nil
}
