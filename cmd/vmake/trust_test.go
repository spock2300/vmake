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

func useTrustVmakeDir(t *testing.T) string {
	t.Helper()
	previous := vmakeDir
	vmakeDir = t.TempDir()
	t.Cleanup(func() { vmakeDir = previous })
	return vmakeDir
}

func writeTrustTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func setupTrustUpdatableRepo(t *testing.T, cloneDir string) (string, string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	work := filepath.Join(root, "work")
	runGitLine(t, root, "init", "-q", "--bare", origin)
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatal(err)
	}
	runGitLine(t, work, "init", "-q", "-b", "master")
	runGitLine(t, work, "config", "user.email", "test@example.com")
	runGitLine(t, work, "config", "user.name", "test")
	writeTrustTestFile(t, filepath.Join(work, "wrapper.txt"), "v1\n")
	runGitLine(t, work, "add", "-A")
	runGitLine(t, work, "commit", "-q", "-m", "v1")
	runGitLine(t, work, "remote", "add", "origin", origin)
	runGitLine(t, work, "push", "-q", "-u", "origin", "master")
	runGitLine(t, origin, "symbolic-ref", "HEAD", "refs/heads/master")
	if err := os.MkdirAll(filepath.Dir(cloneDir), 0755); err != nil {
		t.Fatal(err)
	}
	runGitLine(t, filepath.Dir(cloneDir), "clone", "-q", origin, cloneDir)
	return work, origin
}

func TestRemoteScriptTrustCheckerTrustsRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("VMAKE_TRUST_ALL", "")
	_, commit := setupTrustRepo(t)
	if err := addTrustedRepo("testrepo"); err != nil {
		t.Fatal(err)
	}
	base := buildscript.Source{Name: "testrepo/pkg", Repo: "testrepo", Commit: commit, Origin: api.SourceRemote}
	if err := remoteScriptTrustChecker(base); err != nil {
		t.Fatalf("trusted repository rejected: %v", err)
	}
	changed := base
	changed.Commit = strings.Repeat("0", len(commit))
	if err := remoteScriptTrustChecker(changed); err != nil {
		t.Fatalf("repository trust did not cover a new commit: %v", err)
	}
	missing := base
	missing.Commit = ""
	if err := remoteScriptTrustChecker(missing); err != nil {
		t.Fatalf("repository trust required a commit: %v", err)
	}
	if trusted, err := repoScriptTrusted("other"); err != nil || trusted {
		t.Fatalf("untrusted repository result = %v, %v", trusted, err)
	}
}

func TestAddAndRemoveTrustedRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tmp, _ := setupTrustRepo(t)
	if err := addTrustedRepo("testrepo"); err != nil {
		t.Fatal(err)
	}
	store, err := openTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	ok, err := store.Verify(trustKindRepo, "testrepo", "")
	if err != nil || !ok {
		t.Fatalf("trust record missing: %v, %v", ok, err)
	}
	runGitLine(t, filepath.Join(tmp, "repos", "testrepo"), "commit", "--allow-empty", "-q", "-m", "v2")
	if trusted, err := repoScriptTrusted("testrepo"); err != nil || !trusted {
		t.Fatalf("trust did not survive a new commit: %v, %v", trusted, err)
	}
	if err := removeTrustedRepo("testrepo"); err != nil {
		t.Fatal(err)
	}
	ok, err = store.Verify(trustKindRepo, "testrepo", "")
	if err != nil || ok {
		t.Fatalf("trust record survived removal: %v, %v", ok, err)
	}
}

func TestRepoTrustUsesOriginURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tmp, _ := setupTrustRepo(t)
	t.Setenv("VMAKE_TRUST_ALL", "")
	const origin = "https://example.invalid/testrepo.git"
	const changed = "https://evil.invalid/testrepo.git"
	cloneDir := filepath.Join(tmp, "repos", "testrepo")
	runGitLine(t, cloneDir, "remote", "add", "origin", origin)

	if err := addTrustedRepo("testrepo"); err != nil {
		t.Fatal(err)
	}
	got, err := getRepoManager().URL("testrepo")
	if err != nil {
		t.Fatal(err)
	}
	if got != origin {
		t.Fatalf("RepoManager.URL = %q, want %q", got, origin)
	}
	store, err := openTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Verify(trustKindRepo, "testrepo", origin); err != nil || !ok {
		t.Fatalf("origin-bound trust missing: %v, %v", ok, err)
	}
	if ok, err := store.Verify(trustKindRepo, "testrepo", changed); err != nil || ok {
		t.Fatalf("trust matched a different origin: %v, %v", ok, err)
	}
	if trusted, err := repoScriptTrusted("testrepo"); err != nil || !trusted {
		t.Fatalf("origin-bound repository not trusted: %v, %v", trusted, err)
	}
	runGitLine(t, cloneDir, "remote", "set-url", "origin", changed)
	if trusted, err := repoScriptTrusted("testrepo"); err != nil || trusted {
		t.Fatalf("trust survived an origin change: %v, %v", trusted, err)
	}
}

func TestRepoUpdateKeepsTrust(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tmp := useTrustVmakeDir(t)
	t.Setenv("VMAKE_TRUST_ALL", "")
	cloneDir := filepath.Join(tmp, "repos", "testrepo")
	work, _ := setupTrustUpdatableRepo(t, cloneDir)
	if err := addTrustedRepo("testrepo"); err != nil {
		t.Fatal(err)
	}
	writeTrustTestFile(t, filepath.Join(work, "wrapper.txt"), "v2\n")
	runGitLine(t, work, "add", "-A")
	runGitLine(t, work, "commit", "-q", "-m", "v2")
	runGitLine(t, work, "push", "-q", "origin", "master")
	before, err := repoPkg.GetCurrentCommit(cloneDir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := getRepoManager()
	lock, err := mgr.Lock("testrepo", true)
	if err != nil {
		t.Fatal(err)
	}
	updateErr := mgr.UpdateLocked("testrepo")
	lock.Release()
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	after, err := repoPkg.GetCurrentCommit(cloneDir)
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatal("repository update did not fetch the new commit")
	}
	if trusted, err := repoScriptTrusted("testrepo"); err != nil || !trusted {
		t.Fatalf("trust revoked by repository update: %v, %v", trusted, err)
	}
}

func TestDepsRefreshReposKeepsTrust(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tmp := useTrustVmakeDir(t)
	t.Setenv("VMAKE_TRUST_ALL", "")
	cloneDir := filepath.Join(tmp, "repos", "testrepo")
	work, _ := setupTrustUpdatableRepo(t, cloneDir)
	if err := addTrustedRepo("testrepo"); err != nil {
		t.Fatal(err)
	}
	writeTrustTestFile(t, filepath.Join(work, "wrapper.txt"), "v2\n")
	runGitLine(t, work, "add", "-A")
	runGitLine(t, work, "commit", "-q", "-m", "v2")
	runGitLine(t, work, "push", "-q", "origin", "master")
	before, err := repoPkg.GetCurrentCommit(cloneDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := refreshRegistryReposForUpdate(nil); err != nil {
		t.Fatal(err)
	}
	after, err := repoPkg.GetCurrentCommit(cloneDir)
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatal("deps refresh did not fetch the new commit")
	}
	if trusted, err := repoScriptTrusted("testrepo"); err != nil || !trusted {
		t.Fatalf("trust revoked by deps refresh: %v, %v", trusted, err)
	}
}

func TestNativeRepoTrustWithoutCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	setupTrustRepo(t)
	const template = "https://git.example.com/{name}.git"
	nativeDir := filepath.Join(vmakeDir, "repos", "native")
	if err := os.MkdirAll(nativeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := repoPkg.SaveNativeConfig(nativeDir, template); err != nil {
		t.Fatal(err)
	}
	if err := addTrustedRepo("native"); err != nil {
		t.Fatalf("native repository trust failed without a commit: %v", err)
	}
	store, err := openTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Verify(trustKindRepo, "native", template); err != nil || !ok {
		t.Fatalf("native trust record missing: %v, %v", ok, err)
	}
	if trusted, err := repoScriptTrusted("native"); err != nil || !trusted {
		t.Fatalf("native repository not trusted: %v, %v", trusted, err)
	}
	src := buildscript.Source{Name: "native/pkg", Repo: "native", Origin: api.SourceRemote}
	if err := remoteScriptTrustChecker(src); err != nil {
		t.Fatalf("native repository checker rejected trust: %v", err)
	}
	const changed = "https://other.example.com/{name}.git"
	if err := repoPkg.SaveNativeConfig(nativeDir, changed); err != nil {
		t.Fatal(err)
	}
	if trusted, err := repoScriptTrusted("native"); err != nil || trusted {
		t.Fatalf("native trust survived a template change: %v, %v", trusted, err)
	}
}

func TestExtensionTrustDoesNotRequireCommitOrClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tmp := useTrustVmakeDir(t)
	t.Setenv("VMAKE_TRUST_ALL", "")

	extDir := filepath.Join(tmp, "extensions", "ext")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	runGitLine(t, extDir, "init", "-q", "-b", "master")
	const origin = "https://example.invalid/ext.git"
	const changed = "https://evil.invalid/ext.git"
	runGitLine(t, extDir, "remote", "add", "origin", origin)

	if err := trustExtension("ext"); err != nil {
		t.Fatalf("trust extension without a commit or clean worktree: %v", err)
	}
	store, err := openTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Verify(trustKindExtension, "ext", origin); err != nil || !ok {
		t.Fatalf("extension origin trust missing: %v, %v", ok, err)
	}
	if trusted, err := extensionTrusted("ext"); err != nil || !trusted {
		t.Fatalf("trusted extension result = %v, %v", trusted, err)
	}
	writeTrustTestFile(t, filepath.Join(extDir, "dirty.go"), "package main\n")
	if trusted, err := extensionTrusted("ext"); err != nil || !trusted {
		t.Fatalf("dirty extension lost trust: %v, %v", trusted, err)
	}
	runGitLine(t, extDir, "remote", "set-url", "origin", changed)
	if trusted, err := extensionTrusted("ext"); err != nil || trusted {
		t.Fatalf("extension trust survived an origin change: %v, %v", trusted, err)
	}
}

func TestExtensionUpdateKeepsTrust(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tmp := useTrustVmakeDir(t)
	t.Setenv("VMAKE_TRUST_ALL", "")
	cloneDir := filepath.Join(tmp, "extensions", "ext")
	work, _ := setupTrustUpdatableRepo(t, cloneDir)
	if err := trustExtension("ext"); err != nil {
		t.Fatal(err)
	}
	writeTrustTestFile(t, filepath.Join(work, "wrapper.txt"), "v2\n")
	runGitLine(t, work, "add", "-A")
	runGitLine(t, work, "commit", "-q", "-m", "v2")
	runGitLine(t, work, "push", "-q", "origin", "master")
	before, err := getPluginManager().Commit("ext")
	if err != nil {
		t.Fatal(err)
	}
	if err := updateExtensionRepo(getPluginManager(), "ext"); err != nil {
		t.Fatal(err)
	}
	after, err := getPluginManager().Commit("ext")
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatal("extension update did not fetch the new commit")
	}
	if trusted, err := extensionTrusted("ext"); err != nil || !trusted {
		t.Fatalf("trust revoked by extension update: %v, %v", trusted, err)
	}
}

func TestExtensionUntrustRevokesTrust(t *testing.T) {
	tmp := useTrustVmakeDir(t)
	t.Setenv("VMAKE_TRUST_ALL", "")
	if err := os.MkdirAll(filepath.Join(tmp, "extensions", "ext"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := trustExtension("ext"); err != nil {
		t.Fatal(err)
	}
	if err := revokeExtension("ext"); err != nil {
		t.Fatal(err)
	}
	store, err := openTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Verify(trustKindExtension, "ext", ""); err != nil || ok {
		t.Fatalf("extension trust survived untrust: %v, %v", ok, err)
	}
}

func TestExtensionRemoveRevokesTrust(t *testing.T) {
	tmp := useTrustVmakeDir(t)
	t.Setenv("VMAKE_TRUST_ALL", "")
	if err := os.MkdirAll(filepath.Join(tmp, "extensions", "ext"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := trustExtension("ext"); err != nil {
		t.Fatal(err)
	}
	if err := removeExtensionRepo("ext"); err != nil {
		t.Fatal(err)
	}
	store, err := openTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Verify(trustKindExtension, "ext", ""); err != nil || ok {
		t.Fatalf("extension trust survived removal: %v, %v", ok, err)
	}
}

func TestTrustStoreCorruptionFailsClosed(t *testing.T) {
	setupTrustRepo(t)
	path := filepath.Join(vmakeDir, "trust.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"records":`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := openTrustStore(); err == nil {
		t.Fatal("corrupt trust store was accepted")
	}
	if err := remoteScriptTrustChecker(buildscript.Source{Repo: "testrepo", Origin: api.SourceRemote}); err == nil {
		t.Fatal("trust checker accepted a corrupt store")
	}
}

func TestTrustStoreUsesStrictSchema(t *testing.T) {
	setupTrustRepo(t)
	path := filepath.Join(vmakeDir, "trust.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"records":{},"extra":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := truststore.Open(path); err == nil {
		t.Fatal("trust store accepted an unknown field")
	}
}
