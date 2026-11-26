package buildscript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spock2300/vmake/internal/gitstore"
	"github.com/spock2300/vmake/pkg/api"
)

const ManifestFileName = "vmake.json"

type ScriptSection struct {
	Module string `json:"module"`
	Root   string `json:"root,omitempty"`
}

type RepoSpec struct {
	Native string `json:"native,omitempty"`
}

type ModuleSpec struct {
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
}

func (m *ModuleSpec) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		m.Version = s
		m.Path = ""
		return nil
	}
	type alias ModuleSpec
	var obj alias
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("module spec must be a version constraint string or {\"version\"|\"path\"} object: %w", err)
	}
	*m = ModuleSpec(obj)
	return nil
}

func (m ModuleSpec) MarshalJSON() ([]byte, error) {
	if m.Path == "" {
		return json.Marshal(m.Version)
	}
	type alias ModuleSpec
	return json.Marshal(alias(m))
}

type Manifest struct {
	Script        *ScriptSection        `json:"script,omitempty"`
	Roots         []string              `json:"roots,omitempty"`
	DevRoots      []string              `json:"dev_roots,omitempty"`
	Repos         map[string]RepoSpec   `json:"repos,omitempty"`
	Modules       map[string]ModuleSpec `json:"modules,omitempty"`
	Exports  map[string]string     `json:"exports,omitempty"`
	Sources  map[string]string     `json:"sources,omitempty"`
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
	if err := validateLocalRootList(path, "roots", m.Roots); err != nil {
		return err
	}
	if err := validateLocalRootList(path, "dev_roots", m.DevRoots); err != nil {
		return err
	}
	for repoName, spec := range m.Repos {
		if err := gitstore.ValidateName(repoName); err != nil {
			return fmt.Errorf("%s: repos key %q: %w", path, repoName, err)
		}
		tmpl := strings.TrimSpace(spec.Native)
		if tmpl == "" {
			return fmt.Errorf("%s: repos[%q] must specify a non-empty \"native\" URL template", path, repoName)
		}
		if !strings.Contains(tmpl, "{name}") {
			return fmt.Errorf("%s: repos[%q].native %q must contain the {name} placeholder", path, repoName, spec.Native)
		}
	}
	for mod, spec := range m.Modules {
		if mod == api.LocalContainerID {
			return fmt.Errorf("%s: modules key %q must be an external container ID", path, mod)
		}
		if err := api.ValidateContainerID(mod); err != nil {
			return fmt.Errorf("%s: modules key %q: %w", path, mod, err)
		}
		if err := validateModuleSpec(spec); err != nil {
			return fmt.Errorf("%s: modules[%q]: %w", path, mod, err)
		}
	}
	if err := validateExports(path, m.Exports); err != nil {
		return err
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

func validateModuleSpec(spec ModuleSpec) error {
	p := strings.TrimSpace(spec.Path)
	v := strings.TrimSpace(spec.Version)
	if p != "" {
		if v != "" {
			return fmt.Errorf("cannot specify both \"path\" and \"version\"")
		}
		if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
			return fmt.Errorf("path %q must be relative to the project root", spec.Path)
		}
		return nil
	}
	return validateConstraint(v)
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

func validateLocalRootList(path, field string, roots []string) error {
	seen := make(map[string]bool, len(roots))
	for _, root := range roots {
		if err := api.ValidatePackageRef(root); err != nil {
			return fmt.Errorf("%s: %s entry %q: %w", path, field, root, err)
		}
		if !api.IsLocalPackageID(root) {
			return fmt.Errorf("%s: %s entry %q must be a local package starting with '//'", path, field, root)
		}
		if seen[root] {
			return fmt.Errorf("%s: duplicate %s entry %q", path, field, root)
		}
		seen[root] = true
	}
	return nil
}

func validateExportKey(key string) error {
	if key == "." {
		return nil
	}
	if key == "" || strings.HasPrefix(key, "/") || strings.HasSuffix(key, "/") || strings.Contains(key, ":") || strings.Contains(key, "\\") || strings.Contains(key, "//") {
		return fmt.Errorf("invalid export name %q (use '.' or a clean relative path without leading/trailing '/')", key)
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("invalid export name %q (path segments must not be empty, '.' or '..')", key)
		}
	}
	return nil
}

func validateExports(path string, exports map[string]string) error {
	seenTargets := make(map[string]string, len(exports))
	for key, target := range exports {
		if err := validateExportKey(key); err != nil {
			return fmt.Errorf("%s: exports[%q]: %w", path, key, err)
		}
		if err := api.ValidatePackageRef(target); err != nil {
			return fmt.Errorf("%s: exports[%q] target %q: %w", path, key, target, err)
		}
		if !api.IsLocalPackageID(target) {
			return fmt.Errorf("%s: exports[%q] target %q must be a local package starting with '//'", path, key, target)
		}
		if prev, dup := seenTargets[target]; dup {
			return fmt.Errorf("%s: exports[%q] and exports[%q] both target %q; export targets must be unique", path, prev, key, target)
		}
		seenTargets[target] = key
	}
	return nil
}

func (m *Manifest) HasExports() bool {
	return m != nil && m.Exports != nil
}

func (m *Manifest) ExportNames() []string {
	if !m.HasExports() {
		return nil
	}
	names := make([]string, 0, len(m.Exports))
	for k := range m.Exports {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}

func (m *Manifest) ResolveExport(reqRelPath string) (string, bool) {
	reqRelPath = strings.Trim(filepath.ToSlash(reqRelPath), "/")
	if !m.HasExports() {
		return reqRelPath, true
	}
	key := reqRelPath
	if key == "" {
		key = "."
	}
	target, ok := m.Exports[key]
	if !ok {
		return "", false
	}
	rel, _ := api.LocalRelPath(target)
	return rel, true
}

func (m *Manifest) ExportKeyForRelPath(relPath string) (string, bool) {
	if !m.HasExports() {
		return "", false
	}
	relPath = strings.Trim(filepath.ToSlash(relPath), "/")
	want := api.LocalContainerID
	if relPath != "" {
		want = api.LocalContainerID + relPath
	}
	for k, target := range m.Exports {
		if target == want {
			if k == "." {
				return "", true
			}
			return k, true
		}
	}
	return "", false
}

func (m *Manifest) IsExportedRelPath(relPath string) bool {
	if !m.HasExports() {
		return true
	}
	_, ok := m.ExportKeyForRelPath(relPath)
	return ok
}

func (m *Manifest) ExportKeyCollision(relPath string) (string, string, bool) {
	if !m.HasExports() {
		return "", "", false
	}
	if _, exported := m.ExportKeyForRelPath(relPath); exported {
		return "", "", false
	}
	relPath = strings.Trim(filepath.ToSlash(relPath), "/")
	key := relPath
	if key == "" {
		key = "."
	}
	if target, ok := m.Exports[key]; ok {
		return key, target, true
	}
	return "", "", false
}

func (m *Manifest) CanonicalPackageID(containerID, relPath string) string {
	relPath = strings.Trim(filepath.ToSlash(relPath), "/")
	if containerID == "" || containerID == api.LocalContainerID {
		if relPath == "" {
			return api.LocalContainerID
		}
		return api.LocalContainerID + relPath
	}
	if m.HasExports() {
		if expKey, ok := m.ExportKeyForRelPath(relPath); ok {
			if expKey == "" {
				return containerID
			}
			return containerID + "/" + expKey
		}
	}
	if relPath == "" {
		return containerID
	}
	return containerID + "/" + relPath
}

func (m *Manifest) InternalRefMap(containerID string) map[string]string {
	if !m.HasExports() || containerID == "" || containerID == api.LocalContainerID {
		return nil
	}
	out := make(map[string]string, len(m.Exports))
	for k, target := range m.Exports {
		if k == "." {
			out[target] = containerID
		} else {
			out[target] = containerID + "/" + k
		}
	}
	return out
}

func (m *Manifest) EffectiveRoots(rootDir string) ([]string, error) {
	if m != nil && len(m.Roots) > 0 {
		return slices.Clone(m.Roots), nil
	}
	if m != nil && len(m.DevRoots) > 0 {
		return slices.Clone(m.DevRoots), nil
	}
	if _, err := os.Stat(filepath.Join(rootDir, "build.go")); err == nil {
		return []string{api.LocalContainerID}, nil
	}
	return nil, fmt.Errorf("no roots or dev_roots declared in %s and no root build.go found in %s", ManifestFileName, rootDir)
}

func (m *Manifest) MatchModule(ref string) (containerID, relPath string, spec ModuleSpec, ok bool) {
	if m == nil || len(m.Modules) == 0 || ref == "" || api.IsLocalPackageID(ref) {
		return "", "", ModuleSpec{}, false
	}
	bestLen := -1
	for mod, rawSpec := range m.Modules {
		trimmed := ModuleSpec{
			Version: strings.TrimSpace(rawSpec.Version),
			Path:    strings.TrimSpace(rawSpec.Path),
		}
		if ref == mod {
			if len(mod) > bestLen {
				bestLen = len(mod)
				containerID, relPath, spec, ok = mod, "", trimmed, true
			}
			continue
		}
		if strings.HasPrefix(ref, mod+"/") && len(mod) > bestLen {
			bestLen = len(mod)
			containerID, relPath, spec, ok = mod, strings.TrimPrefix(ref, mod+"/"), trimmed, true
		}
	}
	return containerID, relPath, spec, ok
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
