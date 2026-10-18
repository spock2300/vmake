package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/truststore"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	repoPkg "github.com/spock2300/vmake/pkg/repo"
)

func setupTrustRepo(t *testing.T) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	cloneDir := filepath.Join(tmp, "repos", "testrepo")
	if err := os.MkdirAll(cloneDir, 0755); err != nil {
		t.Fatal(err)
	}
	runGitLine(t, cloneDir, "init", "-q", "-b", "master")
	runGitLine(t, cloneDir, "config", "user.email", "test@example.com")
	runGitLine(t, cloneDir, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(cloneDir, "wrapper.txt"), []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitLine(t, cloneDir, "add", "-A")
	runGitLine(t, cloneDir, "commit", "-q", "-m", "v1")

	origVmakeDir := vmakeDir
	vmakeDir = tmp
	t.Cleanup(func() { vmakeDir = origVmakeDir })

	commit, err := repoPkg.GetCurrentCommit(cloneDir)
	if err != nil {
		t.Fatal(err)
	}
	return tmp, commit
}

func runGitLine(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRemoteScriptTrustCheckerBindsCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	_, commit := setupTrustRepo(t)
	store, err := openTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Trust(trustKindRepo, "testrepo", "", commit); err != nil {
		t.Fatal(err)
	}
	base := buildscript.Source{Name: "testrepo/pkg", Repo: "testrepo", Commit: commit, Origin: api.SourceRemote}
	if err := remoteScriptTrustChecker(base); err != nil {
		t.Fatalf("trusted commit rejected: %v", err)
	}
	changed := base
	changed.Commit = strings.Repeat("0", len(commit))
	if err := remoteScriptTrustChecker(changed); err == nil {
		t.Fatal("changed commit was accepted")
	}
	missing := base
	missing.Commit = ""
	if err := remoteScriptTrustChecker(missing); err == nil {
		t.Fatal("empty commit was accepted")
	}
}

func TestAddAndRemoveTrustedRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	_, commit := setupTrustRepo(t)
	if err := addTrustedRepo("testrepo"); err != nil {
		t.Fatal(err)
	}
	store, err := openTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	ok, err := store.Verify(trustKindRepo, "testrepo", "", commit)
	if err != nil || !ok {
		t.Fatalf("trust record missing: %v, %v", ok, err)
	}
	if err := removeTrustedRepo("testrepo"); err != nil {
		t.Fatal(err)
	}
	ok, err = store.Verify(trustKindRepo, "testrepo", "", commit)
	if err != nil || ok {
		t.Fatalf("trust record survived removal: %v, %v", ok, err)
	}
}

func TestTrustStoreCorruptionFailsClosed(t *testing.T) {
	setupTrustRepo(t)
	path := filepath.Join(vmakeDir, "trust.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"records":`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := openTrustStore(); err == nil {
		t.Fatal("corrupt trust store was accepted")
	}
	if err := remoteScriptTrustChecker(buildscript.Source{Repo: "testrepo", Commit: strings.Repeat("a", 40), Origin: api.SourceRemote}); err == nil {
		t.Fatal("trust checker accepted a corrupt store")
	}
}

func TestTrustStoreUsesStrictSchema(t *testing.T) {
	setupTrustRepo(t)
	path := filepath.Join(vmakeDir, "trust.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"records":{},"extra":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := truststore.Open(path); err == nil {
		t.Fatal("trust store accepted an unknown field")
	}
}
