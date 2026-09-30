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
		"on":       {name: "on", optType: OptionBool, defaultVal: true},
		"off":      {name: "off", optType: OptionBool, defaultVal: false},
		"override": {name: "override", optType: OptionBool, defaultVal: false},
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

func TestOptionMacroDefinesDefaults(t *testing.T) {
	tests := []struct {
		name string
		opt  *Option
		val  any
		want []string
	}{
		{"bool true", &Option{name: "debug", optType: OptionBool}, true, []string{"CONFIG_DEBUG=1"}},
		{"bool false", &Option{name: "debug", optType: OptionBool}, false, nil},
		{"int", &Option{name: "size", optType: OptionInt}, float64(42), []string{"CONFIG_SIZE=42"}},
		{"string", &Option{name: "label", optType: OptionString}, "board-a", []string{`CONFIG_LABEL="board-a"`}},
		{"choice", &Option{name: "mcu", optType: OptionChoice}, "py32f539", []string{
			`CONFIG_MCU="py32f539"`, "CONFIG_MCU_PY32F539=1",
		}},
		{"dashes", &Option{name: "tick-hz", optType: OptionInt}, 1000, []string{"CONFIG_TICK_HZ=1000"}},
	}
	for _, tt := range tests {
		got, err := tt.opt.MacroDefines(tt.val)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if len(got) != len(tt.want) || (len(tt.want) > 0 && !reflect.DeepEqual(got, tt.want)) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestOptionMacroDefinesCustomName(t *testing.T) {
	opt := &Option{name: "wifi", optType: OptionBool, macroName: "CONFIG_FIRMWARE_RADIO"}
	got, err := opt.MacroDefines(true)
	if err != nil || !reflect.DeepEqual(got, []string{"CONFIG_FIRMWARE_RADIO=1"}) {
		t.Errorf("enabled: got %v, %v", got, err)
	}
	got, err = opt.MacroDefines(false)
	if err != nil || len(got) != 0 {
		t.Errorf("disabled: got %v, %v", got, err)
	}
}

func TestOptionMacroDefinesTemplate(t *testing.T) {
	opt := &Option{name: "variant", optType: OptionChoice, macroName: "PY32F539xx%s"}
	got, err := opt.MacroDefines("M")
	if err != nil || !reflect.DeepEqual(got, []string{"PY32F539xxM=1"}) {
		t.Errorf("got %v, %v", got, err)
	}
	if _, err := opt.MacroDefines("M-1"); err == nil {
		t.Error("expected invalid rendered macro name error")
	}
}

func TestValidateOptionMacroName(t *testing.T) {
	local := &Option{name: "flag", optType: OptionBool, macroName: "CONFIG_FLAG"}
	if err := ValidateOption(local); err == nil {
		t.Error("SetMacroName on a package option should fail")
	}
	global := &Option{name: "flag", optType: OptionBool, group: GroupGlobal, macroName: "CONFIG_FLAG"}
	if err := ValidateOption(global); err != nil {
		t.Errorf("global option: %v", err)
	}
	bad := &Option{name: "flag", optType: OptionBool, group: GroupGlobal, macroName: "CONFIG-FLAG"}
	if err := ValidateOption(bad); err == nil {
		t.Error("invalid C identifier should fail")
	}
	tmpl := &Option{
		name: "variant", optType: OptionChoice, group: GroupGlobal,
		macroName: "PY32F539xx%s", values: []string{"G", "L", "M"},
	}
	if err := ValidateOption(tmpl); err != nil {
		t.Errorf("choice template: %v", err)
	}
	broken := &Option{
		name: "variant", optType: OptionChoice, group: GroupGlobal,
		macroName: "PY32F539xx%s", values: []string{"M-1"},
	}
	if err := ValidateOption(broken); err == nil {
		t.Error("choice value rendering an invalid macro name should fail")
	}
}

func TestOptionMacroDefinesRejectsInvalidChoiceMacro(t *testing.T) {
	opt := &Option{name: "platform", optType: OptionChoice}
	if _, err := opt.MacroDefines("v1.2"); err == nil {
		t.Error("choice value producing an invalid derived macro name should fail")
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
