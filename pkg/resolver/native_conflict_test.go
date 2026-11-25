package resolver

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/repo"
)

func newTrustedTestResolver(repoMgr *repo.RepoManager, depsDir string) *Resolver {
	r := NewResolver(repoMgr, depsDir)
	r.SetTrustChecker(func(buildscript.Source) error { return nil })
	return r
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeNativeRepoWithVersions(t *testing.T) string {
	t.Helper()
	root := canonicalTempDir(t)
	pkgDir := filepath.Join(root, "conflictpkg.git")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, pkgDir, "init", "-q", "-b", "master")
	gitCmd(t, pkgDir, "config", "user.email", "test@example.com")
	gitCmd(t, pkgDir, "config", "user.name", "test")

	buildGo := `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "build.go"), []byte(buildGo), 0644); err != nil {
		t.Fatal(err)
	}
	verFile := filepath.Join(pkgDir, "version.txt")
	if err := os.WriteFile(verFile, []byte("1.2.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, pkgDir, "add", "-A")
	gitCmd(t, pkgDir, "commit", "-q", "-m", "v1.2.0")
	gitCmd(t, pkgDir, "tag", "v1.2.0")

	if err := os.WriteFile(verFile, []byte("1.9.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, pkgDir, "add", "-A")
	gitCmd(t, pkgDir, "commit", "-q", "-m", "v1.9.0")
	gitCmd(t, pkgDir, "tag", "v1.9.0")

	return pkgDir
}

func writeTaggedNativeModule(t *testing.T, bareDest string, files map[string]string, tag string) {
	t.Helper()
	work := canonicalTempDir(t)
	writeProjectFiles(t, work, files)
	gitCmd(t, work, "init", "-q", "-b", "master")
	gitCmd(t, work, "config", "user.email", "test@example.com")
	gitCmd(t, work, "config", "user.name", "test")
	gitCmd(t, work, "add", "-A")
	gitCmd(t, work, "commit", "-q", "-m", tag)
	gitCmd(t, work, "tag", tag)
	if err := os.MkdirAll(filepath.Dir(bareDest), 0755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, work, "clone", "-q", "--bare", ".", bareDest)
}

func writeLocalProject(t *testing.T, modules, sources map[string]string, reqs ...string) string {
	t.Helper()
	dir := canonicalTempDir(t)
	manifest := buildscript.Manifest{
		Modules: modules,
		Sources: sources,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vmake.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	quoted := make([]string, len(reqs))
	for i, req := range reqs {
		quoted[i] = fmt.Sprintf("%q", req)
	}
	body := `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
`
	if len(quoted) > 0 {
		body += fmt.Sprintf("\tp.OnRequire(func(ctx *api.RequireContext) {\n\t\tctx.AddRequires(%s)\n\t})\n", strings.Join(quoted, ", "))
	}
	body += "}\n"
	if err := os.WriteFile(filepath.Join(dir, "build.go"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newNativeTestResolver(t *testing.T, pkgGitURL string) *Resolver {
	t.Helper()
	reposDir := canonicalTempDir(t)
	depsDir := canonicalTempDir(t)
	cacheDir := canonicalTempDir(t)

	repoMgr := repo.NewRepoManager(reposDir)
	template := strings.TrimSuffix(pkgGitURL, "conflictpkg.git") + "{name}.git"
	if err := repoMgr.AddNative("testnative", template); err != nil {
		t.Fatal(err)
	}

	r := newTrustedTestResolver(repoMgr, depsDir)
	r.SetSourceManager(repo.NewSourceManager(depsDir, cacheDir))
	t.Cleanup(r.Close)
	return r
}

func TestResolveNativeDiamondIntersectionSelectsCommonVersion(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	url := writeNativeRepoWithVersions(t)
	baseDir := filepath.Dir(url)
	writeTaggedNativeModule(t, filepath.Join(baseDir, "mod_a.git"), map[string]string{
		"vmake.json": `{"modules": {"testnative/conflictpkg": ">=1.2"}}`,
		"build.go":   requirePackageMain("testnative/conflictpkg"),
	}, "v1.0.0")
	writeTaggedNativeModule(t, filepath.Join(baseDir, "mod_b.git"), map[string]string{
		"vmake.json": `{"modules": {"testnative/conflictpkg": "<1.5"}}`,
		"build.go":   requirePackageMain("testnative/conflictpkg"),
	}, "v1.0.0")

	r := newNativeTestResolver(t, url)
	project := writeLocalProject(t, map[string]string{
		"testnative/mod_a": ">=1.0.0",
		"testnative/mod_b": ">=1.0.0",
	}, nil, "testnative/mod_a", "testnative/mod_b")

	if err := r.ResolveProject(project, nil); err != nil {
		t.Fatalf("intersectable diamond should resolve 1.2.0: %v", err)
	}
	c := r.Graph().Containers["testnative/conflictpkg"]
	if c == nil || c.Version != "1.2.0" {
		t.Fatalf("container = %+v, want version 1.2.0", c)
	}
}

func TestResolveNativeDiamondDisjointConflictFails(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	url := writeNativeRepoWithVersions(t)
	baseDir := filepath.Dir(url)
	writeTaggedNativeModule(t, filepath.Join(baseDir, "mod_a.git"), map[string]string{
		"vmake.json": `{"modules": {"testnative/conflictpkg": ">=1.6"}}`,
		"build.go":   requirePackageMain("testnative/conflictpkg"),
	}, "v1.0.0")
	writeTaggedNativeModule(t, filepath.Join(baseDir, "mod_b.git"), map[string]string{
		"vmake.json": `{"modules": {"testnative/conflictpkg": "<1.5"}}`,
		"build.go":   requirePackageMain("testnative/conflictpkg"),
	}, "v1.0.0")

	r := newNativeTestResolver(t, url)
	project := writeLocalProject(t, map[string]string{
		"testnative/mod_a": ">=1.0.0",
		"testnative/mod_b": ">=1.0.0",
	}, nil, "testnative/mod_a", "testnative/mod_b")

	err := r.ResolveProject(project, nil)
	if err == nil {
		t.Fatal("disjoint diamond constraints should fail resolution")
	}
	for _, want := range []string{"conflicting version constraints", "testnative/conflictpkg", "<1.5"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

func TestResolveNativeConfigPinReusesTreeOffline(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	url := writeNativeRepoWithVersions(t)
	depsDir, cacheDir := canonicalTempDir(t), canonicalTempDir(t)
	repoMgr := repo.NewRepoManager(canonicalTempDir(t))
	template := strings.TrimSuffix(url, "conflictpkg.git") + "{name}.git"
	if err := repoMgr.AddNative("testnative", template); err != nil {
		t.Fatal(err)
	}

	project := writeLocalProject(t, map[string]string{"testnative/conflictpkg": ">=1.2"}, nil, "testnative/conflictpkg")

	r := newTrustedTestResolver(repoMgr, depsDir)
	r.SetSourceManager(repo.NewSourceManager(depsDir, cacheDir))
	if err := r.ResolveProject(project, nil); err != nil {
		t.Fatal(err)
	}
	c := r.Graph().Containers["testnative/conflictpkg"]
	if c == nil || c.Commit == "" {
		t.Fatalf("native container incomplete: %+v", c)
	}

	// A config pin with a matching lock must reuse the materialized tree even
	// when the cache directory is gone.
	if err := os.RemoveAll(cacheDir); err != nil {
		t.Fatal(err)
	}
	pinned := newTrustedTestResolver(repoMgr, depsDir)
	pinned.SetSourceManager(repo.NewSourceManager(depsDir, cacheDir))
	lock := lockfile.New()
	lock.SetContainer("testnative/conflictpkg", &lockfile.LockedContainer{Version: "1.9.0", Commit: c.Commit, Source: "native"})
	pinned.SetLockfile(lock, false)
	pinned.SetConfigPins(map[string]string{"testnative/conflictpkg": "1.9.0"})
	if err := pinned.ResolveProject(project, nil); err != nil {
		t.Fatalf("offline config pin: %v", err)
	}
	pinnedC := pinned.Graph().Containers["testnative/conflictpkg"]
	if pinnedC == nil || pinnedC.Version != "1.9.0" {
		t.Fatalf("pinned resolution = %+v", pinnedC)
	}
}

func TestResolveNativeDiamondResolvedByPinAndRootExactOverride(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	url := writeNativeRepoWithVersions(t)
	baseDir := filepath.Dir(url)
	writeTaggedNativeModule(t, filepath.Join(baseDir, "mod_a.git"), map[string]string{
		"vmake.json": `{"modules": {"testnative/conflictpkg": ">=1.6"}}`,
		"build.go":   requirePackageMain("testnative/conflictpkg"),
	}, "v1.0.0")
	writeTaggedNativeModule(t, filepath.Join(baseDir, "mod_b.git"), map[string]string{
		"vmake.json": `{"modules": {"testnative/conflictpkg": "<1.5"}}`,
		"build.go":   requirePackageMain("testnative/conflictpkg"),
	}, "v1.0.0")

	t.Run("deps_pin_resolves_disjoint_diamond", func(t *testing.T) {
		r := newNativeTestResolver(t, url)
		r.SetConfigPins(map[string]string{"testnative/conflictpkg": "1.2.0"})
		project := writeLocalProject(t, map[string]string{
			"testnative/mod_a": ">=1.0.0",
			"testnative/mod_b": ">=1.0.0",
		}, nil, "testnative/mod_a", "testnative/mod_b")
		if err := r.ResolveProject(project, nil); err != nil {
			t.Fatalf("deps pin should resolve disjoint diamond: %v", err)
		}
		if c := r.Graph().Containers["testnative/conflictpkg"]; c == nil || c.Version != "1.2.0" {
			t.Fatalf("container = %+v, want 1.2.0", c)
		}
	})

	t.Run("root_exact_override_resolves_disjoint_diamond", func(t *testing.T) {
		r := newNativeTestResolver(t, url)
		project := writeLocalProject(t, map[string]string{
			"testnative/mod_a":       ">=1.0.0",
			"testnative/mod_b":       ">=1.0.0",
			"testnative/conflictpkg": "=1.2.0",
		}, nil, "testnative/mod_a", "testnative/mod_b")
		if err := r.ResolveProject(project, nil); err != nil {
			t.Fatalf("root exact override should resolve disjoint diamond: %v", err)
		}
		if c := r.Graph().Containers["testnative/conflictpkg"]; c == nil || c.Version != "1.2.0" {
			t.Fatalf("container = %+v, want 1.2.0", c)
		}
	})

	t.Run("mixed_commit_and_semver_fails", func(t *testing.T) {
		const sha = "0123456789abcdef0123456789abcdef01234567"
		if _, _, err := SplitConstraints("testnative/conflictpkg", []string{sha, ">=1.6"}); err == nil || !strings.Contains(err.Error(), "conflicting commit constraint") {
			t.Fatalf("expected conflict error for mixed commit+semver, got %v", err)
		}
	})
}
