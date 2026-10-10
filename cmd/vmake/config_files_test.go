package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/tui"
)

func TestConfigFileCommandsWithoutBuildScripts(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	project := filepath.Join(dir, "project")
	child := filepath.Join(project, "child", "nested")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) { panic("BUILD_SCRIPT_EXECUTED") }
`)
	lock := `{"lockfileVersion":1,"packages":{}}`
	writeExtensionFile(t, filepath.Join(project, ".vmake", "vmake.lock"), lock)
	run := func(cwd string, args ...string) string {
		t.Helper()
		out, err := extensionCommand(t, state, cwd, args...)
		if err != nil || strings.Contains(out, "BUILD_SCRIPT_EXECUTED") {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return out
	}
	if out := run(child, "config", "list"); !strings.Contains(out, "* config.json (not saved yet)") {
		t.Fatal(out)
	}
	run(child, "config", "copy", "config-debug.json")
	if _, err := os.Stat(filepath.Join(project, ".vmake", "project.json")); !os.IsNotExist(err) {
		t.Fatalf("copy created selection: %v", err)
	}
	run(child, "config", "use", "config-debug.json")
	run(child, "config", "describe", "Board B 调试配置")
	if out := run(child, "config", "describe"); strings.TrimSpace(out) != "Board B 调试配置" {
		t.Fatalf("describe = %q", out)
	}
	if out, err := extensionCommand(t, state, child, "config", "describe", "bad\x01text"); err == nil || strings.Contains(out, "BUILD_SCRIPT_EXECUTED") {
		t.Fatalf("describe accepted invalid text: %v\n%s", err, out)
	}
	run(project, "config", "copy", "config-other.json")
	writeExtensionFile(t, filepath.Join(project, ".vmake", "broken.json"), "{")
	listing := run(child, "config", "list")
	if !strings.Contains(listing, "* config-debug.json  Board B 调试配置") || !strings.Contains(listing, "broken.json (invalid:") {
		t.Fatal(listing)
	}
	for _, args := range [][]string{{"config", "copy", "config-debug.json"}, {"config", "use", "missing.json"}, {"config", "use", "broken.json"}, {"config", "use", "../escape.json"}, {"config", "typo"}} {
		out, err := extensionCommand(t, state, child, args...)
		if err == nil || strings.Contains(out, "BUILD_SCRIPT_EXECUTED") {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	if _, path, err := config.LoadProject(project); err != nil || filepath.Base(path) != "config-debug.json" {
		t.Fatalf("failed commands changed selection: %s, %v", path, err)
	}
	completion := run(child, "__complete", "config", "use", "")
	if !strings.Contains(completion, "config-debug.json") || !strings.Contains(completion, "config-other.json") || !strings.Contains(completion, "config-debug.json\tBoard B 调试配置") || strings.Contains(completion, "broken.json") || strings.Contains(completion, "project.json") {
		t.Fatal(completion)
	}
	run(child, "config", "describe", "")
	if out := run(child, "config", "describe"); strings.TrimSpace(out) != "" {
		t.Fatalf("cleared describe = %q", out)
	}
	if err := os.Remove(filepath.Join(project, ".vmake", "config-debug.json")); err != nil {
		t.Fatal(err)
	}
	run(child, "config", "use", "config-other.json")
	data, err := os.ReadFile(filepath.Join(project, ".vmake", "vmake.lock"))
	if err != nil || string(data) != lock {
		t.Fatalf("management changed shared lock: %q, %v", data, err)
	}
	for _, name := range []string{"project.json", "config-other.json"} {
		if _, err := os.Stat(filepath.Join(child, ".vmake", name)); !os.IsNotExist(err) {
			t.Fatalf("created child configuration %s: %v", name, err)
		}
	}
}

func TestConfigSwitchBuildQueryAndClean(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	project := filepath.Join(dir, "project")
	writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "healthy", "toolchain.json"), "healthy")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import (
    "os"
    "path/filepath"
    "github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.Option("label").SetType(api.OptionString).SetDefault("default")
    })
    p.OnBuild(func(ctx *api.BuildContext) {
        label := ctx.String("label")
        ctx.Target("record").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
            if err := os.WriteFile(filepath.Join(pkg.BuildDir(), "artifact"), []byte(label), 0644); err != nil { return err }
            return os.WriteFile(filepath.Join(pkg.SourceDir(), "last-build"), []byte(pkg.BuildDir()), 0644)
        })
    })
}
`)
	a := `{"version":"1","global":{"toolchain":"healthy","mode":"debug"},"entries":{"project":{"options":{"label":"A"},"kconfig":"CONFIG_A=y\n","selected_preset":"a"}}}`
	b := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(a, "debug", "release"), `"A"`, `"B"`), "CONFIG_A", "CONFIG_B")
	writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), a)
	writeExtensionFile(t, filepath.Join(project, ".vmake", "board-b.json"), b)
	writeExtensionFile(t, filepath.Join(project, ".vmake", "vmake.lock"), `{"lockfileVersion":1,"packages":{}}`)
	run := func(args ...string) string {
		t.Helper()
		out, err := extensionCommand(t, state, project, args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return out
	}
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	build := func(want string) string {
		t.Helper()
		run("build")
		buildDir := read(filepath.Join(project, "last-build"))
		if got := read(filepath.Join(buildDir, "artifact")); got != want {
			t.Fatalf("built %q, want %q", got, want)
		}
		return buildDir
	}
	aDir := build("A")
	if _, err := os.Stat(filepath.Join(project, ".vmake", "project.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy build created selection: %v", err)
	}
	run("config", "use", "board-b.json")
	query := run("query", "config", "project")
	if !strings.Contains(query, "label=B") || !strings.Contains(query, `CONFIG_LABEL="B"`) {
		t.Fatal(query)
	}
	if out := run("config", "--set", "project/label=B-edited"); !strings.Contains(out, "board-b.json") {
		t.Fatal(out)
	}
	if got := read(filepath.Join(project, ".vmake", "config.json")); got != a {
		t.Fatal("--set changed inactive config")
	}
	cfg, _, err := config.LoadProject(project)
	if err != nil || cfg.Entries["project"].KConfig != "CONFIG_B=y\n" {
		t.Fatalf("--set lost KConfig: %+v, %v", cfg, err)
	}
	bDir := build("B-edited")
	if aDir == bDir {
		t.Fatal("different configurations shared a build directory")
	}
	run("config", "copy", "board-b-copy.json")
	run("config", "use", "board-b-copy.json")
	if got := build("B-edited"); got != bDir {
		t.Fatalf("equivalent file changed BuildKey: %s != %s", got, bDir)
	}
	run("config", "use", "config.json")
	if got := build("A"); got != aDir {
		t.Fatalf("switch back changed BuildKey: %s != %s", got, aDir)
	}
	run("clean")
	if _, err := os.Stat(aDir); !os.IsNotExist(err) {
		t.Fatalf("clean retained selected artifacts: %v", err)
	}
	if got := read(filepath.Join(bDir, "artifact")); got != "B-edited" {
		t.Fatal("clean changed inactive artifacts")
	}
	build("A")
	for _, args := range [][]string{{"clean", "--all"}, {"distclean"}} {
		run(args...)
		for _, path := range []string{aDir, bDir} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("%v retained artifacts %s: %v", args, path, err)
			}
		}
		for _, name := range []string{"config.json", "board-b.json", "board-b-copy.json", "project.json", "vmake.lock"} {
			if _, err := os.Stat(filepath.Join(project, ".vmake", name)); err != nil {
				t.Fatalf("%v removed %s: %v", args, name, err)
			}
		}
	}
}

func TestConfigConsumersRejectMissingSelectionFromSubdirectory(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	project := filepath.Join(dir, "project")
	child := filepath.Join(project, "child")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	writeExtensionFile(t, filepath.Join(project, "build.go"), "package main")
	writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), `{"version":"1","entries":{}}`)
	writeExtensionFile(t, filepath.Join(project, ".vmake", "project.json"), `{"config":"missing-selected.json"}`)
	for _, args := range [][]string{{"build"}, {"test"}, {"query"}, {"config", "--set", "mode=debug"}, {"clean"}, {"distclean"}, {"lock", "update"}, {"doctor"}} {
		out, err := extensionCommand(t, state, child, args...)
		if err == nil || !strings.Contains(out, filepath.Join(project, ".vmake", "missing-selected.json")) {
			t.Errorf("%v did not reject project selection: %v\n%s", args, err, out)
		}
	}
}

func TestConfigDoctorUsesProjectSelection(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	project := filepath.Join(dir, "project")
	child := filepath.Join(project, "child")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	writeExtensionFile(t, filepath.Join(project, "build.go"), "package main")
	writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), `{"global":{"toolchain":"unused-default"}}`)
	writeExtensionFile(t, filepath.Join(project, ".vmake", "selected.json"), `{"global":{"toolchain":"selected-missing"}}`)
	writeExtensionFile(t, filepath.Join(project, ".vmake", "project.json"), `{"config":"selected.json"}`)
	for _, cwd := range []string{project, child} {
		out, err := extensionCommand(t, state, cwd, "doctor")
		if err == nil || !strings.Contains(out, "selected-missing") || strings.Contains(out, "unused-default") {
			t.Fatalf("doctor in %s: %v\n%s", cwd, err, out)
		}
	}
}

func TestConfigTUISavesOnlySelectedFile(t *testing.T) {
	project := t.TempDir()
	original := `{"version":"1","entries":{}}`
	selected := `{"version":"1","entries":{"firmware":{"version":"v1","kconfig":"CONFIG_KEEP=y\n","selected_preset":"board"}}}`
	writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), original)
	writeExtensionFile(t, filepath.Join(project, ".vmake", "board.json"), selected)
	writeExtensionFile(t, filepath.Join(project, ".vmake", "project.json"), `{"config":"board.json"}`)
	cfg, path, err := config.LoadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &RuntimeContext{Config: cfg, ConfigPath: path}
	result := &tui.ConfigResult{Saved: true, Description: "Board A 调试配置", Toolchain: "host", GlobalValues: map[string]any{"mode": "debug"}, Values: map[string]map[string]any{"firmware": {"enabled": true}}}
	if err := saveConfigResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(project, ".vmake", "config.json"))
	if err != nil || string(data) != original {
		t.Fatalf("TUI touched inactive file: %q, %v", data, err)
	}
	saved, _, err := config.LoadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	entry := saved.Entries["firmware"]
	if saved.Description != "Board A 调试配置" || saved.Global.Mode != "debug" || entry.Options["enabled"] != true || entry.Version != "v1" || entry.KConfig != "CONFIG_KEEP=y\n" || entry.SelectedPreset != "board" {
		data, _ := json.Marshal(saved)
		t.Fatalf("TUI saved unexpected config: %s", data)
	}
}
