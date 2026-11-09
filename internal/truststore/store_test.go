package truststore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTrustVerifyRevoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := store.Verify("repo", "official", "")
	if err != nil || ok {
		t.Fatalf("empty store Verify = %v, %v", ok, err)
	}
	if err := store.Trust("repo", "official", "https://example.invalid/repo"); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Verify("repo", "official", "https://example.invalid/repo"); err != nil || !ok {
		t.Fatalf("Verify after Trust = %v, %v", ok, err)
	}
	if ok, err := store.Verify("repo", "official", ""); err != nil || !ok {
		t.Fatalf("Verify without URL = %v, %v", ok, err)
	}
	if ok, err := store.Verify("repo", "official", "https://evil.invalid/repo"); err != nil || ok {
		t.Fatalf("Verify with wrong URL = %v, %v", ok, err)
	}
	if err := store.Trust("repo", "localrepo", ""); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Verify("repo", "localrepo", ""); err != nil || !ok {
		t.Fatalf("Verify URL-less record with empty URL = %v, %v", ok, err)
	}
	if ok, err := store.Verify("repo", "localrepo", "https://evil.invalid/repo"); err != nil || ok {
		t.Fatalf("Verify URL-less record with non-empty URL = %v, %v", ok, err)
	}
	if err := store.Revoke("repo", "official"); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Verify("repo", "official", ""); err != nil || ok {
		t.Fatalf("Verify after Revoke = %v, %v", ok, err)
	}
}

func TestTrustStoreFailsClosedOnCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"records":`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("Open accepted a corrupt trust store")
	}
}

func TestTrustStoreIgnoresOldVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	old := `{"version":1,"records":{"repo:official@abc":{"kind":"repo","name":"official","commit":"abc","trustedAt":"2026-10-18T00:00:00Z"}}}`
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Verify("repo", "official", ""); err != nil || ok {
		t.Fatalf("old trust record was accepted: %v, %v", ok, err)
	}
	if err := store.Trust("repo", "official", ""); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if data.Version != Version {
		t.Fatalf("trust store version = %d, want %d", data.Version, Version)
	}
}

func TestTrustStoreTreatsNewerVersionAsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	newer := `{"version":3,"records":{"repo:official":{"kind":"repo","name":"official","url":"https://example.invalid/repo","trustedAt":"2026-10-18T00:00:00Z"}}}`
	if err := os.WriteFile(path, []byte(newer), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Verify("repo", "official", ""); err != nil || ok {
		t.Fatalf("newer trust record was accepted: %v, %v", ok, err)
	}
	if err := store.Trust("repo", "official", ""); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if data.Version != Version {
		t.Fatalf("trust store version = %d, want %d", data.Version, Version)
	}
}

func TestTrustStoreRejectsOldVersionWithBadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"records":`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("Open accepted malformed JSON")
	}
}

func TestTrustRequiresKindAndName(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Trust("", "official", ""); err == nil {
		t.Fatal("Trust accepted an empty kind")
	}
	if err := store.Trust("repo", "", ""); err == nil {
		t.Fatal("Trust accepted an empty name")
	}
}

func TestTrustStoreRejectsMismatchedRecordKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	data := `{"version":2,"records":{"repo:other":{"kind":"repo","name":"official","trustedAt":"2026-10-18T00:00:00Z"}}}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("Open accepted a mismatched record key")
	}
}
