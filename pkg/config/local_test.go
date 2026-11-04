package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLocalMissingReturnsEmpty(t *testing.T) {
	cfg, err := LoadLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != LocalVersion || len(cfg.Links) != 0 {
		t.Fatalf("unexpected local config: %+v", cfg)
	}
}

func TestSaveLocalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := &LocalConfig{Version: LocalVersion, Links: map[string]string{
		"native/sdk": "/tmp/sdk",
	}}
	if err := SaveLocal(dir, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := LoadLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Links["native/sdk"] != "/tmp/sdk" {
		t.Fatalf("links = %v", got.Links)
	}
	if _, err := os.Stat(LocalPath(dir)); err != nil {
		t.Fatal(err)
	}
}

func TestLoadLocalCorruptFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".vmake"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LocalPath(dir), []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLocal(dir); err == nil {
		t.Fatal("corrupt local.json should fail")
	}
}
