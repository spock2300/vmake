package api

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
)

func configMacroName(optName string) string {
	return "CONFIG_" + strings.ToUpper(strings.ReplaceAll(optName, "-", "_"))
}

func resolveOptVal(opts map[string]*Option, cfgVals map[string]any, name string) (any, OptionType) {
	opt := opts[name]
	if val, ok := cfgVals[name]; ok {
		return val, opt.Type()
	}
	if opt.Default() != nil {
		return opt.Default(), opt.Type()
	}
	return nil, opt.Type()
}

type configEntryKind int

const (
	ceBoolTrue configEntryKind = iota
	ceBoolFalse
	ceInt
	ceString
)

type configEntry struct {
	macro string
	val   string
	kind  configEntryKind
}

func validMacroName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func macroEntries(macro string, typ OptionType, val any) []configEntry {
	switch typ {
	case OptionBool:
		if v, ok := val.(bool); ok && v {
			return []configEntry{{macro: macro, val: "1", kind: ceBoolTrue}}
		}
		return []configEntry{{macro: macro, kind: ceBoolFalse}}
	case OptionInt:
		var s string
		switch v := val.(type) {
		case int:
			s = strconv.Itoa(v)
		case float64:
			s = strconv.Itoa(int(v))
		case int64:
			s = strconv.FormatInt(v, 10)
		default:
			s = fmt.Sprintf("%d", v)
		}
		return []configEntry{{macro: macro, val: s, kind: ceInt}}
	case OptionString, OptionChoice:
		entries := []configEntry{{macro: macro, val: fmt.Sprintf("%v", val), kind: ceString}}
		if typ == OptionChoice {
			choiceMacro := macro + "_" + strings.ToUpper(strings.ReplaceAll(fmt.Sprintf("%v", val), "-", "_"))
			entries = append(entries, configEntry{macro: choiceMacro, val: "1", kind: ceBoolTrue})
		}
		return entries
	}
	return nil
}

func collectConfigEntries(opts map[string]*Option, cfgVals map[string]any) []configEntry {
	names := slices.Sorted(maps.Keys(opts))
	var entries []configEntry
	for _, name := range names {
		opt := opts[name]
		if opt.IsGlobal() {
			continue
		}
		val, typ := resolveOptVal(opts, cfgVals, name)
		if val == nil {
			continue
		}
		entries = append(entries, macroEntries(configMacroName(name), typ, val)...)
	}
	return entries
}

func (o *Option) MacroDefines(val any) ([]string, error) {
	macro := configMacroName(o.name)
	if custom := o.MacroName(); custom != "" {
		if strings.Contains(custom, "%") {
			rendered := fmt.Sprintf(custom, fmt.Sprintf("%v", val))
			if !validMacroName(rendered) {
				return nil, fmt.Errorf("option %q: macro name %q is not a valid C identifier", o.name, rendered)
			}
			return []string{rendered + "=1"}, nil
		}
		macro = custom
	}
	if !validMacroName(macro) {
		return nil, fmt.Errorf("option %q: macro name %q is not a valid C identifier", o.name, macro)
	}
	var defines []string
	for _, e := range macroEntries(macro, o.Type(), val) {
		if !validMacroName(e.macro) {
			return nil, fmt.Errorf("option %q: macro name %q is not a valid C identifier", o.name, e.macro)
		}
		switch e.kind {
		case ceBoolTrue, ceInt:
			defines = append(defines, e.macro+"="+e.val)
		case ceString:
			defines = append(defines, fmt.Sprintf("%s=\"%s\"", e.macro, e.val))
		}
	}
	return defines, nil
}

func ConfigToDefines(opts map[string]*Option, cfgVals map[string]any) []string {
	var defines []string
	for _, e := range collectConfigEntries(opts, cfgVals) {
		switch e.kind {
		case ceBoolTrue, ceInt:
			defines = append(defines, e.macro+"="+e.val)
		case ceString:
			defines = append(defines, fmt.Sprintf("%s=\"%s\"", e.macro, e.val))
		}
	}
	return defines
}

func ConfigToHeader(opts map[string]*Option, cfgVals map[string]any) string {
	var sb strings.Builder
	sb.WriteString("#ifndef VMAKE_AUTOCONF_H\n")
	sb.WriteString("#define VMAKE_AUTOCONF_H\n\n")
	for _, e := range collectConfigEntries(opts, cfgVals) {
		switch e.kind {
		case ceBoolTrue:
			sb.WriteString(fmt.Sprintf("#define %s 1\n", e.macro))
		case ceBoolFalse:
			sb.WriteString(fmt.Sprintf("/* #undef %s */\n", e.macro))
		case ceInt:
			sb.WriteString(fmt.Sprintf("#define %s %s\n", e.macro, e.val))
		case ceString:
			sb.WriteString(fmt.Sprintf("#define %s \"%s\"\n", e.macro, e.val))
		}
	}
	sb.WriteString("\n#endif\n")
	return sb.String()
}

func WriteConfigHeader(dir string, content string) error {
	if err := fs.EnsureDir(dir); err != nil {
		return err
	}
	path := filepath.Join(dir, "autoconf.h")
	data, err := os.ReadFile(path)
	if err == nil && string(data) == content {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// mergeMapNoOverwrite copies entries from src into dst, without overwriting existing keys.
func mergeMapNoOverwrite[K comparable, V any](dst, src map[K]V) {
	for k, v := range src {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}

func MergeImportedOptions(localOpts map[string]*Option, localVals map[string]any, pkgs []*Package) (map[string]*Option, map[string]any) {
	mergedOpts := make(map[string]*Option, len(localOpts))
	mergedVals := make(map[string]any, len(localVals))
	for k, v := range localOpts {
		mergedOpts[k] = v
	}
	for k, v := range localVals {
		mergedVals[k] = v
	}
	for _, dep := range pkgs {
		mergeMapNoOverwrite(mergedOpts, dep.Options)
		mergeMapNoOverwrite(mergedVals, dep.CfgVals)
	}
	return mergedOpts, mergedVals
}
