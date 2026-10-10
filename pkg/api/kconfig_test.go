package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKConfigConfigName(t *testing.T) {
	p := NewPackage()
	if got := p.kconfigConfigName(); got != ".config" {
		t.Fatalf("default = %q", got)
	}
	p.AddKConfig("fw").SetConfigPath("my.config")
	if got := p.kconfigConfigName(); got != "my.config" {
		t.Fatalf("custom = %q", got)
	}
}

func TestEnsureConfigUsesDefaultConfigPath(t *testing.T) {
	p := NewPackage().SetName("fw")
	p.AddKConfig("fw")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".config"), []byte("CONFIG_A=y\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if p.EnsureConfig(dir) {
		t.Fatal("existing .config should be reused")
	}
}

func TestEnsureConfigUsesCustomConfigPath(t *testing.T) {
	p := NewPackage().SetName("fw")
	p.AddKConfig("fw").SetConfigPath("my.config")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "my.config"), []byte("CONFIG_A=y\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if p.EnsureConfig(dir) {
		t.Fatal("existing custom config should be reused")
	}
}
