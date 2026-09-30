package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeProjectFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, ".vmake", name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertProjectFile(t *testing.T, root, name, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".vmake", name))
	if err != nil || string(data) != want {
		t.Fatalf("%s = %q, %v; want %q", name, data, err, want)
	}
}

func TestProjectConfigLegacyAndCopy(t *testing.T) {
	root := t.TempDir()
	cfg, path, err := LoadProject(root)
	if err != nil || path != filepath.Join(root, ".vmake", DefaultFilename) || !reflect.DeepEqual(cfg, newConfigFile()) {
		t.Fatalf("legacy empty config: %+v, %s, %v", cfg, path, err)
	}
	infos, err := ListProjectConfigs(root)
	if err != nil || len(infos) != 1 || !infos[0].Active || !infos[0].Unsaved || infos[0].Error != nil {
		t.Fatalf("unsaved config: %+v, %v", infos, err)
	}
	if err := CopyProjectConfig(root, "empty.json"); err != nil {
		t.Fatal(err)
	}
	copied, err := Load(filepath.Join(root, ".vmake", "empty.json"))
	if err != nil || !reflect.DeepEqual(copied, cfg) {
		t.Fatalf("empty copy: %+v, %v", copied, err)
	}
	for _, name := range []string{ProjectFilename, DefaultFilename} {
		if _, err := os.Stat(filepath.Join(root, ".vmake", name)); !os.IsNotExist(err) {
			t.Fatalf("copy created %s: %v", name, err)
		}
	}
	content := "{\n  \"version\": \"1\", \"global\": {\"mode\":\"debug\"},\n  \"entries\": {\"app\": {\"version\":\"v1\",\"options\":{\"n\":2},\"kconfig\":\"CONFIG_X=y\\n\",\"selected_preset\":\"board\"}},\n  \"extension\": {\"keep\":true}\n}\n"
	writeProjectFile(t, root, DefaultFilename, content)
	if err := CopyProjectConfig(root, "board.json"); err != nil {
		t.Fatal(err)
	}
	assertProjectFile(t, root, "board.json", content)
	if err := CopyProjectConfig(root, "board.json"); err == nil {
		t.Fatal("overwrote an existing config")
	}
	assertProjectFile(t, root, "board.json", content)
	if _, err := UseProjectConfig(root, "board.json"); err != nil {
		t.Fatal(err)
	}
	cfg, path, err = LoadProject(root)
	if err != nil || filepath.Base(path) != "board.json" || cfg.Global.Mode != "debug" || cfg.Entries["app"].KConfig != "CONFIG_X=y\n" {
		t.Fatalf("selected config: %+v, %s, %v", cfg, path, err)
	}
	if err := CopyProjectConfig(root, "board-copy.json"); err != nil {
		t.Fatal(err)
	}
	assertProjectFile(t, root, "board-copy.json", content)
	_, path, err = LoadProject(root)
	if err != nil || filepath.Base(path) != "board.json" {
		t.Fatalf("copy changed selection: %s, %v", path, err)
	}
}

func TestProjectSelectionErrors(t *testing.T) {
	for _, selection := range []string{"{", "null", "{}", `{"config":null}`, `{"config":12}`, `{"config":""}`, `{"config":"../outside.json"}`, `{"config":"project.json"}`} {
		t.Run(selection, func(t *testing.T) {
			root := t.TempDir()
			writeProjectFile(t, root, DefaultFilename, `{"version":"1","entries":{}}`)
			writeProjectFile(t, root, ProjectFilename, selection)
			if _, _, err := LoadProject(root); err == nil || !strings.Contains(err.Error(), ProjectFilename) {
				t.Fatalf("invalid selection silently accepted: %v", err)
			}
			if _, err := UseProjectConfig(root, DefaultFilename); err == nil {
				t.Fatal("overwrote a damaged project selection")
			}
			if err := CopyProjectConfig(root, "copy.json"); err == nil {
				t.Fatal("copied with invalid selection")
			}
			assertProjectFile(t, root, ProjectFilename, selection)
		})
	}
}

func TestProjectConfigMissingInvalidAndRecovery(t *testing.T) {
	root := t.TempDir()
	selection := `{"config":"gone.json"}`
	writeProjectFile(t, root, ProjectFilename, selection)
	writeProjectFile(t, root, DefaultFilename, `{"version":"1","entries":{}}`)
	writeProjectFile(t, root, "broken.json", "{")
	if _, _, err := LoadProject(root); err == nil || !strings.Contains(err.Error(), "gone.json") {
		t.Fatalf("missing config fell back: %v", err)
	}
	if err := CopyProjectConfig(root, "copy.json"); err == nil {
		t.Fatal("copied missing explicit config")
	}
	for _, target := range []string{"gone.json", "broken.json"} {
		if _, err := UseProjectConfig(root, target); err == nil {
			t.Fatalf("selected invalid config %s", target)
		}
		assertProjectFile(t, root, ProjectFilename, selection)
	}
	infos, err := ListProjectConfigs(root)
	if err != nil || len(infos) != 3 || infos[0].Name != "broken.json" || infos[0].Error == nil || infos[2].Name != "gone.json" || !infos[2].Active || infos[2].Error == nil {
		t.Fatalf("invalid file listing: %+v, %v", infos, err)
	}
	if _, err := UseProjectConfig(root, DefaultFilename); err != nil {
		t.Fatalf("cannot recover missing selected file: %v", err)
	}
	writeProjectFile(t, root, DefaultFilename, "{")
	if _, _, err := LoadProject(root); err == nil {
		t.Fatal("invalid selected JSON accepted")
	}
	if err := os.Remove(filepath.Join(root, ".vmake", DefaultFilename)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProject(root); err == nil {
		t.Fatal("explicit default silently created")
	}
}

func TestProjectConfigFilenames(t *testing.T) {
	for _, name := range []string{"", ".json", "config", "config.JSON", "project.json", "PROJECT.json", "../a.json", "sub/a.json", `sub\a.json`, "/tmp/a.json", `C:\a.json`, "C:a.json", "a\x00.json", "a\n.json", "a*.json"} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateFilename(name); err == nil {
				t.Fatalf("accepted %q", name)
			}
			root := t.TempDir()
			if _, err := UseProjectConfig(root, name); err == nil {
				t.Fatal("selected invalid filename")
			}
			if err := CopyProjectConfig(root, name); err == nil {
				t.Fatal("copied to invalid filename")
			}
		})
	}
	for _, name := range []string{"config.json", "board-debug.json", "板型 A.json"} {
		if err := ValidateFilename(name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProjectConfigExplicitErrorNamesSelection(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, ProjectFilename, `{"config":"gone.json"}`)
	_, _, err := LoadProject(root)
	if err == nil || !strings.Contains(err.Error(), ProjectFilename) || !strings.Contains(err.Error(), "gone.json") {
		t.Fatalf("missing explicit config error does not name the selection file: %v", err)
	}
	writeProjectFile(t, root, "broken.json", "{")
	writeProjectFile(t, root, ProjectFilename, `{"config":"broken.json"}`)
	if _, _, err := LoadProject(root); err == nil || !strings.Contains(err.Error(), ProjectFilename) {
		t.Fatalf("invalid explicit config error does not name the selection file: %v", err)
	}
}

func TestProjectConfigVariantNameResolution(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".vmake")
	writeProjectFile(t, root, DefaultFilename, `{"version":"1","entries":{}}`)
	if err := os.Symlink(filepath.Join(dir, DefaultFilename), filepath.Join(dir, "Config.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got := matchingConfigName(dir, "Config.json"); got != DefaultFilename {
		t.Fatalf("matchingConfigName variant spelling = %q, want %q", got, DefaultFilename)
	}
	infos, err := ListProjectConfigs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 || infos[0].Name != "Config.json" || infos[1].Name != DefaultFilename || !infos[1].Active || infos[0].Active {
		t.Fatalf("variant spelling listed twice or mislabeled: %+v", infos)
	}
	name, err := UseProjectConfig(root, "Config.json")
	if err != nil {
		t.Fatal(err)
	}
	if name != DefaultFilename {
		t.Fatalf("UseProjectConfig stored variant spelling %q, want %q", name, DefaultFilename)
	}
	_, path, err := LoadProject(root)
	if err != nil || filepath.Base(path) != DefaultFilename {
		t.Fatalf("selection stored as %s, %v; want %s", path, err, DefaultFilename)
	}
}

func TestProjectConfigDistinctCaseVariantsKeepSelection(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".vmake")
	writeProjectFile(t, root, DefaultFilename, `{"version":"1","entries":{}}`)
	writeProjectFile(t, root, "Config.json", `{"version":"1","entries":{}}`)
	first, firstErr := os.Stat(filepath.Join(dir, DefaultFilename))
	second, secondErr := os.Stat(filepath.Join(dir, "Config.json"))
	if firstErr != nil || secondErr != nil {
		t.Fatal(firstErr, secondErr)
	}
	if os.SameFile(first, second) {
		t.Skip("case-insensitive filesystem; spellings cannot be distinct files")
	}
	if got := matchingConfigName(dir, DefaultFilename); got != "" {
		t.Fatalf("matchingConfigName merged distinct files into %q", got)
	}
	name, err := UseProjectConfig(root, "Config.json")
	if err != nil || name != "Config.json" {
		t.Fatalf("UseProjectConfig = %q, %v; want Config.json", name, err)
	}
	_, path, err := LoadProject(root)
	if err != nil || filepath.Base(path) != "Config.json" {
		t.Fatalf("selection = %s, %v; want Config.json", path, err)
	}
}

func TestProjectCopyDoesNotFollowDestinationSymlink(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, DefaultFilename, `{"version":"1","entries":{}}`)
	target := filepath.Join(root, "outside.json")
	if err := os.WriteFile(target, []byte("preserve"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, ".vmake", "link.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := CopyProjectConfig(root, "link.json"); err == nil {
		t.Fatal("copied over symlink")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("changed symlink target: %q, %v", data, err)
	}
}

func TestProjectConfigDescription(t *testing.T) {
	root := t.TempDir()
	path, err := SetProjectDescription(root, "  Board A 调试配置  ")
	if err != nil || path != filepath.Join(root, ".vmake", DefaultFilename) {
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
	if err := CopyProjectConfig(root, "board.json"); err != nil {
		t.Fatal(err)
	}
	copied, err := Load(filepath.Join(root, ".vmake", "board.json"))
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
	writeProjectFile(t, root, DefaultFilename, original)
	writeProjectFile(t, root, ProjectFilename, `{"config":"config.json"}`)
	for _, bad := range []string{"two\nlines", "tab\there", strings.Repeat("a", MaxDescriptionLength+1)} {
		if _, err := SetProjectDescription(root, bad); err == nil {
			t.Fatalf("accepted invalid description %q", bad)
		}
		assertProjectFile(t, root, DefaultFilename, original)
	}
	if _, err := SetProjectDescription(root, strings.Repeat("a", MaxDescriptionLength)); err != nil {
		t.Fatalf("rejected max-length description: %v", err)
	}
	writeProjectFile(t, root, ProjectFilename, `{"config":"missing.json"}`)
	if _, err := SetProjectDescription(root, "text"); err == nil || !strings.Contains(err.Error(), ProjectFilename) {
		t.Fatalf("wrote description for missing explicit selection: %v", err)
	}
}
