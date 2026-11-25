package buildscript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spock2300/vmake/pkg/api"
)

const ManifestFileName = "vmake.json"

type ScriptSection struct {
	Module string `json:"module"`
	Root   string `json:"root,omitempty"`
}

type Manifest struct {
	Script  *ScriptSection    `json:"script,omitempty"`
	Roots   []string          `json:"roots,omitempty"`
	Modules map[string]string `json:"modules,omitempty"`
	Sources map[string]string `json:"sources,omitempty"`
}

func LoadManifest(rootDir string) (*Manifest, bool, error) {
	path := filepath.Join(rootDir, ManifestFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Manifest{}, false, nil
		}
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, true, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := m.Validate(path); err != nil {
		return nil, true, err
	}
	return &m, true, nil
}

func (m *Manifest) Validate(path string) error {
	if m == nil {
		return nil
	}
	seenRoots := make(map[string]bool, len(m.Roots))
	for _, root := range m.Roots {
		if err := api.ValidatePackageRef(root); err != nil {
			return fmt.Errorf("%s: roots entry %q: %w", path, root, err)
		}
		if !api.IsLocalPackageID(root) {
			return fmt.Errorf("%s: roots entry %q must be a local package starting with '//'", path, root)
		}
		if seenRoots[root] {
			return fmt.Errorf("%s: duplicate roots entry %q", path, root)
		}
		seenRoots[root] = true
	}
	for mod, raw := range m.Modules {
		if mod == api.LocalContainerID {
			return fmt.Errorf("%s: modules key %q must be an external container ID", path, mod)
		}
		if err := api.ValidateContainerID(mod); err != nil {
			return fmt.Errorf("%s: modules key %q: %w", path, mod, err)
		}
		if err := validateConstraint(raw); err != nil {
			return fmt.Errorf("%s: modules[%q]: %w", path, mod, err)
		}
	}
	for srcPkg, raw := range m.Sources {
		if err := api.ValidatePackageRef(srcPkg); err != nil {
			return fmt.Errorf("%s: sources key %q: %w", path, srcPkg, err)
		}
		if err := validateConstraint(raw); err != nil {
			return fmt.Errorf("%s: sources[%q]: %w", path, srcPkg, err)
		}
	}
	return nil
}

func validateConstraint(raw string) error {
	c := strings.TrimSpace(raw)
	if c == "" || c == "*" {
		return nil
	}
	if isHexCommit(c) {
		return nil
	}
	if _, ok := api.ParseConstraint(c); !ok {
		return fmt.Errorf("invalid version constraint %q", raw)
	}
	return nil
}

func isHexCommit(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func (m *Manifest) EffectiveRoots(rootDir string) ([]string, error) {
	if m != nil && len(m.Roots) > 0 {
		return slices.Clone(m.Roots), nil
	}
	if _, err := os.Stat(filepath.Join(rootDir, "build.go")); err == nil {
		return []string{api.LocalContainerID}, nil
	}
	return nil, fmt.Errorf("no roots declared in %s and no root build.go found in %s", ManifestFileName, rootDir)
}

func (m *Manifest) MatchModule(ref string) (containerID, relPath, constraint string, ok bool) {
	if m == nil || len(m.Modules) == 0 || ref == "" || api.IsLocalPackageID(ref) {
		return "", "", "", false
	}
	bestLen := -1
	for mod, c := range m.Modules {
		if ref == mod {
			if len(mod) > bestLen {
				bestLen = len(mod)
				containerID, relPath, constraint, ok = mod, "", strings.TrimSpace(c), true
			}
			continue
		}
		if strings.HasPrefix(ref, mod+"/") && len(mod) > bestLen {
			bestLen = len(mod)
			containerID, relPath, constraint, ok = mod, strings.TrimPrefix(ref, mod+"/"), strings.TrimSpace(c), true
		}
	}
	return containerID, relPath, constraint, ok
}

func LoadPackageSource(rootDir, containerID, relPath string, origin api.SourceOrigin) (*Source, error) {
	relPath = strings.Trim(filepath.ToSlash(relPath), "/")
	dir := rootDir
	if relPath != "" {
		dir = filepath.Join(rootDir, filepath.FromSlash(relPath))
	}
	buildGo := filepath.Join(dir, "build.go")
	if _, err := os.Stat(buildGo); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("build.go not found in %s", dir)
		}
		return nil, fmt.Errorf("stat %s: %w", buildGo, err)
	}
	id := containerID
	if containerID == api.LocalContainerID {
		if relPath != "" {
			id = api.LocalContainerID + relPath
		}
	} else if relPath != "" {
		id = containerID + "/" + relPath
	}
	opts, err := DetectScriptOptions(dir, rootDir)
	if err != nil {
		return nil, err
	}
	src := NewSource(id, buildGo, dir, origin)
	src.Container = containerID
	src.RelPath = relPath
	src.Script = opts
	return src, nil
}
