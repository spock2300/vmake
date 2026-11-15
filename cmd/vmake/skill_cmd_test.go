package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/version"
)

func TestEmbeddedSkillDigestStable(t *testing.T) {
	first, err := embeddedSkillDigest()
	if err != nil {
		t.Fatal(err)
	}
	second, err := embeddedSkillDigest()
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("digest is not stable: %q vs %q", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("digest is not a sha256 hex string: %q", first)
	}
}

func TestInstallSkillWritesManifest(t *testing.T) {
	dir := t.TempDir()
	if err := installSkillTargets([]string{dir}); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"SKILL.md", "references/cli.md", skillManifestName} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("%s was not installed: %v", rel, err)
		}
	}
	m, err := readSkillManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != 1 {
		t.Errorf("schemaVersion = %d, want 1", m.SchemaVersion)
	}
	if m.Version != version.Version || m.Commit != version.GitCommit {
		t.Errorf("manifest records %s %s, want %s %s", m.Version, m.Commit, version.Version, version.GitCommit)
	}
	digest, err := embeddedSkillDigest()
	if err != nil {
		t.Fatal(err)
	}
	if m.SkillDigest != digest {
		t.Errorf("manifest digest = %q, want %q", m.SkillDigest, digest)
	}
	if m.InstalledAt == "" {
		t.Error("manifest has no installedAt")
	}
}

func TestCheckInstalledSkill(t *testing.T) {
	dir := t.TempDir()
	if findings := checkInstalledSkill(dir); len(findings) != 0 {
		t.Fatalf("missing install reported findings: %v", findings)
	}
	if err := installSkillTargets([]string{dir}); err != nil {
		t.Fatal(err)
	}
	for _, f := range checkInstalledSkill(dir) {
		if f.Severity == "error" {
			t.Fatalf("fresh install reported an error: %v", f)
		}
	}

	m, err := readSkillManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.SkillDigest = strings.Repeat("0", 64)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, skillManifestName), data, 0644); err != nil {
		t.Fatal(err)
	}
	if !hasDoctorError(checkInstalledSkill(dir)) {
		t.Fatal("stale digest did not report an error")
	}

	if err := os.Remove(filepath.Join(dir, skillManifestName)); err != nil {
		t.Fatal(err)
	}
	if !hasDoctorError(checkInstalledSkill(dir)) {
		t.Fatal("missing manifest did not report an error")
	}
}

func hasDoctorError(findings []doctorFinding) bool {
	for _, f := range findings {
		if f.Severity == "error" {
			return true
		}
	}
	return false
}
