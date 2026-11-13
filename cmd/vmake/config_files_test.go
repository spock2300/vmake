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
	if out := run(child, "config", "list"); !strings.Contains(out, "* configs/default.json (not saved yet)") {
		t.Fatal(out)
	}
	run(child, "config", "copy", "config-debug")
	if _, err := os.Stat(filepath.Join(project, ".vmake", "local.json")); !os.IsNotExist(err) {
		t.Fatalf("copy created local selection: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".vmake", "project.json")); !os.IsNotExist(err) {
		t.Fatalf("copy created project metadata: %v", err)
	}
	run(child, "config", "use", "config-debug")
	run(child, "config", "describe", "Board B 调试配置")
	if out := run(child, "config", "describe"); strings.TrimSpace(out) != "Board B 调试配置" {
		t.Fatalf("describe = %q", out)
	}
	if out, err := extensionCommand(t, state, child, "config", "describe", "bad\x01text"); err == nil || strings.Contains(out, "BUILD_SCRIPT_EXECUTED") {
		t.Fatalf("describe accepted invalid text: %v\n%s", err, out)
	}
	run(project, "config", "copy", "test/config-other.json")
	writeExtensionFile(t, filepath.Join(project, "configs", "broken.json"), "{")
	listing := run(child, "config", "list")
	if !strings.Contains(listing, "* configs/config-debug.json  Board B 调试配置") ||
		!strings.Contains(listing, "configs/test/config-other.json") ||
		!strings.Contains(listing, "configs/broken.json (invalid:") {
		t.Fatal(listing)
	}
	for _, args := range [][]string{
		{"config", "copy", "config-debug"},
		{"config", "use", "missing"},
		{"config", "use", "broken.json"},
		{"config", "use", "../escape.json"},
		{"config", "typo"},
	} {
		out, err := extensionCommand(t, state, child, args...)
		if err == nil || strings.Contains(out, "BUILD_SCRIPT_EXECUTED") {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	if _, path, err := config.LoadProject(project); err != nil || filepath.Base(path) != "config-debug.json" {
		t.Fatalf("failed commands changed selection: %s, %v", path, err)
	}
	completion := run(child, "__complete", "config", "use", "")
	if !strings.Contains(completion, "configs/config-debug.json") ||
		!strings.Contains(completion, "configs/test/config-other.json") ||
		!strings.Contains(completion, "configs/config-debug.json\tBoard B 调试配置") ||
		strings.Contains(completion, "broken.json") ||
		strings.Contains(completion, "project.json") ||
		strings.Contains(completion, "local.json") {
		t.Fatal(completion)
	}
	run(child, "config", "describe", "")
	if out := run(child, "config", "describe"); strings.TrimSpace(out) != "" {
		t.Fatalf("cleared describe = %q", out)
	}
	if err := os.Remove(filepath.Join(project, "configs", "config-debug.json")); err != nil {
		t.Fatal(err)
	}
	run(child, "config", "use", "test/config-other")
	data, err := os.ReadFile(filepath.Join(project, ".vmake", "vmake.lock"))
	if err != nil || string(data) != lock {
		t.Fatalf("management changed shared lock: %q, %v", data, err)
	}
	for _, rel := range []string{".vmake/local.json", "configs/config-other.json"} {
		if _, err := os.Stat(filepath.Join(child, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("created child configuration %s: %v", rel, err)
		}
	}
}

func TestConfigSwitchStatelessOverrideBuildQueryAndClean(t *testing.T) {
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
        ctx.Option("extra").SetType(api.OptionBool).SetDefault(false)
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
	writeExtensionFile(t, filepath.Join(project, "configs", "default.json"), a)
	writeExtensionFile(t, filepath.Join(project, "configs", "board-b.json"), b)
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
	build := func(want string, extraArgs ...string) string {
		t.Helper()
		args := append([]string{"build"}, extraArgs...)
		run(args...)
		buildDir := read(filepath.Join(project, "last-build"))
		if got := read(filepath.Join(buildDir, "artifact")); got != want {
			t.Fatalf("built %q, want %q", got, want)
		}
		return buildDir
	}
	aDir := build("A")
	if _, err := os.Stat(filepath.Join(project, ".vmake", "local.json")); !os.IsNotExist(err) {
		t.Fatalf("default build created local selection: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".vmake", "project.json")); !os.IsNotExist(err) {
		t.Fatalf("default build created project.json: %v", err)
	}

	bStatelessDir := build("B", "-c", "board-b")
	if bStatelessDir == aDir {
		t.Fatal("stateless -c shared build directory with default")
	}
	if _, err := os.Stat(filepath.Join(project, ".vmake", "local.json")); !os.IsNotExist(err) {
		t.Fatalf("stateless -c build created local.json: %v", err)
	}
	queryStateless := run("query", "config", "project", "-c", "board-b")
	if !strings.Contains(queryStateless, "label=B") {
		t.Fatal(queryStateless)
	}

	run("config", "use", "board-b")
	query := run("query", "config", "project")
	if !strings.Contains(query, "label=B") || !strings.Contains(query, `CONFIG_LABEL="B"`) {
		t.Fatal(query)
	}
	if out := run("config", "--set", "project/label=B-edited", "--set", "project/extra=true"); !strings.Contains(out, "board-b.json") {
		t.Fatal(out)
	}
	if got := read(filepath.Join(project, "configs", "default.json")); got != a {
		t.Fatal("--set changed inactive config")
	}
	if out := run("config", "--unset", "project/extra"); !strings.Contains(out, "unset project/extra") {
		t.Fatal(out)
	}
	cfg, _, err := config.LoadProject(project)
	if err != nil || cfg.Entries["project"].KConfig != "CONFIG_B=y\n" {
		t.Fatalf("--set/--unset lost KConfig: %+v, %v", cfg, err)
	}
	if _, exists := cfg.Entries["project"].Options["extra"]; exists {
		t.Fatalf("--unset did not remove project/extra: %+v", cfg.Entries["project"].Options)
	}
	bDir := build("B-edited")
	if aDir == bDir {
		t.Fatal("different configurations shared a build directory")
	}
	run("config", "copy", "board-b-copy")
	run("config", "use", "board-b-copy")
	if got := build("B-edited"); got != bDir {
		t.Fatalf("equivalent file changed BuildKey: %s != %s", got, bDir)
	}
	run("config", "use", "default")
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
		for _, rel := range []string{"configs/default.json", "configs/board-b.json", "configs/board-b-copy.json", ".vmake/local.json", ".vmake/vmake.lock"} {
			if _, err := os.Stat(filepath.Join(project, filepath.FromSlash(rel))); err != nil {
				t.Fatalf("%v removed %s: %v", args, rel, err)
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
	writeExtensionFile(t, filepath.Join(project, "configs", "default.json"), `{"version":"1","entries":{}}`)
	writeExtensionFile(t, filepath.Join(project, ".vmake", "project.json"), `{"default_config":"configs/missing-selected.json"}`)
	for _, args := range [][]string{{"build"}, {"test"}, {"query"}, {"config", "--set", "mode=debug"}, {"clean"}, {"distclean"}, {"deps", "update"}, {"doctor"}} {
		out, err := extensionCommand(t, state, child, args...)
		if err == nil || !strings.Contains(out, filepath.Join(project, "configs", "missing-selected.json")) {
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
	writeExtensionFile(t, filepath.Join(project, "configs", "default.json"), `{"global":{"toolchain":"unused-default"}}`)
	writeExtensionFile(t, filepath.Join(project, "configs", "selected.json"), `{"global":{"toolchain":"selected-missing"}}`)
	writeExtensionFile(t, filepath.Join(project, ".vmake", "project.json"), `{"default_config":"configs/selected.json"}`)
	for _, cwd := range []string{project, child} {
		out, err := extensionCommand(t, state, cwd, "doctor")
		if err == nil || !strings.Contains(out, "selected-missing") || strings.Contains(out, "unused-default") {
			t.Fatalf("doctor in %s: %v\n%s", cwd, err, out)
		}
	}
}

func TestConfigTUISavesOnlySelectedFileSparsely(t *testing.T) {
	project := t.TempDir()
	original := `{"version":"1"}`
	selected := `{"version":"1","entries":{"firmware":{"options":{"stale_explicit":true},"kconfig":"CONFIG_KEEP=y\n","selected_preset":"board"},"other":{"options":{"drop_me":true}}}}`
	writeExtensionFile(t, filepath.Join(project, "configs", "default.json"), original)
	writeExtensionFile(t, filepath.Join(project, "configs", "board.json"), selected)
	writeExtensionFile(t, filepath.Join(project, ".vmake", "local.json"), `{"config":"configs/board.json"}`)
	cfg, path, err := config.LoadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &RuntimeContext{Config: cfg, ConfigPath: path}
	result := &tui.ConfigResult{
		Saved:        true,
		Description:  "Board A 调试配置",
		Toolchain:    "host",
		GlobalValues: map[string]any{"mode": "debug", "toolchain": "host"},
		Values:       map[string]map[string]any{"firmware": {"enabled": true}},
	}
	if err := saveConfigResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(project, "configs", "default.json"))
	if err != nil || string(data) != original {
		t.Fatalf("TUI touched inactive file: %q, %v", data, err)
	}
	saved, _, err := config.LoadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if _, hasOther := saved.Entries["other"]; hasOther {
		t.Fatalf("expected pruned 'other' entry to be removed, got %+v", saved.Entries)
	}
	entry := saved.Entries["firmware"]
	if saved.Description != "Board A 调试配置" || saved.Global.Mode != "debug" || entry.Options["enabled"] != true || entry.Options["stale_explicit"] != nil || entry.KConfig != "CONFIG_KEEP=y\n" || entry.SelectedPreset != "board" {
		data, _ := json.Marshal(saved)
		t.Fatalf("TUI saved unexpected config: %s", data)
	}
}

func TestConfigCheckAndMultiRootWorkspace(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	project := filepath.Join(dir, "project")
	writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "healthy", "toolchain.json"), "healthy")

	writeExtensionFile(t, filepath.Join(project, "firmware", "build.go"), `package main
import (
	"os"
	"path/filepath"
	"github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("board") })
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("ui").SetType(api.OptionChoice).SetValues("widgets", "cafe").SetDefault("widgets")
	})
	p.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		if ctx.String("ui") == "cafe" {
			ctx.SetDefault("board", "accel", true)
		}
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ui := ctx.String("ui")
		ctx.Target("fw").SetKind(api.TargetVoid).AddDeps("board:board").SetBuildFunc(func(pkg *api.Package) error {
			return os.WriteFile(filepath.Join(pkg.SourceDir(), "..", "built-entry"), []byte("firmware:"+ui), 0644)
		})
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "board", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("accel").SetType(api.OptionBool).SetDefault(false)
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("board").SetKind(api.TargetVoid)
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "tests", "target-app", "build.go"), `package main
import (
	"os"
	"path/filepath"
	"github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
	p.SetName("target-app")
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("board") })
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("suite").SetType(api.OptionChoice).SetValues("gfx", "sd").SetDefault("gfx")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		suite := ctx.String("suite")
		ctx.Target("fw").SetKind(api.TargetVoid).AddDeps("board:board").SetBuildFunc(func(pkg *api.Package) error {
			return os.WriteFile(filepath.Join(pkg.SourceDir(), "..", "..", "built-entry"), []byte("target-app:"+suite), 0644)
		})
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, ".vmake", "project.json"), `{"default_config":"configs/sku-cafe.json"}`)
	writeExtensionFile(t, filepath.Join(project, "configs", "sku-cafe.json"), `{
  "version": "1",
  "description": "Cafe SKU",
  "global": {"toolchain": "healthy"},
  "entries": {"firmware": {"options": {"ui": "cafe"}}}
}`)
	writeExtensionFile(t, filepath.Join(project, "configs", "test", "gfx.json"), `{
  "version": "1",
  "description": "Graphics target test",
  "root": "target-app",
  "global": {"toolchain": "healthy"},
  "entries": {"target-app": {"options": {"suite": "sd"}}}
}`)

	out, err := extensionCommand(t, state, project, "config", "check", "--all")
	if err != nil || !strings.Contains(out, "OK   configs/sku-cafe.json") || !strings.Contains(out, "OK   configs/test/gfx.json") {
		t.Fatalf("config check --all failed: %v\n%s", err, out)
	}

	if out, err := extensionCommand(t, state, project, "build"); err != nil {
		t.Fatalf("default build failed: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(filepath.Join(project, "built-entry")); string(got) != "firmware:cafe" {
		t.Fatalf("default build entry = %q, want firmware:cafe", got)
	}

	if out, err := extensionCommand(t, state, project, "build", "-c", "test/gfx"); err != nil {
		t.Fatalf("target-app build failed: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(filepath.Join(project, "built-entry")); string(got) != "target-app:sd" {
		t.Fatalf("target-app build entry = %q, want target-app:sd", got)
	}

	writeExtensionFile(t, filepath.Join(project, "configs", "bad-stale.json"), `{
  "version": "1",
  "global": {"toolchain": "healthy"},
  "entries": {"firmware": {"options": {"nonexistent_opt": true}}, "target-app": {"options": {"suite": "gfx"}}}
}`)
	badOut, badErr := extensionCommand(t, state, project, "config", "check", "--all")
	if badErr == nil || !strings.Contains(badOut, "FAIL configs/bad-stale.json") || !strings.Contains(badOut, "nonexistent_opt") || !strings.Contains(badOut, "not reachable") {
		t.Fatalf("expected config check --all to catch stale option and unreachable entry: %v\n%s", badErr, badOut)
	}
}
