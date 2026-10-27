package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/repo"
)

func TestListNativePackagesAt(t *testing.T) {
	depsDir := t.TempDir()
	writePkgTestFile(t, filepath.Join(depsDir, "native", "libfoo", "src", "build.go"), "package main")
	writePkgTestFile(t, filepath.Join(depsDir, "native", "libbar", "src", "README.md"), "no build")
	writePkgTestFile(t, filepath.Join(depsDir, "native", "nosrc", "build.go"), "package main")

	names, err := listNativePackagesAt(depsDir, "native")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(names, ","), "libfoo"; got != want {
		t.Fatalf("native packages = %q, want %q", got, want)
	}
}

func TestSearchNativeRepoWarnsWhenNothingCached(t *testing.T) {
	previous := projectOverride
	projectOverride = t.TempDir()
	t.Cleanup(func() { projectOverride = previous })
	writePkgTestFile(t, filepath.Join(projectOverride, ".vmake", "config.json"), `{"version":"1"}`)

	entries, warnings := searchNativeRepo("test_build", pkgSearchOptions{}, nil)
	if len(entries) != 0 {
		t.Fatalf("entries = %+v", entries)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "no packages cached") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestRepoManagerListPackages(t *testing.T) {
	reposDir := t.TempDir()
	writePkgTestFile(t, filepath.Join(reposDir, "official", "packages", "z", "zlib", "build.go"), "package main")
	writePkgTestFile(t, filepath.Join(reposDir, "official", "packages", "c", "cjson", "build.go"), "package main")
	writePkgTestFile(t, filepath.Join(reposDir, "official", "packages", "x", "empty", "README.md"), "not a package")

	mgr := repo.NewRepoManager(reposDir)
	names, err := mgr.ListPackages("official")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(names, ","), "cjson,zlib"; got != want {
		t.Fatalf("registry packages = %q, want %q", got, want)
	}
	if _, err := mgr.ListPackages("missing"); err == nil {
		t.Fatal("missing repository did not fail")
	}
}

func TestPkgStatusReader(t *testing.T) {
	projectDir := t.TempDir()
	depsDir := filepath.Join(projectDir, ".vmake_deps")
	cacheDir := filepath.Join(projectDir, ".vmake", "cache")
	writePkgTestFile(t, filepath.Join(projectDir, ".vmake", "vmake.lock"), `{
  "lockfileVersion": 1,
  "packages": {
    "official/zlib": {"version": "1.3.1", "commit": "abcdef1234567890", "source": "git"}
  }
}`)
	writePkgTestFile(t, filepath.Join(depsDir, "official", "zlib", "state.json"), `{
  "urls": ["https://example.com/zlib.git"],
  "version": "1.3.1",
  "commit": "1234567890abcdef"
}`)
	writePkgTestFile(t, filepath.Join(projectDir, "install", "manifest.json"), `{
  "vmake": "test",
  "toolchain": "host",
  "mode": "release",
  "generated": "2026-01-01T00:00:00Z",
  "packages": [{"name": "official/zlib", "version": "1.3.1", "source": "registry"}]
}`)

	reader, err := newPkgStatusReaderAt(projectDir, depsDir, cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	status := reader.forPackage("official", "zlib")
	if status == nil {
		t.Fatal("missing package status")
	}
	if status.Cached != "1.3.1" || status.Locked != "1.3.1" || !status.Installed {
		t.Fatalf("status = %+v", status)
	}
	if got := reader.forPackage("official", "unknown"); got != nil {
		t.Fatalf("unknown package status = %+v", got)
	}
}

func TestPkgEntryMatches(t *testing.T) {
	entry := pkgSearchEntry{
		Repo:        "official",
		Name:        "mbedtls",
		Description: "Lightweight SSL/TLS library",
		License:     "Apache-2.0",
	}
	for _, pattern := range []string{"", "mbed", "official/mbedtls", "tls", "apache", "2.0"} {
		if !pkgEntryMatches(entry, pattern) {
			t.Errorf("pattern %q did not match", pattern)
		}
	}
	if pkgEntryMatches(entry, "zlib") {
		t.Error("unrelated pattern matched")
	}
}

func TestSortedPackageVersions(t *testing.T) {
	pkg := api.NewPackage().
		AddVersion("1.2.13", "v1.2.13").
		AddVersion("1.3.1", "v1.3.1").
		AddVersion("1.0.0", "v1.0.0")
	got := sortedPackageVersions(pkg)
	want := []string{"1.3.1", "1.2.13", "1.0.0"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("versions = %v, want %v", got, want)
	}
}

func TestAddRequiresSnippet(t *testing.T) {
	for _, test := range []struct {
		version string
		want    string
	}{
		{"1.3.1", `p.AddRequires("official/zlib >= 1.3")`},
		{"v2.0.0", `p.AddRequires("official/zlib >= 2.0")`},
		{"latest", `p.AddRequires("official/zlib")`},
		{"", `p.AddRequires("official/zlib")`},
	} {
		if got := addRequiresSnippet("official", "zlib", test.version); got != test.want {
			t.Errorf("version %q: %q, want %q", test.version, got, test.want)
		}
	}
}

func TestSplitPkgShowArg(t *testing.T) {
	repoName, pkgName, version, err := splitPkgShowArg("official/zlib@1.3.1")
	if err != nil {
		t.Fatal(err)
	}
	if repoName != "official" || pkgName != "zlib" || version != "1.3.1" {
		t.Fatalf("split = %q %q %q", repoName, pkgName, version)
	}
	if _, _, _, err := splitPkgShowArg("official"); err == nil {
		t.Fatal("invalid reference did not fail")
	}
	if _, _, _, err := splitPkgShowArg("official/zlib@"); err == nil {
		t.Fatal("empty version did not fail")
	}
	if _, _, _, err := splitPkgShowArg("subtest/mother/sub_a"); err == nil || !strings.Contains(err.Error(), "parent package") {
		t.Fatalf("sub-package error = %v", err)
	}
}

func TestWritePkgSearchTable(t *testing.T) {
	result := &pkgSearchResult{
		SchemaVersion: 1,
		Packages: []pkgSearchEntry{{
			Repo:        "official",
			Name:        "zlib",
			Source:      "registry",
			Latest:      "1.3.1",
			License:     "zlib",
			Description: "Compression library",
			Status:      &pkgStatus{Cached: "1.3.1", Locked: "1.3.1"},
		}, {
			Repo:   "native",
			Name:   "libfoo",
			Source: "native",
			Cached: true,
		}},
	}
	var out bytes.Buffer
	if err := writePkgSearchTable(&out, result, pkgSearchOptions{details: true, forProject: true}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"REPO", "PACKAGE", "LATEST", "LICENSE", "DESCRIPTION", "STATUS", "official", "zlib", "native (cached)", "cached 1.3.1, locked 1.3.1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := writePkgSearchTable(&out, &pkgSearchResult{Packages: []pkgSearchEntry{}}, pkgSearchOptions{pattern: "tls"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `No packages matching "tls".`) {
		t.Fatalf("empty output = %q", out.String())
	}
}

func TestPkgSearchJSONStableSource(t *testing.T) {
	var out bytes.Buffer
	result := &pkgSearchResult{
		SchemaVersion: 1,
		Packages: []pkgSearchEntry{{
			Repo:   "native",
			Name:   "libfoo",
			Source: "native",
			Cached: true,
		}},
		Warnings: []string{},
	}
	if err := writePkgJSON(&out, result); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, `"source": "native"`) || !strings.Contains(got, `"cached": true`) {
		t.Fatalf("native source JSON = %s", got)
	}
	if strings.Contains(got, "native (cached)") {
		t.Fatalf("JSON contains display label: %s", got)
	}
}

func TestLoadPackageScriptRejectsWrites(t *testing.T) {
	t.Setenv("VMAKE_TRUST_ALL", "1")
	dir := t.TempDir()
	buildGo := filepath.Join(dir, "build.go")
	writePkgTestFile(t, buildGo, `package main

import (
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		_ = os.WriteFile(filepath.Join(p.ScriptDir(), "written"), []byte("data"), 0644)
	})
}
`)
	_, err := loadPackageScript(buildscript.Source{
		Name:   "test/pkg",
		Path:   buildGo,
		Dir:    dir,
		Origin: api.SourceRemote,
		Repo:   "test",
		Commit: strings.Repeat("a", 40),
	})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("write attempt error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "written")); !os.IsNotExist(err) {
		t.Fatalf("read-only metadata load wrote a file: %v", err)
	}
}

func TestRepoScriptTrusted(t *testing.T) {
	previous := vmakeDir
	vmakeDir = t.TempDir()
	t.Cleanup(func() { vmakeDir = previous })
	t.Setenv("VMAKE_TRUST_ALL", "")

	commit := strings.Repeat("a", 40)
	if trusted, err := repoScriptTrusted("official", commit); err != nil || trusted {
		t.Fatalf("untrusted result = %v, %v", trusted, err)
	}
	store, err := openTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Trust(trustKindRepo, "official", "", commit); err != nil {
		t.Fatal(err)
	}
	if trusted, err := repoScriptTrusted("official", commit); err != nil || !trusted {
		t.Fatalf("trusted result = %v, %v", trusted, err)
	}
}

func TestLoadPkgShowFailsWhenTrustedScriptFails(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	previous := vmakeDir
	vmakeDir = t.TempDir()
	t.Cleanup(func() { vmakeDir = previous })
	t.Setenv("VMAKE_TRUST_ALL", "")

	repoDir := filepath.Join(vmakeDir, "repos", "official")
	writePkgTestFile(t, filepath.Join(repoDir, "packages", "b", "broken", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	panic("boom")
}
`)
	runGitLine(t, repoDir, "init", "-q", "-b", "master")
	runGitLine(t, repoDir, "config", "user.email", "test@example.com")
	runGitLine(t, repoDir, "config", "user.name", "test")
	runGitLine(t, repoDir, "add", "-A")
	runGitLine(t, repoDir, "commit", "-q", "-m", "v1")
	commit, err := repo.GetCurrentCommit(repoDir)
	if err != nil {
		t.Fatal(err)
	}

	result, err := loadPkgShow("official", "broken", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Trusted || result.Warning == "" {
		t.Fatalf("untrusted result = %+v", result)
	}

	store, err := openTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Trust(trustKindRepo, "official", "", commit); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPkgShow("official", "broken", ""); err == nil {
		t.Fatal("trusted script failure did not fail the command")
	}
}

func TestWritePkgJSON(t *testing.T) {
	var out bytes.Buffer
	result := &pkgShowResult{
		SchemaVersion: 1,
		Repo:          "official",
		Name:          "zlib",
		Add:           `p.AddRequires("official/zlib >= 1.3")`,
	}
	if err := writePkgJSON(&out, result); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["schemaVersion"].(float64) != 1 {
		t.Fatalf("decoded = %#v", decoded)
	}
	if !strings.Contains(out.String(), `>= 1.3`) {
		t.Fatalf("JSON escaped the constraint: %s", out.String())
	}
}

func writePkgTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
