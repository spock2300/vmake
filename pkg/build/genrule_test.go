package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spock2300/vmake/pkg/api"
)

func TestRunGenRulesRegeneratesWhenInputIdentityChanges(t *testing.T) {
	workDir := t.TempDir()
	generatedDir := filepath.Join(workDir, "build", "generated")
	old := time.Unix(0, 0)

	writeInput := func(rel string, data []byte) {
		t.Helper()
		path := filepath.Join(workDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	writeInput("data1/blob.bin", []byte{0x11, 0x22})
	writeInput("data2/blob.bin", []byte{0x33, 0x44})

	headerPath := filepath.Join(generatedDir, "blob.h")
	readHeader := func() string {
		t.Helper()
		data, err := os.ReadFile(headerPath)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	first := api.NewTargetRegistry().Target("app").AddBinHeader("data1/blob.bin").GenRules()
	if err := runGenRules(first, generatedDir, workDir); err != nil {
		t.Fatal(err)
	}
	if header := readHeader(); !strings.Contains(header, "0x11") || strings.Contains(header, "0x33") {
		t.Fatalf("first header = %q, want data1 bytes", header)
	}
	if !genRuleMatchesInput(headerPath, "data1/blob.bin") || genRuleMatchesInput(headerPath, "data2/blob.bin") {
		t.Fatal("generated header does not record the data1 input")
	}

	second := api.NewTargetRegistry().Target("app").AddBinHeader("data2/blob.bin").GenRules()
	if err := runGenRules(second, generatedDir, workDir); err != nil {
		t.Fatal(err)
	}
	if header := readHeader(); !strings.Contains(header, "0x33") || strings.Contains(header, "0x11") {
		t.Fatalf("header after input switch = %q, want data2 bytes", header)
	}
	if !genRuleMatchesInput(headerPath, "data2/blob.bin") || genRuleMatchesInput(headerPath, "data1/blob.bin") {
		t.Fatal("generated header does not record the switched input")
	}

	stamp, err := os.Stat(headerPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := runGenRules(second, generatedDir, workDir); err != nil {
		t.Fatal(err)
	}
	again, err := os.Stat(headerPath)
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(stamp.ModTime()) || again.Size() != stamp.Size() {
		t.Fatal("unchanged gen rule rewrote the header")
	}
}

func TestRunGenRulesWritesOnlyHeaders(t *testing.T) {
	workDir := t.TempDir()
	generatedDir := filepath.Join(workDir, "generated")
	if err := os.WriteFile(filepath.Join(workDir, "blob.bin"), []byte{0x01}, 0644); err != nil {
		t.Fatal(err)
	}
	rules := api.NewTargetRegistry().Target("app").AddBinHeader("blob.bin").GenRules()
	if err := runGenRules(rules, generatedDir, workDir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(generatedDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "blob.h" {
		t.Fatalf("generated dir contains extra files: %v", entries)
	}
	if !genRuleMatchesInput(filepath.Join(generatedDir, "blob.h"), "blob.bin") {
		t.Fatal("generated header is missing the input marker")
	}
}
