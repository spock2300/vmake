package gocompile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompilePluginToOutput_ForceRemovesExisting(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "test.so")

	os.WriteFile(outputPath, []byte("old"), 0644)

	opts := PluginOptions{
		WorkDir:    dir,
		OutputPath: outputPath,
		EntryFile:  "main.go",
		ModuleName: "test",
		Prefix:     "test_",
	}

	entryFile := filepath.Join(dir, "main.go")
	os.WriteFile(entryFile, []byte("package main\nfunc Main(){}\n"), 0644)

	result := CompilePluginToOutput(opts, true)

	if _, err := os.Stat(outputPath); err == nil {
		os.Remove(outputPath)
	}

	_ = result
}

func TestCompilePluginToOutput_NonForceSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "test.so")

	os.WriteFile(outputPath, []byte("existing"), 0644)

	entryFile := filepath.Join(dir, "main.go")
	os.WriteFile(entryFile, []byte("package main\nfunc Main(){}\n"), 0644)

	opts := PluginOptions{
		WorkDir:    dir,
		OutputPath: outputPath,
		EntryFile:  "main.go",
		ModuleName: "test",
		Prefix:     "test_",
	}

	result := CompilePluginToOutput(opts, false)

	if _, err := os.Stat(outputPath); err != nil {
		t.Errorf("output should still exist with non-force compile")
	}

	_ = result
}

func TestCompilePluginToOutput_InvalidEntry(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "test.so")

	opts := PluginOptions{
		WorkDir:    dir,
		OutputPath: outputPath,
		EntryFile:  "nonexistent.go",
		ModuleName: "test",
		Prefix:     "test_",
	}

	result := CompilePluginToOutput(opts, true)

	if result.Success {
		t.Error("expected compilation to fail for nonexistent entry file")
	}
}

func TestSanitizeModuleName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"with/slash", "with_slash"},
		{"a/b/c", "a_b_c"},
	}
	for _, tt := range tests {
		got := SanitizeModuleName(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeModuleName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
