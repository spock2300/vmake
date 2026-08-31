package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/jsonio"
	vlog "github.com/spock2300/vmake/pkg/log"
)

type globalConfig struct {
	TrustedRepos []string `json:"trustedRepos,omitempty"`
}

func globalConfigPath() string {
	return filepath.Join(vmakeDir, "config.json")
}

func loadGlobalConfig() *globalConfig {
	cfg := &globalConfig{}
	if err := jsonio.Load(globalConfigPath(), cfg); err != nil {
		return &globalConfig{}
	}
	return cfg
}

func saveGlobalConfig(cfg *globalConfig) error {
	return jsonio.Save(globalConfigPath(), cfg)
}

func isRepoTrusted(repo string) bool {
	cfg := loadGlobalConfig()
	for _, r := range cfg.TrustedRepos {
		if r == repo {
			return true
		}
	}
	return false
}

func addTrustedRepo(repo string) error {
	cfg := loadGlobalConfig()
	for _, r := range cfg.TrustedRepos {
		if r == repo {
			return nil
		}
	}
	cfg.TrustedRepos = append(cfg.TrustedRepos, repo)
	if err := saveGlobalConfig(cfg); err != nil {
		return err
	}
	vlog.Info("Trusted repository '%s' (recorded in %s)", repo, globalConfigPath())
	return nil
}

func removeTrustedRepo(repo string) error {
	cfg := loadGlobalConfig()
	var kept []string
	for _, r := range cfg.TrustedRepos {
		if r != repo {
			kept = append(kept, r)
		}
	}
	cfg.TrustedRepos = kept
	return saveGlobalConfig(cfg)
}

func confirmTrustRemoteRepo(repo string) bool {
	if yesFlag {
		if err := addTrustedRepo(repo); err != nil {
			vlog.Error("record trust for %s: %v", repo, err)
			return false
		}
		return true
	}
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	vlog.Info("")
	vlog.Info("The remote repository '%s' provides build.go scripts that vmake will", repo)
	vlog.Info("execute (full system access). Trust this repository?")
	fmt.Print("Trust? [y/N]: ")
	var answer string
	fmt.Fscanln(bufio.NewReader(os.Stdin), &answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return false
	}
	return addTrustedRepo(repo) == nil
}

func remoteScriptTrustChecker(repo string) error {
	if os.Getenv("VMAKE_TRUST_ALL") == "1" {
		return nil
	}
	if isRepoTrusted(repo) {
		return nil
	}
	if confirmTrustRemoteRepo(repo) {
		return nil
	}
	return fmt.Errorf("its build.go scripts are not trusted; pass --yes or run 'vmake repo trust %s'", repo)
}
