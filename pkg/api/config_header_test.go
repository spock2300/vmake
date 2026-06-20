package api

import (
	"reflect"
	"strings"
	"testing"
)

func TestConfigMacroName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"feature", "CONFIG_FEATURE"},
		{"my-flag", "CONFIG_MY_FLAG"},
		{"Mixed-Case", "CONFIG_MIXED_CASE"},
	}
	for _, tt := range tests {
		if got := configMacroName(tt.in); got != tt.want {
			t.Errorf("configMacroName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestConfigToDefinesBool(t *testing.T) {
	opts := map[string]*Option{
		"on":         {name: "on", optType: OptionBool, defaultVal: true},
		"off":        {name: "off", optType: OptionBool, defaultVal: false},
		"override":   {name: "override", optType: OptionBool, defaultVal: false},
	}
	vals := map[string]any{"override": true}
	defs := ConfigToDefines(opts, vals)

	if !contains(defs, "CONFIG_ON=1") {
		t.Errorf("expected CONFIG_ON=1 in %v", defs)
	}
	if contains(defs, "CONFIG_OFF=") {
		t.Errorf("CONFIG_OFF should not appear (false bool), got %v", defs)
	}
	if !contains(defs, "CONFIG_OVERRIDE=1") {
		t.Errorf("expected CONFIG_OVERRIDE=1 (overridden to true), got %v", defs)
	}
}

func TestConfigToDefinesInt(t *testing.T) {
	opts := map[string]*Option{
		"size": {name: "size", optType: OptionInt, defaultVal: 0},
	}
	vals := map[string]any{"size": float64(42)}
	defs := ConfigToDefines(opts, vals)
	if !contains(defs, "CONFIG_SIZE=42") {
		t.Errorf("expected CONFIG_SIZE=42 in %v", defs)
	}
}

func TestConfigToDefinesString(t *testing.T) {
	opts := map[string]*Option{
		"name": {name: "name", optType: OptionString, defaultVal: ""},
	}
	vals := map[string]any{"name": "hello"}
	defs := ConfigToDefines(opts, vals)
	if !contains(defs, `CONFIG_NAME="hello"`) {
		t.Errorf("expected CONFIG_NAME=\"hello\" in %v", defs)
	}
}

func TestConfigToDefinesChoiceEmitsExtraMacro(t *testing.T) {
	opts := map[string]*Option{
		"arch": {name: "arch", optType: OptionChoice, defaultVal: "arm"},
	}
	vals := map[string]any{"arch": "arm-cortex"}
	defs := ConfigToDefines(opts, vals)
	if !contains(defs, `CONFIG_ARCH="arm-cortex"`) {
		t.Errorf("missing choice value define in %v", defs)
	}
	if !contains(defs, "CONFIG_ARCH_ARM_CORTEX=1") {
		t.Errorf("missing choice selection macro in %v", defs)
	}
}

func TestConfigToDefinesSkipsGlobal(t *testing.T) {
	opts := map[string]*Option{
		"global-opt": {name: "global-opt", optType: OptionBool, defaultVal: true, group: "Global"},
		"local-opt":  {name: "local-opt", optType: OptionBool, defaultVal: true},
	}
	defs := ConfigToDefines(opts, nil)
	if contains(defs, "CONFIG_GLOBAL_OPT") {
		t.Errorf("global option should be skipped, got %v", defs)
	}
	if !contains(defs, "CONFIG_LOCAL_OPT=1") {
		t.Errorf("local option should appear, got %v", defs)
	}
}

func TestConfigToHeaderStructure(t *testing.T) {
	opts := map[string]*Option{
		"on":  {name: "on", optType: OptionBool, defaultVal: true},
		"off": {name: "off", optType: OptionBool, defaultVal: false},
		"n":   {name: "n", optType: OptionInt, defaultVal: 5},
		"s":   {name: "s", optType: OptionString, defaultVal: "x"},
	}
	h := ConfigToHeader(opts, nil)
	if !strings.HasPrefix(h, "#ifndef VMAKE_AUTOCONF_H\n") {
		t.Errorf("missing guard header: %q", h[:40])
	}
	if !strings.HasSuffix(h, "\n#endif\n") {
		t.Errorf("missing guard footer: ...%q", h[len(h)-20:])
	}
	if !strings.Contains(h, "#define CONFIG_ON 1\n") {
		t.Errorf("missing #define CONFIG_ON 1")
	}
	if !strings.Contains(h, "/* #undef CONFIG_OFF */\n") {
		t.Errorf("missing /* #undef CONFIG_OFF */")
	}
	if !strings.Contains(h, `#define CONFIG_N 5`+"\n") {
		t.Errorf("missing #define CONFIG_N 5")
	}
	if !strings.Contains(h, "#define CONFIG_S \"x\"\n") {
		t.Errorf(`missing #define CONFIG_S "x"`)
	}
}

func TestMergeImportedOptionsLocalWins(t *testing.T) {
	localOpt := &Option{name: "shared", defaultVal: "local"}
	importedOpt := &Option{name: "shared", defaultVal: "imported"}
	onlyImported := &Option{name: "only-imported", defaultVal: "from-dep"}

	localOpts := map[string]*Option{"shared": localOpt}
	localVals := map[string]any{"shared": "local-value", "only-local": "L"}

	dep := NewPackage()
	dep.Options = map[string]*Option{"shared": importedOpt, "only-imported": onlyImported}
	dep.CfgVals = map[string]any{"shared": "imported-value", "only-imported": "I"}

	mergedOpts, mergedVals := MergeImportedOptions(localOpts, localVals, []*Package{dep})

	if mergedOpts["shared"] != localOpt {
		t.Error("local option should win on collision")
	}
	if mergedOpts["only-imported"] != onlyImported {
		t.Error("imported-only option should be added")
	}
	if mergedVals["shared"] != "local-value" {
		t.Error("local value should win on collision")
	}
	if mergedVals["only-imported"] != "I" {
		t.Error("imported-only value should be added")
	}
	if mergedVals["only-local"] != "L" {
		t.Error("local-only value should be preserved")
	}
}

func TestMergeMapNoOverwrite(t *testing.T) {
	dst := map[string]int{"a": 1, "b": 2}
	src := map[string]int{"a": 99, "c": 3}
	mergeMapNoOverwrite(dst, src)

	want := map[string]int{"a": 1, "b": 2, "c": 3}
	if !reflect.DeepEqual(dst, want) {
		t.Errorf("got %v, want %v", dst, want)
	}
}

func contains(slice []string, want string) bool {
	for _, s := range slice {
		if s == want {
			return true
		}
	}
	return false
}
