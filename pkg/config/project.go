package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spock2300/vmake/internal/jsonio"
)

const (
	ProjectFilename       = "project.json"
	ConfigsDirName        = "configs"
	DefaultConfigFilename = "default.json"
	DefaultConfigSelector = ConfigsDirName + "/" + DefaultConfigFilename
)

type projectFile struct {
	DefaultConfig string            `json:"default_config,omitempty"`
	Pins          map[string]string `json:"pins,omitempty"`
}

type ProjectConfigInfo struct {
	Name        string
	Path        string
	Active      bool
	Unsaved     bool
	Description string
	Error       error
}

const MaxDescriptionLength = 200

// ValidateDescription 规范化说明文本：去掉首尾空白，限制 MaxDescriptionLength 个字符。
// 允许换行（说明可以多行），其余控制字符一律拒绝。
func ValidateDescription(text string) (string, error) {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n > MaxDescriptionLength {
		return "", fmt.Errorf("description too long: %d characters (maximum %d)", n, MaxDescriptionLength)
	}
	for _, c := range text {
		if c == '\n' {
			continue
		}
		if c < 32 || c == 127 {
			return "", fmt.Errorf("description must not contain control characters other than line breaks")
		}
	}
	return text, nil
}

func ValidateConfigSelector(sel string) error {
	if !strings.HasPrefix(sel, ConfigsDirName+"/") || !strings.HasSuffix(sel, ".json") {
		return fmt.Errorf("invalid config path %q: expected %s/<name>.json", sel, ConfigsDirName)
	}
	if strings.ContainsAny(sel, `\:<>"|?*`) {
		return fmt.Errorf("invalid config path %q: invalid path characters", sel)
	}
	for _, c := range sel {
		if c < 32 {
			return fmt.Errorf("invalid config path %q: control characters are not allowed", sel)
		}
	}
	sub := strings.TrimPrefix(sel, ConfigsDirName+"/")
	if sub == "" || sub == ".json" || strings.HasSuffix(sub, "/.json") {
		return fmt.Errorf("invalid config path %q: empty filename", sel)
	}
	for _, seg := range strings.Split(sub, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") {
			return fmt.Errorf("invalid config path %q: invalid path segment %q", sel, seg)
		}
	}
	if !filepath.IsLocal(filepath.FromSlash(sel)) {
		return fmt.Errorf("invalid config path %q: must be relative to project root", sel)
	}
	return nil
}

func NormalizeConfigReference(ref string) (string, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", fmt.Errorf("empty configuration reference")
	}
	if strings.ContainsAny(trimmed, `\:<>"|?*`) {
		return "", fmt.Errorf("invalid configuration reference %q", ref)
	}
	for _, c := range trimmed {
		if c < 32 {
			return "", fmt.Errorf("invalid configuration reference %q: control characters are not allowed", ref)
		}
	}
	slashed := strings.TrimPrefix(trimmed, "./")
	if !strings.HasPrefix(slashed, ConfigsDirName+"/") {
		slashed = ConfigsDirName + "/" + slashed
	}
	if !strings.HasSuffix(slashed, ".json") {
		slashed += ".json"
	}
	if err := ValidateConfigSelector(slashed); err != nil {
		return "", err
	}
	return slashed, nil
}

func selectorToPath(projectDir, sel string) string {
	return filepath.Join(projectDir, filepath.FromSlash(sel))
}

func ensureSafeConfigPath(projectDir, path string, allowMissing bool) error {
	rootAbs, err := filepath.Abs(projectDir)
	if err != nil {
		return err
	}
	rootReal := rootAbs
	if r, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootReal = r
	}
	configsAllowed := filepath.Join(rootReal, ConfigsDirName)
	isInsideConfigs := func(candidate string) bool {
		rel, relErr := filepath.Rel(configsAllowed, candidate)
		return relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}

	targetAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	checkDir := filepath.Dir(targetAbs)
	for {
		if _, err := os.Lstat(checkDir); err == nil {
			break
		}
		parent := filepath.Dir(checkDir)
		if parent == checkDir {
			break
		}
		checkDir = parent
	}
	dirReal, err := filepath.EvalSymlinks(checkDir)
	if err != nil {
		return fmt.Errorf("resolve config directory %s: %w", checkDir, err)
	}
	relRoot, err := filepath.Rel(rootReal, dirReal)
	if err != nil || relRoot == ".." || strings.HasPrefix(relRoot, ".."+string(filepath.Separator)) {
		return fmt.Errorf("config path %s escapes project root %s", path, projectDir)
	}
	if dirReal != rootReal && !isInsideConfigs(dirReal) {
		return fmt.Errorf("config path %s escapes %s/", path, ConfigsDirName)
	}
	info, lstatErr := os.Lstat(targetAbs)
	if lstatErr != nil {
		if allowMissing && os.IsNotExist(lstatErr) {
			return nil
		}
		return lstatErr
	}
	if info.Mode()&os.ModeSymlink != 0 {
		fileReal, err := filepath.EvalSymlinks(targetAbs)
		if err != nil {
			return fmt.Errorf("resolve config symlink %s: %w", path, err)
		}
		if !isInsideConfigs(fileReal) {
			return fmt.Errorf("config symlink %s escapes %s/", path, ConfigsDirName)
		}
	}
	return nil
}

func projectSelection(projectDir string) (projectFile, bool, error) {
	path := filepath.Join(projectDir, ".vmake", ProjectFilename)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return projectFile{}, false, nil
	}
	if err != nil {
		return projectFile{}, false, fmt.Errorf("read project metadata %s: %w", path, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return projectFile{}, false, fmt.Errorf("parse project metadata %s: %w", path, err)
	}
	if raw == nil {
		return projectFile{}, false, fmt.Errorf("project metadata %s: invalid null content", path)
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var project projectFile
	if err := dec.Decode(&project); err != nil {
		return projectFile{}, false, fmt.Errorf("parse project metadata %s: %w", path, err)
	}
	for pkg, ver := range project.Pins {
		if strings.TrimSpace(pkg) == "" || strings.TrimSpace(ver) == "" {
			return projectFile{}, false, fmt.Errorf("project metadata %s: invalid empty pin for %q", path, pkg)
		}
	}
	_, hasDefault := raw["default_config"]
	if hasDefault {
		norm, err := NormalizeConfigReference(project.DefaultConfig)
		if err != nil {
			return projectFile{}, true, fmt.Errorf("project metadata %s: default_config: %w", path, err)
		}
		project.DefaultConfig = norm
	} else if len(project.Pins) == 0 {
		return projectFile{}, false, fmt.Errorf("project metadata %s: empty metadata file", path)
	}
	return project, hasDefault, nil
}

func localConfigSelection(projectDir string) (string, bool, error) {
	path := LocalPath(projectDir)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read local selection %s: %w", path, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", false, fmt.Errorf("parse local selection %s: %w", path, err)
	}
	if raw == nil {
		return "", false, fmt.Errorf("local selection %s: invalid null content", path)
	}
	var local LocalConfig
	if err := json.Unmarshal(data, &local); err != nil {
		return "", false, fmt.Errorf("parse local selection %s: %w", path, err)
	}
	if _, hasCfg := raw["config"]; hasCfg {
		norm, err := NormalizeConfigReference(local.Config)
		if err != nil {
			return "", true, fmt.Errorf("local selection %s: %w", path, err)
		}
		return norm, true, nil
	}
	return "", false, nil
}

type resolvedSelection struct {
	selector   string
	path       string
	explicit   bool
	sourceFile string
}

func resolveActiveSelection(projectDir string) (resolvedSelection, error) {
	project, hasDefault, err := projectSelection(projectDir)
	if err != nil {
		return resolvedSelection{}, err
	}
	localSel, hasLocal, err := localConfigSelection(projectDir)
	if err != nil {
		return resolvedSelection{}, err
	}
	if hasLocal {
		sel := matchingConfigSelector(projectDir, localSel)
		return resolvedSelection{
			selector:   sel,
			path:       selectorToPath(projectDir, sel),
			explicit:   true,
			sourceFile: LocalPath(projectDir),
		}, nil
	}
	if hasDefault {
		sel := matchingConfigSelector(projectDir, project.DefaultConfig)
		return resolvedSelection{
			selector:   sel,
			path:       selectorToPath(projectDir, sel),
			explicit:   true,
			sourceFile: filepath.Join(projectDir, ".vmake", ProjectFilename),
		}, nil
	}
	defaultPath := selectorToPath(projectDir, DefaultConfigSelector)
	if info, statErr := os.Stat(defaultPath); statErr == nil && !info.IsDir() {
		return resolvedSelection{
			selector: DefaultConfigSelector,
			path:     defaultPath,
			explicit: true,
		}, nil
	}
	return resolvedSelection{
		selector: DefaultConfigSelector,
		path:     defaultPath,
		explicit: false,
	}, nil
}

func ResolveConfigReference(projectDir, ref string) (string, string, error) {
	sel, err := NormalizeConfigReference(ref)
	if err != nil {
		return "", "", err
	}
	sel = matchingConfigSelector(projectDir, sel)
	return sel, selectorToPath(projectDir, sel), nil
}

func LoadProjectPins(projectDir string) (map[string]string, error) {
	project, _, err := projectSelection(projectDir)
	if err != nil {
		return nil, err
	}
	if len(project.Pins) == 0 {
		return make(map[string]string), nil
	}
	out := make(map[string]string, len(project.Pins))
	for k, v := range project.Pins {
		out[k] = v
	}
	return out, nil
}

func SaveProjectPins(projectDir string, pins map[string]string) error {
	project, hasDefault, err := projectSelection(projectDir)
	if err != nil {
		return err
	}
	cleaned := make(map[string]string, len(pins))
	for k, v := range pins {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			return fmt.Errorf("invalid empty pin for %q", k)
		}
		cleaned[k] = v
	}
	path := filepath.Join(projectDir, ".vmake", ProjectFilename)
	if len(cleaned) == 0 && !hasDefault {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove project metadata %s: %w", path, err)
		}
		return nil
	}
	out := projectFile{}
	if hasDefault {
		out.DefaultConfig = project.DefaultConfig
	}
	if len(cleaned) > 0 {
		out.Pins = cleaned
	}
	if err := jsonio.Save(path, out); err != nil {
		return fmt.Errorf("save project metadata %s: %w", path, err)
	}
	return nil
}

func readProjectConfig(projectDir, path string, allowMissing bool) (*ConfigFile, []byte, error) {
	if err := ensureSafeConfigPath(projectDir, path, allowMissing); err != nil {
		if allowMissing && os.IsNotExist(err) {
			return newConfigFile(), nil, nil
		}
		return nil, nil, fmt.Errorf("read config %s: %w", path, err)
	}
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
	cfg, path, _, err := LoadProjectSnapshot(projectDir)
	return cfg, path, err
}

func LoadProjectWithOverride(projectDir, overrideRef string) (*ConfigFile, string, error) {
	cfg, path, _, err := LoadProjectSnapshotWithOverride(projectDir, overrideRef)
	return cfg, path, err
}

func LoadProjectSnapshot(projectDir string) (*ConfigFile, string, string, error) {
	return LoadProjectSnapshotWithOverride(projectDir, "")
}

func LoadProjectSnapshotWithOverride(projectDir, overrideRef string) (*ConfigFile, string, string, error) {
	if strings.TrimSpace(overrideRef) != "" {
		_, path, err := ResolveConfigReference(projectDir, overrideRef)
		if err != nil {
			return nil, "", "", err
		}
		cfg, data, err := readProjectConfig(projectDir, path, false)
		if err != nil {
			return nil, path, "", err
		}
		return cfg, path, fmt.Sprintf("%x", sha256.Sum256(data)), nil
	}
	sel, err := resolveActiveSelection(projectDir)
	if err != nil {
		return nil, "", "", err
	}
	cfg, data, err := readProjectConfig(projectDir, sel.path, !sel.explicit)
	if err != nil && sel.explicit && sel.sourceFile != "" {
		return nil, sel.path, "", fmt.Errorf("config selection %s: %w", sel.sourceFile, err)
	}
	return cfg, sel.path, fmt.Sprintf("%x", sha256.Sum256(data)), err
}

func UseProjectConfig(projectDir, ref string) (string, error) {
	if _, _, err := projectSelection(projectDir); err != nil {
		return "", err
	}
	if _, _, err := localConfigSelection(projectDir); err != nil {
		return "", err
	}
	sel, path, err := ResolveConfigReference(projectDir, ref)
	if err != nil {
		return "", err
	}
	if _, _, err := readProjectConfig(projectDir, path, false); err != nil {
		return "", err
	}
	local, err := LoadLocal(projectDir)
	if err != nil {
		return "", err
	}
	local.Config = sel
	if err := SaveLocal(projectDir, local); err != nil {
		return "", fmt.Errorf("save local selection %s: %w", LocalPath(projectDir), err)
	}
	return sel, nil
}

func SetProjectDescription(projectDir, text string) (string, error) {
	return SetProjectDescriptionWithOverride(projectDir, "", text)
}

func SetProjectDescriptionWithOverride(projectDir, overrideRef, text string) (string, error) {
	text, err := ValidateDescription(text)
	if err != nil {
		return "", err
	}
	cfg, path, err := LoadProjectWithOverride(projectDir, overrideRef)
	if err != nil {
		return "", err
	}
	cfg.Description = text
	PruneEmptyEntries(cfg)
	if err := Save(path, cfg); err != nil {
		return "", fmt.Errorf("save config %s: %w", path, err)
	}
	return path, nil
}

func CopyProjectConfig(projectDir, dst string) error {
	_, err := CopyProjectConfigWithOverride(projectDir, "", dst)
	return err
}

func CopyProjectConfigWithOverride(projectDir, overrideRef, dst string) (string, error) {
	dstSel, err := NormalizeConfigReference(dst)
	if err != nil {
		return "", err
	}
	dstPath := selectorToPath(projectDir, dstSel)

	var cfg *ConfigFile
	var data []byte
	if strings.TrimSpace(overrideRef) != "" {
		_, srcPath, err := ResolveConfigReference(projectDir, overrideRef)
		if err != nil {
			return "", err
		}
		cfg, data, err = readProjectConfig(projectDir, srcPath, false)
		if err != nil {
			return "", err
		}
	} else {
		sel, err := resolveActiveSelection(projectDir)
		if err != nil {
			return "", err
		}
		cfg, data, err = readProjectConfig(projectDir, sel.path, !sel.explicit)
		if err != nil {
			return "", err
		}
	}
	if err := ensureSafeConfigPath(projectDir, dstPath, true); err != nil {
		return "", err
	}
	if data == nil {
		PruneEmptyEntries(cfg)
		data, err = json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", fmt.Errorf("create config %s: %w", dstPath, err)
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(dstPath)
		if writeErr != nil {
			return "", fmt.Errorf("write config %s: %w", dstPath, writeErr)
		}
		return "", fmt.Errorf("close config %s: %w", dstPath, closeErr)
	}
	return dstPath, nil
}

func ListProjectConfigs(projectDir string) ([]ProjectConfigInfo, error) {
	sel, err := resolveActiveSelection(projectDir)
	if err != nil {
		return nil, err
	}

	var configs []ProjectConfigInfo
	activeName := ""

	configsRoot := filepath.Join(projectDir, ConfigsDirName)
	if info, statErr := os.Stat(configsRoot); statErr == nil && info.IsDir() {
		walkErr := filepath.WalkDir(configsRoot, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if path != configsRoot && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".json") || strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			rel, relErr := filepath.Rel(projectDir, path)
			if relErr != nil {
				return nil
			}
			name := filepath.ToSlash(rel)
			if name == sel.selector {
				activeName = name
			}
			item := ProjectConfigInfo{Name: name, Path: path}
			item.Error = ValidateConfigSelector(name)
			if item.Error == nil {
				cfg, _, readErr := readProjectConfig(projectDir, path, false)
				item.Error = readErr
				if readErr == nil {
					item.Description = cfg.Description
				}
			}
			configs = append(configs, item)
			return nil
		})
		if walkErr != nil {
			return nil, fmt.Errorf("list configs in %s: %w", configsRoot, walkErr)
		}
	}

	if activeName == "" {
		activeName = matchingConfigSelector(projectDir, sel.selector)
		hasActiveInList := false
		for _, c := range configs {
			if c.Name == activeName {
				hasActiveInList = true
				break
			}
		}
		if !hasActiveInList {
			activeName = ""
		}
	}
	if activeName != "" {
		for i := range configs {
			configs[i].Active = configs[i].Name == activeName
		}
	} else {
		_, _, readErr := readProjectConfig(projectDir, sel.path, !sel.explicit)
		configs = append(configs, ProjectConfigInfo{
			Name:    sel.selector,
			Path:    sel.path,
			Active:  true,
			Unsaved: !sel.explicit && readErr == nil,
			Error:   readErr,
		})
	}
	sort.Slice(configs, func(i, j int) bool { return configs[i].Name < configs[j].Name })
	return configs, nil
}

func matchingConfigSelector(projectDir, sel string) string {
	fullPath := selectorToPath(projectDir, sel)
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		return sel
	}
	dir := filepath.Dir(fullPath)
	base := filepath.Base(fullPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return sel
	}
	for _, entry := range entries {
		entryName := entry.Name()
		if entry.IsDir() || entryName == base || !strings.EqualFold(entryName, base) {
			continue
		}
		candRel, relErr := filepath.Rel(projectDir, filepath.Join(dir, entryName))
		if relErr != nil {
			continue
		}
		candSel := filepath.ToSlash(candRel)
		if ValidateConfigSelector(candSel) != nil {
			continue
		}
		entryInfo, statErr := os.Stat(filepath.Join(dir, entryName))
		if statErr == nil && os.SameFile(info, entryInfo) {
			return candSel
		}
	}
	return sel
}
