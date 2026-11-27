package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/api"
)

const ConfigVersion = "1"

type ConfigFile struct {
	Version     string                  `json:"version"`
	Description string                  `json:"description,omitempty"`
	Root        string                  `json:"root,omitempty"`
	Roots       []string                `json:"roots,omitempty"`
	Global      *GlobalConfig           `json:"global,omitempty"`
	Entries     map[string]*EntryConfig `json:"entries"`
}

func (cfg *ConfigFile) RootOverrides() []string {
	if cfg == nil {
		return nil
	}
	var out []string
	if r := strings.TrimSpace(cfg.Root); r != "" {
		out = append(out, r)
	}
	for _, raw := range cfg.Roots {
		if r := strings.TrimSpace(raw); r != "" && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

type GlobalConfig struct {
	Toolchain string         `json:"toolchain,omitempty"`
	Mode      string         `json:"mode,omitempty"`
	Options   map[string]any `json:"options,omitempty"`
}

type EntryConfig struct {
	Options        map[string]any `json:"options,omitempty"`
	KConfig        string         `json:"kconfig,omitempty"`
	SelectedPreset string         `json:"selected_preset,omitempty"`
}

// Clone copies the configuration containers so a phase can mutate option
// values without changing the caller's snapshot until it commits.
func (c *ConfigFile) Clone() *ConfigFile {
	if c == nil {
		return nil
	}
	out := *c
	if c.Roots != nil {
		out.Roots = append([]string(nil), c.Roots...)
	}
	if c.Global != nil {
		global := *c.Global
		global.Options = maps.Clone(c.Global.Options)
		out.Global = &global
	}
	out.Entries = make(map[string]*EntryConfig, len(c.Entries))
	for name, entry := range c.Entries {
		if entry == nil {
			out.Entries[name] = nil
			continue
		}
		cloned := *entry
		cloned.Options = maps.Clone(entry.Options)
		out.Entries[name] = &cloned
	}
	return &out
}

func newConfigFile() *ConfigFile {
	return &ConfigFile{
		Version: ConfigVersion,
		Global:  &GlobalConfig{Options: make(map[string]any)},
		Entries: make(map[string]*EntryConfig),
	}
}

func (cfg *ConfigFile) UnmarshalJSON(data []byte) error {
	type rawConfig struct {
		Version     string                     `json:"version"`
		Description string                     `json:"description"`
		Root        string                     `json:"root"`
		Roots       []string                   `json:"roots"`
		Global      *GlobalConfig              `json:"global,omitempty"`
		Entries     map[string]json.RawMessage `json:"entries"`
	}

	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*cfg = *newConfigFile()
	cfg.Version = raw.Version
	cfg.Description = raw.Description
	cfg.Root = raw.Root
	cfg.Roots = raw.Roots

	if raw.Global != nil {
		cfg.Global = raw.Global
		if cfg.Global.Options == nil {
			cfg.Global.Options = make(map[string]any)
		}
	}

	for name, rawEntry := range raw.Entries {
		entry := &EntryConfig{Options: make(map[string]any)}
		if err := json.Unmarshal(rawEntry, entry); err == nil {
			cfg.Entries[name] = entry
		}
	}

	return nil
}

func PruneEmptyEntries(cfg *ConfigFile) {
	if cfg == nil {
		return
	}
	for name, entry := range cfg.Entries {
		if entry == nil || (len(entry.Options) == 0 && entry.KConfig == "" && entry.SelectedPreset == "") {
			delete(cfg.Entries, name)
		}
	}
	if cfg.Entries == nil {
		cfg.Entries = make(map[string]*EntryConfig)
	}
	if cfg.Global != nil && cfg.Global.Toolchain == "" && cfg.Global.Mode == "" && len(cfg.Global.Options) == 0 {
		cfg.Global = nil
	}
}

func Load(path string) (*ConfigFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newConfigFile(), nil
		}
		return nil, err
	}

	var cfg ConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func Save(path string, cfg *ConfigFile) error {
	return jsonio.Save(path, cfg)
}

func GetEntry(cfg *ConfigFile, name string) *EntryConfig {
	if cfg.Entries == nil {
		return newEntryConfig()
	}
	entry, ok := cfg.Entries[name]
	if !ok {
		return newEntryConfig()
	}
	if entry.Options == nil {
		entry.Options = make(map[string]any)
	}
	return entry
}

func SetEntry(cfg *ConfigFile, name string, entry *EntryConfig) {
	if cfg.Entries == nil {
		cfg.Entries = make(map[string]*EntryConfig)
	}
	cfg.Entries[name] = entry
}

func BuildGlobalValues(cfg *ConfigFile) map[string]any {
	vals := make(map[string]any)
	if cfg.Global != nil {
		if cfg.Global.Toolchain != "" {
			vals["toolchain"] = cfg.Global.Toolchain
		}
		if cfg.Global.Mode != "" {
			vals["mode"] = cfg.Global.Mode
		}
		for k, v := range cfg.Global.Options {
			vals[k] = v
		}
	}
	return vals
}

func newEntryConfig() *EntryConfig {
	return &EntryConfig{Options: make(map[string]any)}
}

func (c *ConfigFile) SetOption(pkg, name string, val any) {
	if c == nil {
		return
	}
	if pkg == "" {
		if c.Global == nil {
			c.Global = &GlobalConfig{Options: make(map[string]any)}
		}
		switch name {
		case api.ToolchainOptionName:
			if s, ok := val.(string); ok {
				c.Global.Toolchain = s
			}
		case api.ModeOptionName:
			if s, ok := val.(string); ok {
				c.Global.Mode = s
			}
		default:
			if c.Global.Options == nil {
				c.Global.Options = make(map[string]any)
			}
			c.Global.Options[name] = val
		}
		return
	}
	entry := GetEntry(c, pkg)
	entry.Options[name] = val
	SetEntry(c, pkg, entry)
}

func (c *ConfigFile) UnsetOption(pkg, name string) bool {
	if c == nil {
		return false
	}
	if pkg == "" {
		if c.Global == nil {
			return false
		}
		switch name {
		case api.ToolchainOptionName:
			had := c.Global.Toolchain != ""
			c.Global.Toolchain = ""
			return had
		case api.ModeOptionName:
			had := c.Global.Mode != ""
			c.Global.Mode = ""
			return had
		default:
			if c.Global.Options == nil {
				return false
			}
			_, had := c.Global.Options[name]
			delete(c.Global.Options, name)
			return had
		}
	}
	if c.Entries == nil {
		return false
	}
	entry, ok := c.Entries[pkg]
	if !ok || entry == nil || entry.Options == nil {
		return false
	}
	_, had := entry.Options[name]
	delete(entry.Options, name)
	return had
}

func (c *ConfigFile) HasExplicitOption(pkg, name string) bool {
	if c == nil {
		return false
	}
	if pkg == "" {
		if c.Global == nil {
			return false
		}
		switch name {
		case api.ToolchainOptionName:
			return c.Global.Toolchain != ""
		case api.ModeOptionName:
			return c.Global.Mode != ""
		default:
			_, ok := c.Global.Options[name]
			return ok
		}
	}
	if c.Entries == nil {
		return false
	}
	entry := c.Entries[pkg]
	if entry == nil || entry.Options == nil {
		return false
	}
	_, ok := entry.Options[name]
	return ok
}

func ParseSetFlag(flag string) (pkgName, optName, rawVal string, err error) {
	eqIdx := strings.Index(flag, "=")
	if eqIdx < 0 {
		return "", "", "", fmt.Errorf("missing '=' in --set flag: %s", flag)
	}
	keyPart := flag[:eqIdx]
	rawVal = flag[eqIdx+1:]
	if keyPart == "" {
		return "", "", "", fmt.Errorf("empty option name in --set flag: %s", flag)
	}
	pkgName, optName, hasColon := strings.Cut(keyPart, ":")
	if !hasColon {
		if strings.Contains(keyPart, "/") {
			return "", "", "", fmt.Errorf("invalid format in --set flag: %s (use '<pkg>:<option>=<value>')", flag)
		}
		return "", keyPart, rawVal, nil
	}
	if pkgName == "" || optName == "" || strings.Contains(optName, ":") {
		return "", "", "", fmt.Errorf("invalid format in --set flag: %s (expected <pkg>:<option>=<value>)", flag)
	}
	if err := api.ValidatePackageRef(pkgName); err != nil {
		return "", "", "", fmt.Errorf("invalid package in --set flag %q: %w", flag, err)
	}
	return pkgName, optName, rawVal, nil
}

func ParseUnsetFlag(flag string) (pkgName, optName string, err error) {
	trimmed := strings.TrimSpace(flag)
	if trimmed == "" {
		return "", "", fmt.Errorf("empty option name in --unset flag")
	}
	if strings.Contains(trimmed, "=") {
		return "", "", fmt.Errorf("unexpected '=' in --unset flag: %s (expected [pkg:]option)", flag)
	}
	pkgName, optName, hasColon := strings.Cut(trimmed, ":")
	if !hasColon {
		if strings.Contains(trimmed, "/") {
			return "", "", fmt.Errorf("invalid format in --unset flag: %s (use '<pkg>:<option>')", flag)
		}
		return "", trimmed, nil
	}
	if pkgName == "" || optName == "" || strings.Contains(optName, ":") {
		return "", "", fmt.Errorf("invalid format in --unset flag: %s (expected <pkg>:<option>)", flag)
	}
	if err := api.ValidatePackageRef(pkgName); err != nil {
		return "", "", fmt.Errorf("invalid package in --unset flag %q: %w", flag, err)
	}
	return pkgName, optName, nil
}

func CoerceOptionValue(opt *api.Option, raw, keyLabel string) (any, error) {
	if opt == nil {
		return nil, fmt.Errorf("nil option definition for %s", keyLabel)
	}
	val, err := CoerceValue(raw, opt.Type())
	if err != nil {
		if keyLabel != "" {
			return nil, fmt.Errorf("%s: %w", keyLabel, err)
		}
		return nil, err
	}
	if opt.Type() == api.OptionChoice {
		s, _ := val.(string)
		if !IsValidChoice(s, opt.Values()) {
			if keyLabel != "" {
				return nil, fmt.Errorf("%s: invalid choice %q (available: %s)", keyLabel, s, strings.Join(opt.Values(), ", "))
			}
			return nil, fmt.Errorf("invalid choice %q (available: %s)", s, strings.Join(opt.Values(), ", "))
		}
	}
	return val, nil
}

func CoerceValue(raw string, optType api.OptionType) (any, error) {
	switch optType {
	case api.OptionBool:
		switch strings.ToLower(raw) {
		case "true", "on", "1":
			return true, nil
		case "false", "off", "0":
			return false, nil
		default:
			return nil, fmt.Errorf("invalid bool value: %q (use true/false, on/off, or 1/0)", raw)
		}
	case api.OptionInt:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid int value: %q", raw)
		}
		if f != float64(int(f)) {
			return nil, fmt.Errorf("invalid int value: %q (must be an integer)", raw)
		}
		return f, nil
	case api.OptionString, api.OptionChoice:
		return raw, nil
	default:
		return nil, fmt.Errorf("unknown option type: %d", optType)
	}
}

func IsValidChoice(val string, choices []string) bool {
	if len(choices) == 0 {
		return true
	}
	for _, c := range choices {
		if c == val {
			return true
		}
	}
	return false
}
