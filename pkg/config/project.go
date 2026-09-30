package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spock2300/vmake/internal/jsonio"
)

const (
	ProjectFilename = "project.json"
	DefaultFilename = "config.json"
)

type projectFile struct {
	Config string `json:"config"`
}

type ProjectConfigInfo struct {
	Name        string
	Active      bool
	Unsaved     bool
	Description string
	Error       error
}

const MaxDescriptionLength = 200

func ValidateDescription(text string) (string, error) {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n > MaxDescriptionLength {
		return "", fmt.Errorf("description too long: %d characters (maximum %d)", n, MaxDescriptionLength)
	}
	for _, c := range text {
		if c < 32 || c == 127 {
			return "", fmt.Errorf("description must be a single line without control characters")
		}
	}
	return text, nil
}

func ValidateFilename(name string) error {
	if name == ".json" || !strings.HasSuffix(name, ".json") ||
		strings.ContainsAny(name, `/\:<>"|?*`) || !filepath.IsLocal(name) ||
		strings.EqualFold(name, ProjectFilename) {
		return fmt.Errorf("invalid config filename %q: use a .json filename inside .vmake, excluding %s", name, ProjectFilename)
	}
	for _, c := range name {
		if c < 32 {
			return fmt.Errorf("invalid config filename %q: control characters are not allowed", name)
		}
	}
	return nil
}

func projectSelection(projectDir string) (projectFile, bool, error) {
	path := filepath.Join(projectDir, ".vmake", ProjectFilename)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return projectFile{Config: DefaultFilename}, false, nil
	}
	if err != nil {
		return projectFile{}, false, fmt.Errorf("read project selection %s: %w", path, err)
	}
	var project projectFile
	if err := json.Unmarshal(data, &project); err != nil {
		return projectFile{}, false, fmt.Errorf("parse project selection %s: %w", path, err)
	}
	if err := ValidateFilename(project.Config); err != nil {
		return projectFile{}, false, fmt.Errorf("project selection %s: %w", path, err)
	}
	return project, true, nil
}

func readProjectConfig(path string, allowMissing bool) (*ConfigFile, []byte, error) {
	data, err := os.ReadFile(path)
	if allowMissing && os.IsNotExist(err) {
		return newConfigFile(), nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg ConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, data, nil
}

func LoadProject(projectDir string) (*ConfigFile, string, error) {
	project, explicit, err := projectSelection(projectDir)
	if err != nil {
		return nil, "", err
	}
	path := filepath.Join(projectDir, ".vmake", project.Config)
	cfg, _, err := readProjectConfig(path, !explicit)
	if err != nil && explicit {
		return nil, path, fmt.Errorf("project selection %s: %w", filepath.Join(projectDir, ".vmake", ProjectFilename), err)
	}
	return cfg, path, err
}

func UseProjectConfig(projectDir, name string) (string, error) {
	if err := ValidateFilename(name); err != nil {
		return "", err
	}
	if _, _, err := projectSelection(projectDir); err != nil {
		return "", err
	}
	dir := filepath.Join(projectDir, ".vmake")
	if _, _, err := readProjectConfig(filepath.Join(dir, name), false); err != nil {
		return "", err
	}
	if match := matchingConfigName(dir, name); match != "" {
		name = match
	}
	path := filepath.Join(dir, ProjectFilename)
	if err := jsonio.Save(path, projectFile{Config: name}); err != nil {
		return "", fmt.Errorf("save project selection %s: %w", path, err)
	}
	return name, nil
}

func SetProjectDescription(projectDir, text string) (string, error) {
	text, err := ValidateDescription(text)
	if err != nil {
		return "", err
	}
	cfg, path, err := LoadProject(projectDir)
	if err != nil {
		return "", err
	}
	cfg.Description = text
	if err := Save(path, cfg); err != nil {
		return "", fmt.Errorf("save config %s: %w", path, err)
	}
	return path, nil
}

func CopyProjectConfig(projectDir, name string) error {
	if err := ValidateFilename(name); err != nil {
		return err
	}
	project, explicit, err := projectSelection(projectDir)
	if err != nil {
		return err
	}
	dir := filepath.Join(projectDir, ".vmake")
	cfg, data, err := readProjectConfig(filepath.Join(dir, project.Config), !explicit)
	if err != nil {
		return err
	}
	if data == nil {
		data, err = json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("create config %s: %w", path, err)
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if writeErr != nil {
			return fmt.Errorf("write config %s: %w", path, writeErr)
		}
		return fmt.Errorf("close config %s: %w", path, closeErr)
	}
	return nil
}

func ListProjectConfigs(projectDir string) ([]ProjectConfigInfo, error) {
	project, explicit, err := projectSelection(projectDir)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(projectDir, ".vmake")
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("list configs in %s: %w", dir, err)
	}
	var configs []ProjectConfigInfo
	activeName := ""
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") || strings.EqualFold(name, ProjectFilename) {
			continue
		}
		if name == project.Config {
			activeName = name
		}
		info := ProjectConfigInfo{Name: name}
		info.Error = ValidateFilename(name)
		if info.Error == nil {
			cfg, _, err := readProjectConfig(filepath.Join(dir, name), false)
			info.Error = err
			if err == nil {
				info.Description = cfg.Description
			}
		}
		configs = append(configs, info)
	}
	if activeName == "" {
		activeName = matchingConfigName(dir, project.Config)
	}
	if activeName != "" {
		for i := range configs {
			configs[i].Active = configs[i].Name == activeName
		}
	} else {
		_, _, readErr := readProjectConfig(filepath.Join(dir, project.Config), !explicit)
		configs = append(configs, ProjectConfigInfo{Name: project.Config, Active: true, Unsaved: !explicit && readErr == nil, Error: readErr})
	}
	sort.Slice(configs, func(i, j int) bool { return configs[i].Name < configs[j].Name })
	return configs, nil
}

func matchingConfigName(dir, name string) string {
	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil || info.IsDir() {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		entryName := entry.Name()
		if entry.IsDir() || entryName == name || !strings.EqualFold(entryName, name) || ValidateFilename(entryName) != nil {
			continue
		}
		entryInfo, err := os.Stat(filepath.Join(dir, entryName))
		if err == nil && os.SameFile(info, entryInfo) {
			return entryName
		}
	}
	return ""
}
