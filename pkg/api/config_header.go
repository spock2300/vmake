package api

import (
	"fmt"
	"os"
	"path/filepath"
	"maps"
	"slices"
	"strconv"
	"strings"

	"gitee.com/spock2300/vmake/internal/fs"
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
	ceBoolTrue  configEntryKind = iota
	ceBoolFalse
	ceInt
	ceString
)

type configEntry struct {
	macro string
	val   string
	kind  configEntryKind
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
		macro := configMacroName(name)
		switch typ {
		case OptionBool:
			if v, ok := val.(bool); ok && v {
				entries = append(entries, configEntry{macro: macro, val: "1", kind: ceBoolTrue})
			} else {
				entries = append(entries, configEntry{macro: macro, kind: ceBoolFalse})
			}
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
			entries = append(entries, configEntry{macro: macro, val: s, kind: ceInt})
		case OptionString, OptionChoice:
			entries = append(entries, configEntry{macro: macro, val: fmt.Sprintf("%v", val), kind: ceString})
			if typ == OptionChoice {
				choiceMacro := macro + "_" + strings.ToUpper(strings.ReplaceAll(fmt.Sprintf("%v", val), "-", "_"))
				entries = append(entries, configEntry{macro: choiceMacro, val: "1", kind: ceBoolTrue})
			}
		}
	}
	return entries
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
