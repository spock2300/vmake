package toolchain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/internal/fs"
)

type InstallConfig struct {
	Method string `json:"method"`
	File   string `json:"file"`
	URL    string `json:"url"`
	Sha256 string `json:"sha256"`
	Format string `json:"format"`
}

type ToolchainDef struct {
	Name         string         `json:"name"`
	Version      string         `json:"version"`
	DisplayName  string         `json:"display_name"`
	Host         string         `json:"host"`
	Prefix       string         `json:"prefix"`
	Tools        Tools          `json:"tools"`
	DefaultFlags DefaultFlags   `json:"default_flags"`
	Install      *InstallConfig `json:"install"`
}

func LoadToolchainDef(path string) (*ToolchainDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read toolchain def %s: %w", path, err)
	}
	var def ToolchainDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse toolchain def %s: %w", path, err)
	}
	if def.Name == "" {
		return nil, fmt.Errorf("toolchain def %s: missing name", path)
	}
	return &def, nil
}

func (d *ToolchainDef) InstallDir(toolchainsDir string) string {
	if d.Version != "" {
		return filepath.Join(toolchainsDir, d.Name+"-"+d.Version)
	}
	return filepath.Join(toolchainsDir, d.Name)
}

func (d *ToolchainDef) ToToolchain(toolchainsDir string) *Toolchain {
	installPath := ""
	if toolchainsDir != "" {
		candidate := d.InstallDir(toolchainsDir)
		if fs.FileExists(candidate) {
			installPath = candidate
		}
	}
	displayName := d.DisplayName
	if displayName == "" {
		displayName = d.Name
	}
	return &Toolchain{
		Name:         d.Name,
		DisplayName:  displayName,
		Host:         d.Host,
		Prefix:       d.Prefix,
		Tools:        d.Tools,
		DefaultFlags: d.DefaultFlags,
		InstallPath:  installPath,
	}
}

func ScanRepoToolchains(repoDir string) []ToolchainDef {
	var results []ToolchainDef

	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return results
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		defPath := filepath.Join(repoDir, entry.Name(), "toolchain.json")
		if !fs.FileExists(defPath) {
			continue
		}
		def, err := LoadToolchainDef(defPath)
		if err != nil {
			continue
		}
		results = append(results, *def)
	}

	return results
}

func DetectFormat(filename string) string {
	if strings.HasSuffix(filename, ".tar.gz") || strings.HasSuffix(filename, ".tgz") {
		return "tar.gz"
	}
	if strings.HasSuffix(filename, ".tar.xz") || strings.HasSuffix(filename, ".txz") {
		return "tar.xz"
	}
	if strings.HasSuffix(filename, ".tar.bz2") || strings.HasSuffix(filename, ".tbz2") {
		return "tar.bz2"
	}
	if strings.HasSuffix(filename, ".zip") {
		return "zip"
	}
	return "tar.gz"
}
