package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestMenuconfigProcessPreservesCommandAndArguments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "source directory")
	program := filepath.Join(t.TempDir(), "tool directory", "menuconfig.exe")
	entry := (&api.KConfigEntry{}).SetSrcDir(dir).SetMenuconfigCmd(program, "--config", "config file")
	cmd, err := menuconfigProcess(entry, "unused", fixedMake("unused-make"))
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != program || cmd.Dir != dir || !slices.Equal(cmd.Args, []string{program, "--config", "config file"}) {
		t.Fatalf("command = %q %q, dir=%q", cmd.Path, cmd.Args, cmd.Dir)
	}
	cmd, err = menuconfigProcess(&api.KConfigEntry{}, dir, fixedMake(program))
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != dir || !slices.Equal(cmd.Args, []string{program, "menuconfig"}) {
		t.Fatalf("default command = %q, dir=%q", cmd.Args, cmd.Dir)
	}
}

func TestEnsureConfigUsesSelectedMakeInsteadOfMenuconfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX test executable")
	}
	dir := filepath.Join(t.TempDir(), "source directory")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	makePath := filepath.Join(t.TempDir(), "selected make")
	program := "#!/bin/sh\n[ \"$1\" = -C ] || exit 11\n[ \"$3\" = selected_defconfig ] || exit 12\n[ \"$#\" = 3 ] || exit 13\nprintf 'CONFIG_TEST=n\\n' > \"$2/.config\"\n"
	if err := os.WriteFile(makePath, []byte(program), 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, ".config")
	if err := os.WriteFile(configPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	entry := (&api.KConfigEntry{}).SetSrcDir(dir).SetConfigPath(".config").SelectPreset("selected_defconfig").SetMenuconfigCmd("not-the-make-program", "menuconfig").SetKConfigPatches(map[string]string{"CONFIG_TEST=n": "CONFIG_TEST=y"})
	msg := ensureConfigCmd("firmware", []*api.KConfigEntry{entry}, "unused", fixedMake(makePath))().(menuconfigDone)
	if msg.err != nil || !msg.ensured {
		t.Fatalf("ensure config: %+v", msg)
	}
	data, err := os.ReadFile(configPath)
	if err != nil || string(data) != "CONFIG_TEST=y\n" {
		t.Fatalf("config = %q, %v", data, err)
	}
}

func TestEnsureConfigFailureIncludesCommand(t *testing.T) {
	program := filepath.Join(t.TempDir(), "missing make")
	entry := (&api.KConfigEntry{}).SetConfigPath(".config").SelectPreset("project_defconfig")
	msg := ensureConfigCmd("firmware", []*api.KConfigEntry{entry}, t.TempDir(), fixedMake(program))().(menuconfigDone)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "missing make") || !strings.Contains(msg.err.Error(), "project_defconfig") {
		t.Fatalf("error = %v", msg.err)
	}
}

func fixedMake(program string) makeResolver {
	return func() (string, error) { return program, nil }
}

func TestMakeIsResolvedOnlyForPresetOrDefaultMenuconfig(t *testing.T) {
	for _, test := range []struct {
		name, config, preset string
		custom               bool
		ensureNeedsMake      bool
	}{
		{"configured-custom", "CONFIG_TEST=y", "selected_defconfig", true, false},
		{"missing-custom-no-preset", "missing", "", true, false},
		{"missing-custom-preset", "missing", "selected_defconfig", true, true},
		{"empty-custom-preset", "", "selected_defconfig", true, true},
		{"configured-default", "CONFIG_TEST=y", "", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if test.config != "missing" {
				if err := os.WriteFile(filepath.Join(dir, ".config"), []byte(test.config), 0644); err != nil {
					t.Fatal(err)
				}
			}
			entry := (&api.KConfigEntry{}).SetSrcDir(dir).SetConfigPath(".config").SelectPreset(test.preset)
			if test.custom {
				entry.SetMenuconfigCmd("custom-menuconfig", "--config", ".config")
			}
			calls := 0
			missingMake := errors.New("selected make unavailable")
			resolve := func() (string, error) { calls++; return "", missingMake }
			msg := ensureConfigCmd("firmware", []*api.KConfigEntry{entry}, dir, resolve)().(menuconfigDone)
			if !msg.ensured || errors.Is(msg.err, missingMake) != test.ensureNeedsMake || (calls > 0) != test.ensureNeedsMake {
				t.Fatalf("ensure = %+v, make lookups = %d", msg, calls)
			}
			calls = 0
			cmd, err := menuconfigProcess(entry, dir, resolve)
			if test.custom {
				if err != nil || cmd == nil || calls != 0 {
					t.Fatalf("custom menu = %v, %v; make lookups = %d", cmd, err, calls)
				}
			} else if cmd != nil || !errors.Is(err, missingMake) || calls != 1 {
				t.Fatalf("default menu = %v, %v; make lookups = %d", cmd, err, calls)
			}
		})
	}
}

func TestCustomMenuconfigStartsWithoutMake(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, ".config"), []byte("CONFIG_TEST=y\n"), 0644); err != nil {
		t.Fatal(err)
	}
	entry := (&api.KConfigEntry{}).SetSrcDir(dir).SetConfigPath(".config").SelectPreset("selected_defconfig").SetMenuconfigCmd(exe, "-test.run=^$")
	m := Model{
		workDir: dir, selectedPkg: "firmware",
		globalValues: map[string]any{"toolchain": "unused-missing-toolchain"},
		kconfigs:     map[string][]*api.KConfigEntry{"firmware": {entry}},
	}
	_, ensure := m.handleOptionsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if ensure == nil || !m.runningMenuconfig || m.menuconfigErr != nil {
		t.Fatalf("menuconfig did not start: %v", m.menuconfigErr)
	}
	msg := ensure().(menuconfigDone)
	if msg.err != nil || !msg.ensured {
		t.Fatalf("ensure = %+v", msg)
	}
	_, run := m.Update(msg)
	if run == nil || m.menuconfigErr != nil {
		t.Fatalf("custom menuconfig was blocked: %v", m.menuconfigErr)
	}
	cmd, err := menuconfigProcess(entry, dir, m.resolveMake())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("custom menuconfig: %v\n%s", err, out)
	}
}

func TestToolchainDiagnosticsPreserveInvalidSelection(t *testing.T) {
	if os.Getenv("VMAKE_TEST_TUI_DIAGNOSTICS") != "1" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(exe, "-test.run=^TestToolchainDiagnosticsPreserveInvalidSelection$")
		cmd.Env = append(os.Environ(), "VMAKE_TEST_TUI_DIAGNOSTICS=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("diagnostics child: %v\n%s", err, out)
		}
		return
	}
	mgr := toolchain.GetManager()
	path := filepath.Join(t.TempDir(), "toolchain.json")
	mgr.RegisterToolchainError("broken", path, errors.New("legacy field install"))
	mgr.RegisterToolchainError("", filepath.Join(t.TempDir(), "unknown.json"), errors.New("invalid JSON"))
	global := map[string]*api.Option{"toolchain": (&api.Option{}).SetType(api.OptionChoice).SetDefault("host").SetValues("host")}
	m := NewModel(nil, nil, nil, nil, t.TempDir(), "broken", global, nil, nil)
	m.language = languageEnglish
	if getToolchainValue(m.globalValues) != "broken" {
		t.Fatalf("selection was replaced: %v", m.globalValues)
	}
	m.width = 120
	footer := m.renderFooter()
	for _, want := range []string{"2 unavailable toolchain definitions", "vmake toolchain list", "broken", "legacy field install"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("footer missing %q: %s", want, footer)
		}
	}
	m.globalValues["toolchain"] = "host"
	if strings.Contains(m.toolchainDiagnostics(), "Selected toolchain") {
		t.Fatal("healthy selection still reports a selection error")
	}
}
