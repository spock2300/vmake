package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/pkg/api"
)

type NativeConfig struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

func LoadNativeConfig(dir string) (*NativeConfig, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, "repo.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var cfg NativeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, false, err
	}
	return &cfg, cfg.Type == "native", nil
}

func SaveNativeConfig(dir, urlTemplate string) error {
	if err := fs.EnsureDir(dir); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	cfg := NativeConfig{Type: "native", URL: urlTemplate}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "repo.json"), data, 0644)
}

func ResolveNativeURL(urlTemplate, pkgName string) string {
	return strings.ReplaceAll(urlTemplate, "{name}", pkgName)
}

func FilterValidVersions(tags []string) map[string]string {
	versions := make(map[string]string)
	for _, tag := range tags {
		v, ok := api.ParseVersion(tag)
		if !ok {
			continue
		}
		versions[v.String()] = tag
	}
	return versions
}

func SelectNativeVersion(versions map[string]string, constraint string) (string, string, error) {
	available := make([]string, 0, len(versions))
	for v := range versions {
		available = append(available, v)
	}
	selected, ok := api.MatchVersion(available, constraint)
	if !ok {
		return "", "", fmt.Errorf("no version matching '%s' in %v", constraint, available)
	}
	return selected, versions[selected], nil
}
