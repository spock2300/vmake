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
	"github.com/spock2300/vmake/pkg/plugin"
	"github.com/spock2300/vmake/pkg/repo"
)

const (
	trustKindRepo      = "repo"
	trustKindExtension = "extension"
	trustKeptNotice    = "Repository trust is kept across updates; updated code runs without another review."
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

func repoTrustURL(name string) (string, error) {
	mgr := repo.NewRepoManager(getReposDir())
	if !mgr.Exists(name) {
		return "", nil
	}
	if mgr.IsNative(name) {
		return mgr.GetNativeURL(name)
	}
	return mgr.URL(name)
}

func remoteScriptTrustChecker(src buildscript.Source) error {
	if trustAllEnabled() {
		return nil
	}
	repoURL, err := repoTrustURL(src.Repo)
	if err != nil {
		return err
	}
	fromManifest := false
	if repoURL == "" && src.RepoURL != "" {
		repoURL = src.RepoURL
		fromManifest = true
	}
	store, err := openTrustStore()
	if err != nil {
		return err
	}
	trusted, err := store.Verify(trustKindRepo, src.Repo, repoURL)
	if err != nil {
		return err
	}
	if trusted {
		return nil
	}
	subject := fmt.Sprintf("The repository %q provides build.go scripts that vmake will execute (full system access). Trusting it also trusts all future updates.", src.Repo)
	if fromManifest && repoURL != "" {
		subject = fmt.Sprintf("The repository %q (%s) provides build.go scripts that vmake will execute (full system access). Trusting it also trusts all future updates.", src.Repo, repoURL)
	}
	if !confirmTrust(subject) {
		if fromManifest && repoURL != "" {
			return fmt.Errorf("its build.go scripts are not trusted; run 'vmake repo trust %s --url %q' after reviewing the repository", src.Repo, repoURL)
		}
		return fmt.Errorf("its build.go scripts are not trusted; run 'vmake repo trust %s' after reviewing the repository", src.Repo)
	}
	return store.Trust(trustKindRepo, src.Repo, repoURL)
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

func repoScriptTrusted(name string) (bool, error) {
	if trustAllEnabled() {
		return true, nil
	}
	repoURL, err := repoTrustURL(name)
	if err != nil {
		return false, err
	}
	store, err := openTrustStore()
	if err != nil {
		return false, err
	}
	return store.Verify(trustKindRepo, name, repoURL)
}

func addTrustedRepo(name string) error {
	return addTrustedRepoWithURL(name, "")
}

func addTrustedRepoWithURL(name, explicitURL string) error {
	if err := gitstore.ValidateName(name); err != nil {
		return err
	}
	explicitURL = strings.TrimSpace(explicitURL)
	var repoURL string
	if explicitURL != "" {
		repoURL = explicitURL
	} else {
		mgr := repo.NewRepoManager(getReposDir())
		lock, err := mgr.Lock(name, true)
		if err != nil {
			return err
		}
		defer lock.Release()
		if !mgr.Exists(name) {
			return fmt.Errorf("repository %q not found (pass --url <url-template> to trust a repository declared in vmake.json)", name)
		}
		repoURL, err = repoTrustURL(name)
		if err != nil {
			return err
		}
	}
	store, err := openTrustStore()
	if err != nil {
		return err
	}
	if err := store.Trust(trustKindRepo, name, repoURL); err != nil {
		return err
	}
	if !vlog.IsQuiet() {
		if repoURL == "" {
			fmt.Printf("Trusted repository '%s' without an origin URL; trust is bound to its name only (recorded in %s)\n", name, trustStorePath())
		} else {
			fmt.Printf("Trusted repository '%s' (recorded in %s)\n", name, trustStorePath())
		}
	}
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

func extensionTrusted(name string) (bool, error) {
	if trustAllEnabled() {
		return true, nil
	}
	mgr := plugin.NewManager(vmakeDir)
	if !mgr.Exists(name) {
		return false, nil
	}
	store, err := openTrustStore()
	if err != nil {
		return false, err
	}
	return store.Verify(trustKindExtension, name, mgr.URL(name))
}

func trustExtension(name string) error {
	mgr := plugin.NewManager(vmakeDir)
	lock, err := mgr.LockRepo(name, true)
	if err != nil {
		return err
	}
	defer lock.Release()
	if !mgr.Exists(name) {
		return fmt.Errorf("extension repository %q not found", name)
	}
	store, err := openTrustStore()
	if err != nil {
		return err
	}
	if err := store.Trust(trustKindExtension, name, mgr.URL(name)); err != nil {
		return err
	}
	if !vlog.IsQuiet() {
		fmt.Printf("Trusted extension '%s' (recorded in %s)\n", name, trustStorePath())
	}
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
