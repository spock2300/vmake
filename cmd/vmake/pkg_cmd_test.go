package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListPackageSources(t *testing.T) {
	dir := t.TempDir()

	var empty bytes.Buffer
	listPackageSources(&empty, dir)
	if got := empty.String(); got != "No package sources\n" {
		t.Fatalf("empty output = %q", got)
	}

	if err := os.MkdirAll(filepath.Join(dir, "official", "zlib"), 0755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	listPackageSources(&out, dir)
	got := out.String()
	for _, want := range []string{"Package sources:", "official/zlib"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output %q does not contain %q", got, want)
		}
	}
}
