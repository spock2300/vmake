package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	_ = os.WriteFile(path, []byte("{invalid json"), 0644)

	_, err := Load(path)
	if err == nil {
		t.Error("corrupt JSON should produce error")
	}
}

func TestLoadMissingParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subdir", "config.json")
	cfg, err := Load(path)
	if err != nil {
		t.Errorf("missing parent dir is treated as file-not-exist (returns empty cfg): %v", err)
	}
	if cfg == nil {
		t.Error("cfg should be non-nil (newConfigFile)")
	}
}

func TestGetEntryExistingNilOptions(t *testing.T) {
	cfg := &ConfigFile{
		Entries: map[string]*EntryConfig{
			"pkg": {SelectedPreset: "defconfig"},
		},
	}
	entry := GetEntry(cfg, "pkg")
	if entry.SelectedPreset != "defconfig" {
		t.Errorf("SelectedPreset = %q", entry.SelectedPreset)
	}
	if entry.Options == nil {
		t.Error("Options should be initialized")
	}
}

func TestSetEntryNilMap(t *testing.T) {
	cfg := &ConfigFile{Version: "1"}
	SetEntry(cfg, "x", &EntryConfig{SelectedPreset: "board"})
	if cfg.Entries == nil {
		t.Fatal("Entries should be initialized")
	}
	if cfg.Entries["x"].SelectedPreset != "board" {
		t.Errorf("got preset %q", cfg.Entries["x"].SelectedPreset)
	}
}

func TestBuildGlobalValuesNilGlobal(t *testing.T) {
	cfg := &ConfigFile{Version: "1"}
	vals := BuildGlobalValues(cfg)
	if len(vals) != 0 {
		t.Errorf("nil Global should produce empty vals, got %v", vals)
	}
}

func TestBuildGlobalValuesEmptyStrings(t *testing.T) {
	cfg := &ConfigFile{
		Global: &GlobalConfig{},
	}
	vals := BuildGlobalValues(cfg)
	if _, exists := vals["toolchain"]; exists {
		t.Error("empty toolchain should not appear in vals")
	}
	if _, exists := vals["mode"]; exists {
		t.Error("empty mode should not appear in vals")
	}
}

func TestUnmarshalJSONEntryParseFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{
		"version": "1",
		"global": {"toolchain": "gcc"},
		"entries": {
			"good": {"selected_preset": "def"},
			"bad": "not-an-object"
		}
	}`
	_ = os.WriteFile(path, []byte(data), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, exists := cfg.Entries["good"]; !exists {
		t.Error("good entry should be loaded")
	}
	if _, exists := cfg.Entries["bad"]; exists {
		t.Error("bad entry should be silently skipped (per store.go:66)")
	}
}

func TestUnmarshalJSONGlobalOptionsNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{
		"version": "1",
		"global": {"toolchain": "gcc"}
	}`
	_ = os.WriteFile(path, []byte(data), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Global.Options == nil {
		t.Error("Global.Options should be initialized to non-nil even if absent in JSON")
	}
}

func TestSaveProducesValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := newConfigFile()
	cfg.Global.Toolchain = "gcc"
	cfg.Global.Mode = "release"
	cfg.Global.Options["custom"] = "val"
	SetEntry(cfg, "pkg", &EntryConfig{
		Options:        map[string]any{"opt": true},
		KConfig:        "CONFIG_X=y",
		SelectedPreset: "defconfig",
	})

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Global.Toolchain != "gcc" {
		t.Errorf("Toolchain = %q", loaded.Global.Toolchain)
	}
	entry := GetEntry(loaded, "pkg")
	if entry.KConfig != "CONFIG_X=y" {
		t.Errorf("KConfig = %q", entry.KConfig)
	}
	if entry.SelectedPreset != "defconfig" {
		t.Errorf("SelectedPreset = %q", entry.SelectedPreset)
	}
	if !reflect.DeepEqual(entry.Options, map[string]any{"opt": true}) {
		t.Errorf("Options = %v", entry.Options)
	}
}

func TestRoundTripMultipleEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := newConfigFile()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		SetEntry(original, name, &EntryConfig{SelectedPreset: name + "_preset"})
	}
	if err := Save(path, original); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Entries) != 3 {
		t.Errorf("Entries count = %d, want 3", len(loaded.Entries))
	}
}
