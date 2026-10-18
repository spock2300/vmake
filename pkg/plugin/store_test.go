package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func writePluginInfo(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPluginInfoValidatesNameAndSource(t *testing.T) {
	for _, tc := range []struct {
		name    string
		info    string
		wantErr bool
	}{
		{"valid entry", `{"name":"hello","entry":"src/main.go","enabled":true}`, false},
		{"valid sourceDir", `{"name":"hello","sourceDir":"src","enabled":true}`, false},
		{"empty name", `{"name":"","entry":"src/main.go"}`, true},
		{"traversal name", `{"name":"../evil","entry":"src/main.go"}`, true},
		{"traversal entry", `{"name":"hello","entry":"../main.go"}`, true},
		{"absolute entry", `{"name":"hello","entry":"/tmp/main.go"}`, true},
		{"missing source", `{"name":"hello"}`, true},
		{"future api", `{"name":"hello","entry":"src/main.go","apiVersion":2}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "plugin")
			writePluginInfo(t, dir, tc.info)
			_, err := LoadPluginInfo(dir)
			if tc.wantErr && err == nil {
				t.Fatal("LoadPluginInfo accepted invalid metadata")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("LoadPluginInfo: %v", err)
			}
		})
	}
}

func TestPluginSourceRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(dir, "plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(pluginDir, "src")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"hello","sourceDir":"src"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPluginInfo(pluginDir); err == nil {
		t.Fatal("LoadPluginInfo accepted a source directory symlink escape")
	}
}
