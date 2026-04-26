package api

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

func ConfigToDefines(opts map[string]*Option, cfgVals map[string]any) []string {
	names := sortedOptNames(opts)
	var defines []string
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
				defines = append(defines, macro+"=1")
			}
		case OptionInt:
			defines = append(defines, fmt.Sprintf("%s=%v", macro, val))
		case OptionString, OptionChoice:
			defines = append(defines, fmt.Sprintf("%s=\"%s\"", macro, val))
			if typ == OptionChoice {
				choiceMacro := macro + "_" + strings.ToUpper(strings.ReplaceAll(fmt.Sprintf("%v", val), "-", "_"))
				defines = append(defines, choiceMacro+"=1")
			}
		}
	}
	return defines
}

func ConfigToHeader(opts map[string]*Option, cfgVals map[string]any) string {
	names := sortedOptNames(opts)
	var lines []string
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
				lines = append(lines, fmt.Sprintf("#define %s 1", macro))
			} else {
				lines = append(lines, fmt.Sprintf("/* #undef %s */", macro))
			}
		case OptionInt:
			lines = append(lines, fmt.Sprintf("#define %s %v", macro, val))
		case OptionString:
			lines = append(lines, fmt.Sprintf("#define %s \"%v\"", macro, val))
		case OptionChoice:
			lines = append(lines, fmt.Sprintf("#define %s \"%v\"", macro, val))
			choiceMacro := macro + "_" + strings.ToUpper(strings.ReplaceAll(fmt.Sprintf("%v", val), "-", "_"))
			lines = append(lines, fmt.Sprintf("#define %s 1", choiceMacro))
		}
	}

	var sb strings.Builder
	sb.WriteString("#ifndef VMAKE_AUTOCONF_H\n")
	sb.WriteString("#define VMAKE_AUTOCONF_H\n")
	sb.WriteString("\n")
	for _, line := range lines {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString("#endif\n")
	return sb.String()
}

func WriteConfigHeader(dir string, content string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create generated dir: %w", err)
	}
	path := filepath.Join(dir, "autoconf.h")
	data, err := os.ReadFile(path)
	if err == nil && string(data) == content {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func sortedOptNames(opts map[string]*Option) []string {
	names := make([]string, 0, len(opts))
	for name := range opts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
		for k, v := range dep.Options {
			if _, exists := mergedOpts[k]; !exists {
				mergedOpts[k] = v
			}
		}
		for k, v := range dep.CfgVals {
			if _, exists := mergedVals[k]; !exists {
				mergedVals[k] = v
			}
		}
	}
	return mergedOpts, mergedVals
}
