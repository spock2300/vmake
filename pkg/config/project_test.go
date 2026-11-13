package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertConfigFile(t *testing.T, root, rel, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil || string(data) != want {
		t.Fatalf("%s = %q, %v; want %q", rel, data, err, want)
	}
}

func TestProjectConfigDefaultCopyAndUse(t *testing.T) {
	root := t.TempDir()
	cfg, path, err := LoadProject(root)
	if err != nil || path != filepath.Join(root, "configs", "default.json") || !reflect.DeepEqual(cfg, newConfigFile()) {
		t.Fatalf("empty default config: %+v, %s, %v", cfg, path, err)
	}
	infos, err := ListProjectConfigs(root)
	if err != nil || len(infos) != 1 || infos[0].Name != "configs/default.json" || !infos[0].Active || !infos[0].Unsaved || infos[0].Error != nil {
		t.Fatalf("unsaved config: %+v, %v", infos, err)
	}
	if err := CopyProjectConfig(root, "empty"); err != nil {
		t.Fatal(err)
	}
	copied, err := Load(filepath.Join(root, "configs", "empty.json"))
	if err != nil || copied.Version != ConfigVersion || len(copied.Entries) != 0 {
		t.Fatalf("empty copy: %+v, %v", copied, err)
	}
	for _, rel := range []string{".vmake/project.json", ".vmake/local.json", "configs/default.json"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("copy created %s: %v", rel, err)
		}
	}

	content := "{\n  \"version\": \"1\",\n  \"global\": {\"mode\":\"debug\"},\n  \"entries\": {\"app\": {\"options\":{\"n\":2},\"kconfig\":\"CONFIG_X=y\\n\",\"selected_preset\":\"board\"}}\n}\n"
	writeConfigFile(t, root, "configs/default.json", content)
	if err := CopyProjectConfig(root, "board"); err != nil {
		t.Fatal(err)
	}
	assertConfigFile(t, root, "configs/board.json", content)
	if err := CopyProjectConfig(root, "board"); err == nil {
		t.Fatal("overwrote an existing config")
	}
	assertConfigFile(t, root, "configs/board.json", content)

	sel, err := UseProjectConfig(root, "board")
	if err != nil || sel != "configs/board.json" {
		t.Fatalf("UseProjectConfig: %q, %v", sel, err)
	}
	local, err := LoadLocal(root)
	if err != nil || local.Config != "configs/board.json" {
		t.Fatalf("local selection: %+v, %v", local, err)
	}
	cfg, path, err = LoadProject(root)
	if err != nil || path != filepath.Join(root, "configs", "board.json") || cfg.Global.Mode != "debug" || cfg.Entries["app"].KConfig != "CONFIG_X=y\n" {
		t.Fatalf("selected config: %+v, %s, %v", cfg, path, err)
	}
	if err := CopyProjectConfig(root, "test/sub"); err != nil {
		t.Fatal(err)
	}
	assertConfigFile(t, root, "configs/test/sub.json", content)
	_, path, err = LoadProject(root)
	if err != nil || filepath.Base(path) != "board.json" {
		t.Fatalf("copy changed selection: %s, %v", path, err)
	}

	subCfg, subPath, err := LoadProjectWithOverride(root, "test/sub")
	if err != nil || subPath != filepath.Join(root, "configs", "test", "sub.json") || subCfg.Global.Mode != "debug" {
		t.Fatalf("LoadProjectWithOverride: %+v, %s, %v", subCfg, subPath, err)
	}
}

func TestProjectSelectionErrors(t *testing.T) {
	for _, selection := range []string{
		"{",
		"null",
		"{}",
		`{"unknown_field":1}`,
		`{"default_config":null}`,
		`{"default_config":12}`,
		`{"default_config":""}`,
		`{"default_config":"../outside.json"}`,
		`{"default_config":"configs/default.json","pins":"bad"}`,
		`{"default_config":"configs/default.json","pins":{"pkg":""}}`,
		`{"default_config":"configs/default.json","pins":{"":"1.0.0"}}`,
		`{"default_config":"configs/default.json","pins":{"pkg":123}}`,
	} {
		t.Run(selection, func(t *testing.T) {
			root := t.TempDir()
			writeConfigFile(t, root, "configs/default.json", `{"version":"1","entries":{}}`)
			writeConfigFile(t, root, ".vmake/project.json", selection)
			if _, _, err := LoadProject(root); err == nil || !strings.Contains(err.Error(), ProjectFilename) {
				t.Fatalf("invalid selection silently accepted: %v", err)
			}
			if _, err := UseProjectConfig(root, "default"); err == nil {
				t.Fatal("accepted damaged project metadata")
			}
			if err := CopyProjectConfig(root, "copy"); err == nil {
				t.Fatal("copied with invalid project metadata")
			}
			assertConfigFile(t, root, ".vmake/project.json", selection)
		})
	}
}

func TestProjectConfigMissingInvalidAndRecovery(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, ".vmake/project.json", `{"default_config":"configs/gone.json"}`)
	writeConfigFile(t, root, "configs/default.json", `{"version":"1","entries":{}}`)
	writeConfigFile(t, root, "configs/broken.json", "{")
	if _, _, err := LoadProject(root); err == nil || !strings.Contains(err.Error(), "gone.json") {
		t.Fatalf("missing default_config fell back: %v", err)
	}
	if err := CopyProjectConfig(root, "copy"); err == nil {
		t.Fatal("copied missing explicit config")
	}
	for _, target := range []string{"gone", "broken"} {
		if _, err := UseProjectConfig(root, target); err == nil {
			t.Fatalf("selected invalid config %s", target)
		}
	}
	infos, err := ListProjectConfigs(root)
	if err != nil || len(infos) != 3 || infos[0].Name != "configs/broken.json" || infos[0].Error == nil || infos[2].Name != "configs/gone.json" || !infos[2].Active || infos[2].Error == nil {
		t.Fatalf("invalid file listing: %+v, %v", infos, err)
	}
	if _, err := UseProjectConfig(root, "default"); err != nil {
		t.Fatalf("cannot recover missing default_config via local selection: %v", err)
	}
	writeConfigFile(t, root, "configs/default.json", "{")
	if _, _, err := LoadProject(root); err == nil {
		t.Fatal("invalid selected JSON accepted")
	}
	if err := os.Remove(filepath.Join(root, "configs", "default.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProject(root); err == nil {
		t.Fatal("explicit local selection silently created")
	}
}

func TestProjectConfigReferences(t *testing.T) {
	for _, ref := range []string{"", ".json", "../a.json", "configs/../a.json", ".hidden", "configs/.hidden.json", `sub\a.json`, "/tmp/a.json", `C:\a.json`, "C:a.json", "a\x00.json", "a\n.json", "a*.json"} {
		t.Run(ref, func(t *testing.T) {
			if _, err := NormalizeConfigReference(ref); err == nil {
				t.Fatalf("accepted %q", ref)
			}
			root := t.TempDir()
			if _, err := UseProjectConfig(root, ref); err == nil {
				t.Fatal("selected invalid ref")
			}
			if err := CopyProjectConfig(root, ref); err == nil {
				t.Fatal("copied to invalid ref")
			}
		})
	}
	for ref, want := range map[string]string{
		"default":                 "configs/default.json",
		"evb-widgets":             "configs/evb-widgets.json",
		"configs/evb-widgets.json": "configs/evb-widgets.json",
		"test/minimal":            "configs/test/minimal.json",
		"./configs/板型 A.json":     "configs/板型 A.json",
	} {
		got, err := NormalizeConfigReference(ref)
		if err != nil || got != want {
			t.Fatalf("NormalizeConfigReference(%q) = %q, %v; want %q", ref, got, err, want)
		}
	}
}

func TestProjectConfigExplicitErrorNamesSelection(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, ".vmake/project.json", `{"default_config":"gone"}`)
	_, _, err := LoadProject(root)
	if err == nil || !strings.Contains(err.Error(), ProjectFilename) || !strings.Contains(err.Error(), "gone.json") {
		t.Fatalf("missing default_config error does not name the selection file: %v", err)
	}
	writeConfigFile(t, root, "configs/broken.json", "{")
	writeConfigFile(t, root, ".vmake/local.json", `{"version":"1","config":"configs/broken.json"}`)
	if _, _, err := LoadProject(root); err == nil || !strings.Contains(err.Error(), LocalFileName) {
		t.Fatalf("invalid local config error does not name local.json: %v", err)
	}
}

func TestProjectCopyDoesNotFollowDestinationSymlink(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, "configs/default.json", `{"version":"1","entries":{}}`)
	target := filepath.Join(root, "outside.json")
	if err := os.WriteFile(target, []byte("preserve"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "configs", "link.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := CopyProjectConfig(root, "link"); err == nil {
		t.Fatal("copied over symlink")
	}
	if _, _, err := LoadProjectWithOverride(root, "link"); err == nil {
		t.Fatal("read symlink escaping configs/")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("changed symlink target: %q, %v", data, err)
	}
}

func TestProjectConfigDescription(t *testing.T) {
	root := t.TempDir()
	path, err := SetProjectDescription(root, "  Board A 调试配置  ")
	if err != nil || path != filepath.Join(root, "configs", "default.json") {
		t.Fatalf("set description: %s, %v", path, err)
	}
	cfg, _, err := LoadProject(root)
	if err != nil || cfg.Description != "Board A 调试配置" {
		t.Fatalf("loaded description: %+v, %v", cfg, err)
	}
	infos, err := ListProjectConfigs(root)
	if err != nil || len(infos) != 1 || infos[0].Description != "Board A 调试配置" {
		t.Fatalf("listed description: %+v, %v", infos, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `"description": "Board A 调试配置"`) {
		t.Fatalf("saved description: %q, %v", data, err)
	}
	if err := CopyProjectConfig(root, "board"); err != nil {
		t.Fatal(err)
	}
	copied, err := Load(filepath.Join(root, "configs", "board.json"))
	if err != nil || copied.Description != "Board A 调试配置" {
		t.Fatalf("copied description: %+v, %v", copied, err)
	}
	if _, err := SetProjectDescription(root, ""); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = LoadProject(root)
	if err != nil || cfg.Description != "" {
		t.Fatalf("cleared description: %+v, %v", cfg, err)
	}
	data, err = os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "description") {
		t.Fatalf("cleared description persisted: %q, %v", data, err)
	}
}

func TestProjectDescriptionValidation(t *testing.T) {
	root := t.TempDir()
	original := `{"version":"1","entries":{}}`
	writeConfigFile(t, root, "configs/default.json", original)
	for _, bad := range []string{"tab\there", strings.Repeat("a", MaxDescriptionLength+1)} {
		if _, err := SetProjectDescription(root, bad); err == nil {
			t.Fatalf("accepted invalid description %q", bad)
		}
		assertConfigFile(t, root, "configs/default.json", original)
	}
	if _, err := SetProjectDescription(root, "first line\nsecond line"); err != nil {
		t.Fatalf("rejected multiline description: %v", err)
	}
	if _, err := SetProjectDescription(root, strings.Repeat("a", MaxDescriptionLength)); err != nil {
		t.Fatalf("rejected max-length description: %v", err)
	}
	writeConfigFile(t, root, ".vmake/project.json", `{"default_config":"missing"}`)
	if _, err := SetProjectDescription(root, "text"); err == nil || !strings.Contains(err.Error(), ProjectFilename) {
		t.Fatalf("wrote description for missing default_config: %v", err)
	}
}

func TestProjectPinsRoundTripAndDefaultConfigPreservation(t *testing.T) {
	root := t.TempDir()
	pins, err := LoadProjectPins(root)
	if err != nil || len(pins) != 0 {
		t.Fatalf("initial pins: %v, %v", pins, err)
	}
	if err := SaveProjectPins(root, map[string]string{"official/zlib": "1.3.1"}); err != nil {
		t.Fatalf("SaveProjectPins: %v", err)
	}
	pins, err = LoadProjectPins(root)
	if err != nil || !reflect.DeepEqual(pins, map[string]string{"official/zlib": "1.3.1"}) {
		t.Fatalf("loaded pins: %v, %v", pins, err)
	}
	cfg, path, err := LoadProject(root)
	if err != nil || filepath.Base(path) != "default.json" || cfg == nil {
		t.Fatalf("LoadProject with pins only: %+v, %s, %v", cfg, path, err)
	}

	writeConfigFile(t, root, ".vmake/project.json", `{"default_config":"configs/board.json","pins":{"official/zlib":"1.3.1"}}`)
	writeConfigFile(t, root, "configs/board.json", `{"version":"1","entries":{}}`)
	if err := SaveProjectPins(root, nil); err != nil {
		t.Fatalf("SaveProjectPins(nil): %v", err)
	}
	_, path, err = LoadProject(root)
	if err != nil || filepath.Base(path) != "board.json" {
		t.Fatalf("SaveProjectPins(nil) lost default_config: %s, %v", path, err)
	}
}

func TestProjectPinsRemovesFileWhenNoDefaultConfig(t *testing.T) {
	root := t.TempDir()
	if err := SaveProjectPins(root, map[string]string{"pkg": "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveProjectPins(root, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".vmake", ProjectFilename)); !os.IsNotExist(err) {
		t.Fatalf("expected project.json removed when pins empty and no default_config, got err=%v", err)
	}
}
