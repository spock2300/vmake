package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/jsonio"
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

func saveTrustConfig(t *testing.T, cfg *globalConfig) {
	t.Helper()
	if err := jsonio.Save(globalConfigPath(), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyTrustedCommitBootstrapRecords(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	_, commit := setupTrustRepo(t)

	saveTrustConfig(t, &globalConfig{TrustedRepos: []string{"testrepo"}})

	if err := verifyTrustedCommit("testrepo"); err != nil {
		t.Fatalf("bootstrap should pass: %v", err)
	}

	cfg := loadGlobalConfig()
	if cfg.TrustedCommits["testrepo"] != commit {
		t.Errorf("bootstrap should record commit %s, got %q", commit, cfg.TrustedCommits["testrepo"])
	}
}

func TestVerifyTrustedCommitMatchPasses(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	_, commit := setupTrustRepo(t)

	saveTrustConfig(t, &globalConfig{
		TrustedRepos:   []string{"testrepo"},
		TrustedCommits: map[string]string{"testrepo": commit},
	})

	if err := verifyTrustedCommit("testrepo"); err != nil {
		t.Fatalf("matching commit should pass: %v", err)
	}
}

func TestVerifyTrustedCommitMismatchFails(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	_, commit := setupTrustRepo(t)

	cloneDir := filepath.Join(vmakeDir, "repos", "testrepo")
	if err := os.WriteFile(filepath.Join(cloneDir, "wrapper.txt"), []byte("v2-evil\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitLine(t, cloneDir, "add", "-A")
	runGitLine(t, cloneDir, "commit", "-q", "-m", "v2")

	saveTrustConfig(t, &globalConfig{
		TrustedRepos:   []string{"testrepo"},
		TrustedCommits: map[string]string{"testrepo": commit},
	})

	err := verifyTrustedCommit("testrepo")
	if err == nil {
		t.Fatal("changed repo content should fail trust verification")
	}
	for _, want := range []string{"changed since it was trusted", "vmake repo trust testrepo"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}
