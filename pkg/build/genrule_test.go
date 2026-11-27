package build

import (
	"bytes"
	"context"
	"errors"
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

	second := api.NewTargetRegistry().Target("app").AddBinHeader("data2/blob.bin").GenRules()
	if err := runGenRules(second, generatedDir, workDir); err != nil {
		t.Fatal(err)
	}
	if header := readHeader(); !strings.Contains(header, "0x33") || strings.Contains(header, "0x11") {
		t.Fatalf("header after input switch = %q, want data2 bytes", header)
	}

	writeInput("data2/blob.bin", []byte{0x55, 0x66})
	if err := runGenRules(second, generatedDir, workDir); err != nil {
		t.Fatal(err)
	}
	if header := readHeader(); !strings.Contains(header, "0x55,0x66") {
		t.Fatalf("preserved input timestamp hid changed bytes: %s", header)
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
}

type cancelOnGeneration struct {
	context.Context
	cancel context.CancelFunc
	polls  int
}

func (c *cancelOnGeneration) Err() error {
	c.polls++
	if c.polls == 4 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestBinHeaderCanceledGenerationKeepsPreviousHeader(t *testing.T) {
	dir := t.TempDir()
	generated := filepath.Join(dir, "generated")
	input := filepath.Join(dir, "asset.bin")
	header := filepath.Join(generated, "asset.h")
	rules := api.NewTargetRegistry().Target("assets").AddBinHeader("asset.bin").GenRules()
	if err := os.WriteFile(input, []byte{1}, 0644); err != nil {
		t.Fatal(err)
	}
	if err := runGenRules(rules, generated, dir); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(header)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, make([]byte, 512), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := runGenRulesContext(&cancelOnGeneration{Context: ctx, cancel: cancel}, rules, generated, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled generation, got %v", err)
	}
	published, err := os.ReadFile(header)
	if err != nil || !bytes.Equal(original, published) {
		t.Fatalf("canceled generation replaced the published header: %v", err)
	}
	if err := runGenRules(rules, generated, dir); err != nil {
		t.Fatal(err)
	}
	final, err := os.ReadFile(header)
	if err != nil || bytes.Count(final, []byte("0x00")) != 512 {
		t.Fatalf("retry did not embed the updated input: %v", err)
	}

	if err := os.Chmod(header, 0444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte{0x7f}, 0644); err != nil {
		t.Fatal(err)
	}
	if err := runGenRules(rules, generated, dir); err != nil {
		t.Fatalf("regenerate over read-only header: %v", err)
	}
	updated, err := os.ReadFile(header)
	if err != nil || !bytes.Contains(updated, []byte("0x7f")) {
		t.Fatalf("read-only header was not replaced: %v", err)
	}
}
