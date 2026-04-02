package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_NewFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	data := `{
		"version": "1",
		"global": {
			"toolchain": "clang",
			"mode": "release",
			"options": {"opt1": "val1"}
		},
		"entries": {
			"mypkg": {
				"version": "1.0.0",
				"options": {"key": "value"}
			}
		}
	}`
	os.WriteFile(path, []byte(data), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Version != "1" {
		t.Errorf("Version = %q, want %q", cfg.Version, "1")
	}
	if cfg.Global.Toolchain != "clang" {
		t.Errorf("Global.Toolchain = %q, want %q", cfg.Global.Toolchain, "clang")
	}
	if cfg.Global.Mode != "release" {
		t.Errorf("Global.Mode = %q, want %q", cfg.Global.Mode, "release")
	}
	if cfg.Global.Options["opt1"] != "val1" {
		t.Errorf("Global.Options[opt1] = %v, want %q", cfg.Global.Options["opt1"], "val1")
	}

	entry := GetEntry(cfg, "mypkg")
	if entry.Version != "1.0.0" {
		t.Errorf("mypkg.Version = %q, want %q", entry.Version, "1.0.0")
	}
	if entry.Options["key"] != "value" {
		t.Errorf("mypkg.Options[key] = %v, want %q", entry.Options["key"], "value")
	}
}

func TestLoad_OldFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	data := `{
		"version": "1",
		"toolchain": "gcc"
	}`
	os.WriteFile(path, []byte(data), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Global.Toolchain != "gcc" {
		t.Errorf("Global.Toolchain = %q, want %q", cfg.Global.Toolchain, "gcc")
	}
}

func TestLoad_NonExistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Version != ConfigVersion {
		t.Errorf("Version = %q, want %q", cfg.Version, ConfigVersion)
	}
	if cfg.Global == nil {
		t.Error("Global should not be nil")
	}
	if cfg.Entries == nil {
		t.Error("Entries should not be nil")
	}
}

func TestGetEntry_Missing(t *testing.T) {
	cfg := newConfigFile()
	entry := GetEntry(cfg, "nonexistent")

	if entry.Options == nil {
		t.Error("entry.Options should not be nil")
	}
}

func TestGetEntry_NilEntries(t *testing.T) {
	cfg := &ConfigFile{Version: "1"}
	entry := GetEntry(cfg, "test")

	if entry.Options == nil {
		t.Error("entry.Options should not be nil")
	}
}

func TestSetEntry(t *testing.T) {
	cfg := newConfigFile()
	entry := &EntryConfig{Version: "2.0", Options: map[string]any{"x": 1}}
	SetEntry(cfg, "pkg", entry)

	got := GetEntry(cfg, "pkg")
	if got.Version != "2.0" {
		t.Errorf("Version = %q, want %q", got.Version, "2.0")
	}
}

func TestBuildGlobalValues(t *testing.T) {
	cfg := &ConfigFile{
		Version: "1",
		Global: &GlobalConfig{
			Toolchain: "clang",
			Mode:      "debug",
			Options:   map[string]any{"extra": "val"},
		},
	}

	vals := BuildGlobalValues(cfg)
	if vals["toolchain"] != "clang" {
		t.Errorf("toolchain = %v, want clang", vals["toolchain"])
	}
	if vals["mode"] != "debug" {
		t.Errorf("mode = %v, want debug", vals["mode"])
	}
	if vals["extra"] != "val" {
		t.Errorf("extra = %v, want val", vals["extra"])
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	original := newConfigFile()
	original.Global.Toolchain = "gcc"
	original.Global.Mode = "release"
	SetEntry(original, "pkg1", &EntryConfig{Version: "1.0", Options: map[string]any{"flag": true}})

	if err := Save(path, original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Global.Toolchain != "gcc" {
		t.Errorf("Toolchain = %q, want %q", loaded.Global.Toolchain, "gcc")
	}
	if loaded.Global.Mode != "release" {
		t.Errorf("Mode = %q, want %q", loaded.Global.Mode, "release")
	}
}
