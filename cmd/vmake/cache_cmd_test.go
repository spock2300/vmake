package main

import (
	"os"
	"testing"
)

func TestConfirmCacheRemovalRejectsNonTTY(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if ok, err := confirmCacheRemoval("https://example.invalid/repo", f); ok || err == nil {
		t.Fatalf("non-TTY confirmation = %v, %v; want false, error", ok, err)
	}
}
