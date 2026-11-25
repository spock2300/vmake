package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
)

func TestBuildPackageSourceUsesCachedNativeSrcDirAsModuleRoot(t *testing.T) {
	depsDir := t.TempDir()
	srcDir := filepath.Join(depsDir, "native", "libfoo", "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module example.com/libfoo\n\ngo 1.26\n"), 0644); err != nil {
		t.Fatal(err)
	}
	buildGo := filepath.Join(srcDir, "build.go")
	if err := os.WriteFile(buildGo, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	src, err := BuildPackageSource(nil, "native", "libfoo", buildGo, "deadbeef", true, false)
	if err != nil {
		t.Fatalf("BuildPackageSource: %v", err)
	}
	if src.Script.ModuleRoot != srcDir || src.Script.ModulePath != "example.com/libfoo" {
		t.Fatalf("Script = %+v, want ModuleRoot=%q ModulePath=example.com/libfoo", src.Script, srcDir)
	}
}

func TestEnvLoadScriptPrefersInjectedLoader(t *testing.T) {
	called := false
	env := Env{LoadScript: func(buildscript.Source) (*api.Package, error) {
		called = true
		return api.NewPackage().SetName("fake"), nil
	}}
	pkg, err := env.loadScript(buildscript.Source{})
	if err != nil {
		t.Fatalf("loadScript: %v", err)
	}
	if !called || pkg == nil {
		t.Fatalf("injected loader was not used: called=%v pkg=%v", called, pkg)
	}
}

func TestCatalogScriptUsesTheInjectedLoader(t *testing.T) {
	c := Open(Env{LoadScript: func(buildscript.Source) (*api.Package, error) {
		return nil, errors.New("interpretation failed")
	}})
	if _, err := c.Script(buildscript.Source{}); err == nil {
		t.Fatal("Catalog.Script swallowed the loader error")
	}
}

type fakeRepo struct {
	exists   map[string]bool
	native   map[string]bool
	packages map[string][]string
	buildGo  map[string]string
	paths    map[string]string
	commit   string
	lockDir  string
}

func (f *fakeRepo) Exists(name string) bool   { return f.exists[name] }
func (f *fakeRepo) IsNative(name string) bool { return f.native[name] }
func (f *fakeRepo) ListPackages(repo string) ([]string, error) {
	return f.packages[repo], nil
}
func (f *fakeRepo) FindPackageGo(repo, name string) (string, error) {
	path, ok := f.buildGo[repo+"/"+name]
	if !ok {
		return "", errors.New("package not found")
	}
	return path, nil
}
func (f *fakeRepo) Path(name string) string { return f.paths[name] }
func (f *fakeRepo) Lock(name string, exclusive bool) (*flock.FileLock, error) {
	return flock.Acquire(filepath.Join(f.lockDir, name+".lock"))
}

func newFakeRegistry(t *testing.T, withBuildGo bool) *fakeRepo {
	t.Helper()
	root := t.TempDir()
	repoRoot := filepath.Join(root, "official")
	buildGo := filepath.Join(repoRoot, "packages", "z", "zlib", "build.go")
	if err := os.MkdirAll(filepath.Dir(buildGo), 0755); err != nil {
		t.Fatal(err)
	}
	content := "package main\n"
	if err := os.WriteFile(buildGo, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	f := &fakeRepo{
		exists:   map[string]bool{"official": true},
		native:   map[string]bool{},
		packages: map[string][]string{"official": {"zlib"}},
		buildGo:  map[string]string{"official/zlib": buildGo},
		paths:    map[string]string{"official": repoRoot},
		commit:   "0123456789abcdef0123456789abcdef01234567",
		lockDir:  root,
	}
	if !withBuildGo {
		f.buildGo = map[string]string{}
	}
	return f
}

func fakeEnv(repo RepoAccess, projectDir string) Env {
	return Env{
		Repo:        repo,
		ProjectDir:  projectDir,
		CacheDir:    projectDir,
		Commit:      func(string) (string, error) { return "deadbeef", nil },
		RepoTrusted: func(string) (bool, error) { return true, nil },
		LoadScript: func(src buildscript.Source) (*api.Package, error) {
			pkg := api.NewPackage().SetName("zlib")
			pkg.SetDescription("Compression library")
			return pkg, nil
		},
	}
}

func TestSearchRegistryRepoReportsAMetadataEntry(t *testing.T) {
	repo := newFakeRegistry(t, true)
	entries, warnings := Open(fakeEnv(repo, "")).Search("official", SearchOptions{Details: true}, nil)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(entries) != 1 || entries[0].Name != "zlib" || entries[0].Source != "registry" {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Description != "Compression library" {
		t.Fatalf("metadata was not read through the injected loader: %+v", entries[0])
	}
}

func TestSearchRegistryRepoReportsNothingForAMissingRepository(t *testing.T) {
	repo := newFakeRegistry(t, true)
	entries, _ := Open(fakeEnv(repo, "")).Search("missing", SearchOptions{}, nil)
	if len(entries) != 0 {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestSearchNativeRepoNeedsAProject(t *testing.T) {
	repo := newFakeRegistry(t, true)
	repo.native["myrepo"] = true
	entries, warnings := Open(fakeEnv(repo, "")).Search("myrepo", SearchOptions{}, nil)
	if len(entries) != 0 || len(warnings) != 1 {
		t.Fatalf("entries = %+v, warnings = %v", entries, warnings)
	}
	if !strings.Contains(warnings[0], "run inside a project") {
		t.Fatalf("warning = %q", warnings[0])
	}
}

func TestSearchNativeRepoListsCachedPackages(t *testing.T) {
	project := t.TempDir()
	srcDir := filepath.Join(project, ".vmake_deps", "native", "libfoo", "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "build.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := newFakeRegistry(t, true)
	repo.exists["native"] = true
	repo.native["native"] = true
	entries, warnings := Open(fakeEnv(repo, project)).Search("native", SearchOptions{}, nil)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(entries) != 1 || entries[0].Name != "libfoo" || entries[0].Source != "native" || !entries[0].Cached {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestShowRegistryPackage(t *testing.T) {
	repo := newFakeRegistry(t, true)
	res, isUsage, err := Open(fakeEnv(repo, "")).Show("official", "zlib", "")
	if err != nil || isUsage {
		t.Fatalf("Show: err=%v usage=%v", err, isUsage)
	}
	if res == nil || res.Repo != "official" || res.Name != "zlib" || res.Source != "registry" {
		t.Fatalf("result = %+v", res)
	}
}

func TestShowUnknownRepositoryIsUsageError(t *testing.T) {
	repo := newFakeRegistry(t, true)
	_, isUsage, err := Open(fakeEnv(repo, "")).Show("missing", "zlib", "")
	if err == nil {
		t.Fatal("Show(missing/zlib) reported success")
	}
	if !isUsage {
		t.Fatalf("unknown repository was not reported as a usage error: %v", err)
	}
}

func TestShowUnknownPackageFails(t *testing.T) {
	repo := newFakeRegistry(t, true)
	if _, _, err := Open(fakeEnv(repo, "")).Show("official", "nope", ""); err == nil {
		t.Fatal("Show(official/nope) reported success")
	}
}

func TestSortedPackageVersions(t *testing.T) {
	pkg := api.NewPackage().
		AddVersion("1.2.13", "v1.2.13").
		AddVersion("1.3.1", "v1.3.1").
		AddVersion("1.0.0", "v1.0.0")
	got := SortedPackageVersions(pkg)
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
		if got := AddRequiresSnippet("official", "zlib", test.version); got != test.want {
			t.Errorf("version %q: %q, want %q", test.version, got, test.want)
		}
	}
}
