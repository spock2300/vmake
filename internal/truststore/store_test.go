package truststore

import (
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
	ok, err := store.Verify("repo", "official", "", "abc")
	if err != nil || ok {
		t.Fatalf("empty store Verify = %v, %v", ok, err)
	}
	if err := store.Trust("repo", "official", "https://example.invalid/repo", "abc"); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Verify("repo", "official", "", "abc"); err != nil || !ok {
		t.Fatalf("Verify after Trust = %v, %v", ok, err)
	}
	if ok, err := store.Verify("repo", "official", "", "def"); err != nil || ok {
		t.Fatalf("Verify with wrong commit = %v, %v", ok, err)
	}
	if ok, err := store.Verify("repo", "official", "https://evil.invalid/repo", "abc"); err != nil || ok {
		t.Fatalf("Verify with wrong URL = %v, %v", ok, err)
	}
	if err := store.Revoke("repo", "official"); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Verify("repo", "official", "", "abc"); err != nil || ok {
		t.Fatalf("Verify after Revoke = %v, %v", ok, err)
	}
}

func TestTrustStoreFailsClosedOnCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"records":`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("Open accepted a corrupt trust store")
	}
}

func TestTrustStoreRejectsOldVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	if err := os.WriteFile(path, []byte(`{"version":0,"records":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("Open accepted an unsupported trust store version")
	}
}

func TestTrustRequiresCommit(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Trust("repo", "official", "", ""); err == nil {
		t.Fatal("Trust accepted an empty commit")
	}
	for _, commit := range []string{"a:b", "a@b"} {
		if err := store.Trust("repo", "official", "", commit); err == nil {
			t.Fatalf("Trust accepted the record separator in commit %q", commit)
		}
	}
}

func TestTrustRecordsArePerCommit(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, commit := range []string{"aaa", "bbb"} {
		if err := store.Trust("repo", "native", "", commit); err != nil {
			t.Fatal(err)
		}
	}
	for _, commit := range []string{"aaa", "bbb"} {
		if ok, err := store.Verify("repo", "native", "", commit); err != nil || !ok {
			t.Fatalf("commit %s lost its trust record: %v, %v", commit, ok, err)
		}
	}
	if err := store.Revoke("repo", "native"); err != nil {
		t.Fatal(err)
	}
	for _, commit := range []string{"aaa", "bbb"} {
		if ok, err := store.Verify("repo", "native", "", commit); err != nil || ok {
			t.Fatalf("commit %s survived Revoke: %v, %v", commit, ok, err)
		}
	}
}
