package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/gitstore"
	"github.com/spock2300/vmake/internal/truststore"
	"github.com/spock2300/vmake/pkg/buildscript"
	vlog "github.com/spock2300/vmake/pkg/log"
	repoPkg "github.com/spock2300/vmake/pkg/repo"
)

const (
	trustKindRepo      = "repo"
	trustKindExtension = "extension"
)

func trustStorePath() string {
	return filepath.Join(vmakeDir, "trust.json")
}

func openTrustStore() (*truststore.Store, error) {
	return truststore.Open(trustStorePath())
}

func trustAllEnabled() bool {
	return os.Getenv("VMAKE_TRUST_ALL") == "1"
}

func remoteScriptTrustChecker(src buildscript.Source) error {
	if trustAllEnabled() {
		return nil
	}
	if src.Commit == "" {
		return fmt.Errorf("cannot verify trust for repository %q: the resolved commit is empty", src.Repo)
	}
	store, err := openTrustStore()
	if err != nil {
		return err
	}
	trusted, err := store.Verify(trustKindRepo, src.Repo, src.URL, src.Commit)
	if err != nil {
		return err
	}
	if trusted {
		return nil
	}
	subject := fmt.Sprintf("The remote repository %q provides build.go scripts at commit %s that vmake will execute (full system access).", src.Repo, repoPkg.ShortCommit(src.Commit))
	if !confirmTrust(subject) {
		if src.URL != "" {
			return fmt.Errorf("its build.go scripts are not trusted; re-run with --yes after reviewing the package at commit %s", repoPkg.ShortCommit(src.Commit))
		}
		return fmt.Errorf("its build.go scripts are not trusted; run 'vmake repo trust %s' after reviewing the repository", src.Repo)
	}
	return store.Trust(trustKindRepo, src.Repo, src.URL, src.Commit)
}

func confirmTrust(subject string) bool {
	if trustAllEnabled() || yesFlag {
		return true
	}
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	fmt.Println()
	fmt.Println(subject)
	fmt.Print("Trust? [y/N]: ")
	var answer string
	fmt.Fscanln(bufio.NewReader(os.Stdin), &answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

func repoScriptTrusted(name, commit string) (bool, error) {
	if trustAllEnabled() {
		return true, nil
	}
	if commit == "" {
		return false, nil
	}
	store, err := openTrustStore()
	if err != nil {
		return false, err
	}
	return store.Verify(trustKindRepo, name, "", commit)
}

func repoTrustCommit(name string) (string, error) {
	mgr := getRepoManager()
	if !mgr.Exists(name) {
		return "", fmt.Errorf("repository %q not found", name)
	}
	if mgr.IsNative(name) {
		return "", fmt.Errorf("native repository %q is trusted per package commit on first build; use --yes with a build, or inspect and trust the resolved package version", name)
	}
	return repoPkg.GetCurrentCommit(mgr.Path(name))
}

func addTrustedRepo(name string) error {
	lock, err := getRepoManager().Lock(name, true)
	if err != nil {
		return err
	}
	defer lock.Release()
	commit, err := repoTrustCommit(name)
	if err != nil {
		return err
	}
	store, err := openTrustStore()
	if err != nil {
		return err
	}
	if err := store.Trust(trustKindRepo, name, "", commit); err != nil {
		return err
	}
	fmt.Printf("Trusted repository '%s' at %s (recorded in %s)\n", name, repoPkg.ShortCommit(commit), trustStorePath())
	return nil
}

func removeTrustedRepo(name string) error {
	if err := gitstore.ValidateName(name); err != nil {
		return err
	}
	store, err := openTrustStore()
	if err != nil {
		return err
	}
	return store.Revoke(trustKindRepo, name)
}

func revokeRepoTrustBestEffort(name string) {
	if err := removeTrustedRepo(name); err != nil {
		vlog.Error("reset trust for repository %s: %v", name, err)
	}
}

func extensionTrustCommit(name string) (string, error) {
	mgr := getPluginManager()
	if !mgr.Exists(name) {
		return "", fmt.Errorf("extension repository %q not found", name)
	}
	commit, err := mgr.Commit(name)
	if err != nil {
		return "", fmt.Errorf("read extension repository %q: %w", name, err)
	}
	clean, err := mgr.Clean(name)
	if err != nil {
		return "", fmt.Errorf("inspect extension repository %q: %w", name, err)
	}
	if !clean {
		return "", fmt.Errorf("extension repository %q has local changes; commit or discard them before trusting it", name)
	}
	return commit, nil
}

func extensionTrusted(name string) (bool, error) {
	if trustAllEnabled() {
		return true, nil
	}
	mgr := getPluginManager()
	if !mgr.Exists(name) {
		return false, nil
	}
	commit, err := mgr.Commit(name)
	if err != nil {
		return false, err
	}
	clean, err := mgr.Clean(name)
	if err != nil {
		return false, err
	}
	if !clean {
		return false, nil
	}
	store, err := openTrustStore()
	if err != nil {
		return false, err
	}
	return store.Verify(trustKindExtension, name, mgr.URL(name), commit)
}

func trustExtension(name string) error {
	lock, err := getPluginManager().LockRepo(name, true)
	if err != nil {
		return err
	}
	defer lock.Release()
	commit, err := extensionTrustCommit(name)
	if err != nil {
		return err
	}
	store, err := openTrustStore()
	if err != nil {
		return err
	}
	if err := store.Trust(trustKindExtension, name, getPluginManager().URL(name), commit); err != nil {
		return err
	}
	fmt.Printf("Trusted extension '%s' at %s (recorded in %s)\n", name, repoPkg.ShortCommit(commit), trustStorePath())
	return nil
}

func revokeExtension(name string) error {
	if err := gitstore.ValidateName(name); err != nil {
		return err
	}
	store, err := openTrustStore()
	if err != nil {
		return err
	}
	return store.Revoke(trustKindExtension, name)
}
