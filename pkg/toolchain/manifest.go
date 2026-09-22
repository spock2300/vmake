package toolchain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
)

type InstallConfig struct {
	Method  string `json:"method"`
	File    string `json:"file"`
	URL     string `json:"url"`
	Sha256  string `json:"sha256"`
	Format  string `json:"format"`
	RootDir string `json:"root_dir"`
}

type ToolchainDef struct {
	Name          string                   `json:"name"`
	Version       string                   `json:"version"`
	DisplayName   string                   `json:"display_name"`
	Prefix        string                   `json:"prefix"`
	Tools         Tools                    `json:"tools"`
	Installations map[string]InstallConfig `json:"installations"`
}

type DefinitionError struct {
	name string
	path string
	err  error
}

func (e *DefinitionError) Name() string  { return e.name }
func (e *DefinitionError) Path() string  { return e.path }
func (e *DefinitionError) Unwrap() error { return e.err }

func (e *DefinitionError) Error() string {
	if e.name != "" {
		return fmt.Sprintf("toolchain %q definition %s: %v", e.name, e.path, e.err)
	}
	return fmt.Sprintf("toolchain definition %s: %v", e.path, e.err)
}

func LoadToolchainDef(path string) (*ToolchainDef, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("toolchain definition path: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &DefinitionError{path: path, err: err}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, &DefinitionError{path: path, err: err}
	}
	var name string
	if err := json.Unmarshal(fields["name"], &name); err != nil || !validPathComponent(name) {
		name = ""
	}
	for _, field := range []string{"host", "install"} {
		if _, ok := fields[field]; ok {
			return nil, &DefinitionError{name: name, path: path, err: fmt.Errorf("legacy field %q is unsupported; use installations keyed by host OS/architecture", field)}
		}
	}
	for _, field := range []string{"target_os", "target_triple", "default_flags"} {
		if _, ok := fields[field]; ok {
			return nil, &DefinitionError{name: name, path: path, err: fmt.Errorf("field %q belongs in build.go project configuration, not toolchain.json", field)}
		}
	}
	var def ToolchainDef
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&def); err != nil {
		return nil, &DefinitionError{name: name, path: path, err: err}
	}
	if err := def.Validate(); err != nil {
		return nil, &DefinitionError{name: name, path: path, err: err}
	}
	return &def, nil
}

func (d *ToolchainDef) InstallDir(toolchainsDir string) string {
	return filepath.Join(toolchainsDir, runtime.GOOS, runtime.GOARCH, d.Name, d.Version)
}

func (d *ToolchainDef) Installation() (*InstallConfig, error) {
	return d.installationFor(runtime.GOOS, runtime.GOARCH)
}

func (d *ToolchainDef) installationFor(hostOS, hostArch string) (*InstallConfig, error) {
	if len(d.Installations) == 0 {
		return nil, nil
	}
	host := hostOS + "/" + hostArch
	install, ok := d.Installations[host]
	if !ok {
		return nil, fmt.Errorf("toolchain %s: no installation for host %s", d.Name, host)
	}
	return &install, nil
}

func (d *ToolchainDef) Validate() error {
	if !validPathComponent(d.Name) {
		return fmt.Errorf("missing or invalid name %q", d.Name)
	}
	if d.Prefix != "" && !strings.HasSuffix(d.Prefix, "-") {
		return fmt.Errorf("toolchain %s: prefix must include its trailing hyphen", d.Name)
	}
	if len(d.Installations) > 0 && !validPathComponent(d.Version) {
		return fmt.Errorf("toolchain %s: installations require a valid version", d.Name)
	}
	for host, install := range d.Installations {
		parts := strings.Split(host, "/")
		if len(parts) != 2 || !validPathComponent(parts[0]) || !validPathComponent(parts[1]) {
			return fmt.Errorf("toolchain %s: invalid installation host %q; expected OS/architecture", d.Name, host)
		}
		if install.RootDir != "." && !validPathComponent(install.RootDir) {
			return fmt.Errorf("toolchain %s installation %s: missing or invalid root_dir %q", d.Name, host, install.RootDir)
		}
		if !validPathComponent(install.File) {
			return fmt.Errorf("toolchain %s installation %s: missing or invalid archive file %q", d.Name, host, install.File)
		}
		if install.Method == "lfs" && (strings.TrimSpace(install.File) != install.File || strings.ContainsAny(install.File, ",*?[]")) {
			return fmt.Errorf("toolchain %s installation %s: invalid LFS archive file %q; use a literal filename without pattern characters, commas, or surrounding whitespace", d.Name, host, install.File)
		}
		if install.Method != "lfs" && install.Method != "http" {
			return fmt.Errorf("toolchain %s installation %s: unknown method %q", d.Name, host, install.Method)
		}
		if install.Method == "http" && install.URL == "" {
			return fmt.Errorf("toolchain %s installation %s: missing URL", d.Name, host)
		}
	}
	return nil
}

func validPathComponent(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/\\:")
}

func (d *ToolchainDef) ToToolchain(toolchainsDir string) (*Toolchain, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	install, err := d.Installation()
	if err != nil {
		return nil, err
	}
	installPath := ""
	if toolchainsDir != "" && install != nil {
		candidate := d.InstallDir(toolchainsDir)
		info, err := os.Stat(candidate)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("toolchain %s: installation %s is not a directory", d.Name, candidate)
			}
			installPath = candidate
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("toolchain %s: inspect installation %s: %w", d.Name, candidate, err)
		}
	}
	displayName := d.DisplayName
	if displayName == "" {
		displayName = d.Name
	}
	return &Toolchain{
		Name:        d.Name,
		DisplayName: displayName,
		Prefix:      d.Prefix,
		Tools:       d.Tools,
		InstallPath: installPath,
	}, nil
}

func ScanRepoToolchains(repoDir string) ([]ToolchainDef, error) {
	var results []ToolchainDef
	var failures []error

	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return nil, fmt.Errorf("scan toolchains in %s: %w", repoDir, err)
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
			failures = append(failures, err)
			continue
		}
		results = append(results, *def)
	}

	return results, errors.Join(failures...)
}
