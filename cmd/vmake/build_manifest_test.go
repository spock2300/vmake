package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/pipeline"
	repoPkg "github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestManifestRecordsNativeContainerWithoutMemberDuplication(t *testing.T) {
	r := resolver.NewResolver(nil, t.TempDir())
	r.Graph().Order = []string{"native/root/sub", "native/root"}
	r.Graph().Containers["native/root"] = &resolver.ContainerNode{
		ID:       "native/root",
		Repo:     "native",
		Kind:     resolver.SourceKindNative,
		URL:      "https://example.invalid/root.git",
		Versions: map[string]string{"1.0.0": "v1.0.0"},
		Version:  "1.0.0",
		Commit:   "abc123",
	}
	for _, item := range []struct {
		id, rel string
	}{
		{"native/root", ""},
		{"native/root/sub", "sub"},
	} {
		src := buildscript.NewSource(item.id, "build.go", t.TempDir(), api.SourceRemote)
		src.Container = "native/root"
		src.RelPath = item.rel
		r.Graph().Packages[item.id] = resolver.NewPackageNode(item.id, "native/root", item.rel, resolver.SourceKindNative, src, api.NewPackage())
	}
	ctx := &RuntimeContext{Resolver: r, DepGraph: r.Graph()}
	result := &BuildResult{InstalledPkgs: map[string]*api.InstalledPackage{
		"native/root":     {Version: "1.0.0"},
		"native/root/sub": {Version: "1.0.0"},
	}}
	prefix := t.TempDir()
	if err := pipeline.WriteManifest(ctx, result, prefix); err != nil {
		t.Fatal(err)
	}
	var manifest installManifest
	if err := jsonio.Load(filepath.Join(prefix, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Packages) != 1 || manifest.Packages[0].Name != "native/root" || manifest.Packages[0].Source != "native" {
		t.Fatalf("manifest must pin the native container only: %+v", manifest.Packages)
	}
}

func TestCheckoutManifestLocalsSkipsManagedGitSources(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	runGitLine(t, project, "init", "-q", "-b", "master")
	runGitLine(t, project, "config", "user.email", "test@example.com")
	runGitLine(t, project, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(project, "main.c"), []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitLine(t, project, "add", "-A")
	runGitLine(t, project, "commit", "-q", "-m", "v1")
	first, err := repoPkg.GetCurrentCommit(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "main.c"), []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitLine(t, project, "add", "-A")
	runGitLine(t, project, "commit", "-q", "-m", "v2")

	managed := resolver.NewPackageNode("//managed", api.LocalContainerID, "managed", resolver.SourceKindLocal,
		buildscript.NewSource("//managed", "build.go", project, api.SourceLocal), api.NewPackage().
			SetGit("https://example.invalid/upstream.git"))
	plain := resolver.NewPackageNode("//plain", api.LocalContainerID, "plain", resolver.SourceKindLocal,
		buildscript.NewSource("//plain", "build.go", project, api.SourceLocal), api.NewPackage())
	ctx := &RuntimeContext{Context: context.Background(), WorkDir: root, DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
		"//managed": managed,
		"//plain":   plain,
	}}}

	rel, err := filepath.Rel(root, project)
	if err != nil {
		t.Fatal(err)
	}
	mf := installManifest{Packages: []installManifestEntry{
		{Name: "//managed", Source: "local", Ref: strings.Repeat("0", 40), Path: rel},
		{Name: "//plain", Source: "local", Ref: first, Path: rel},
	}}
	manifestPath := filepath.Join(root, "manifest.json")
	if err := jsonio.Save(manifestPath, &mf); err != nil {
		t.Fatal(err)
	}

	if err := pipeline.CheckoutManifestLocals(ctx, &mf); err != nil {
		t.Fatalf("managed git source must not be checked out: %v", err)
	}
	commit, err := repoPkg.GetCurrentCommit(project)
	if err != nil {
		t.Fatal(err)
	}
	if commit != first {
		t.Fatalf("plain local package commit = %s, want %s", commit, first)
	}
	if data, err := os.ReadFile(filepath.Join(project, "main.c")); err != nil || string(data) != "v1\n" {
		t.Fatalf("plain local package content = %q, %v", data, err)
	}
}

func TestResolveManifestEntryPathUsesProjectRoot(t *testing.T) {
	root := canonicalTempDir(t)
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	got, err := pipeline.ResolveManifestEntryPath(project, filepath.Join("src", "pkg"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, "src", "pkg"); got != want {
		t.Fatalf("resolved path = %q, want %q", got, want)
	}
	got, err = pipeline.ResolveManifestEntryPath(project, filepath.Join("src", "..", "lib"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, "lib"); got != want {
		t.Fatalf("normalized path = %q, want %q", got, want)
	}
	if _, err := pipeline.ResolveManifestEntryPath(project, filepath.Join("..", "outside")); err == nil {
		t.Fatal("path escaping the project was accepted")
	}
}

func TestCheckoutLocalRejectsEscapingPaths(t *testing.T) {
	project := t.TempDir()
	for _, path := range []string{filepath.Join("..", "outside"), ""} {
		entry := installManifestEntry{Name: "//pkg", Source: "local", Ref: strings.Repeat("0", 40), Path: path}
		if err := pipeline.CheckoutManifestLocalEntry(project, nil, entry); err == nil {
			t.Errorf("checkoutLocal accepted manifest path %q", path)
		}
	}
}

func TestCheckoutLocalAcceptsRootPackage(t *testing.T) {
	project := t.TempDir()
	runGitLine(t, project, "init", "-q", "-b", "master")
	runGitLine(t, project, "config", "user.email", "test@example.com")
	runGitLine(t, project, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(project, "build.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitLine(t, project, "add", "-A")
	runGitLine(t, project, "commit", "-q", "-m", "v1")
	first, err := repoPkg.GetCurrentCommit(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "build.go"), []byte("package main\n\n// v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitLine(t, project, "add", "-A")
	runGitLine(t, project, "commit", "-q", "-m", "v2")

	entry := installManifestEntry{Name: "//", Source: "local", Ref: first, Path: "."}
	if err := pipeline.CheckoutManifestLocalEntry(project, nil, entry); err != nil {
		t.Fatalf("checkoutLocal rejected a root package manifest entry: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(project, "build.go")); err != nil || string(data) != "package main\n" {
		t.Fatalf("root package was not restored: %q, %v", data, err)
	}
}

func TestCheckoutManifestLocalsRejectsPathNotMatchingPackage(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	ctx := &RuntimeContext{Context: context.Background(), WorkDir: root, DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
		"//plain": resolver.NewPackageNode("//plain", api.LocalContainerID, "plain", resolver.SourceKindLocal,
			buildscript.NewSource("//plain", "build.go", project, api.SourceLocal), api.NewPackage()),
	}}}
	entry := installManifestEntry{Name: "//plain", Source: "local", Ref: strings.Repeat("0", 40), Path: "other"}
	if err := pipeline.CheckoutManifestLocals(ctx, &installManifest{Packages: []installManifestEntry{entry}}); err == nil {
		t.Fatal("manifest path not matching the package source was accepted")
	}
}

func TestCheckoutLocalSkipsManagedGitSourceInsteadOfCheckingOutProjectRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	project := t.TempDir()
	runGitLine(t, project, "init", "-q", "-b", "master")
	runGitLine(t, project, "config", "user.email", "test@example.com")
	runGitLine(t, project, "config", "user.name", "test")
	buildGo := `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetGit("https://example.invalid/upstream.git")
}
`
	if err := os.WriteFile(filepath.Join(project, "build.go"), []byte(buildGo), 0644); err != nil {
		t.Fatal(err)
	}
	runGitLine(t, project, "add", "-A")
	runGitLine(t, project, "commit", "-q", "-m", "v1")
	head, err := repoPkg.GetCurrentCommit(project)
	if err != nil {
		t.Fatal(err)
	}

	entry := installManifestEntry{Name: "//", Source: "local", Ref: strings.Repeat("0", 40), Path: "."}
	if err := pipeline.CheckoutManifestLocalEntry(project, nil, entry); err != nil {
		t.Fatalf("checkoutLocal failed on managed SetGit local package: %v", err)
	}
	after, err := repoPkg.GetCurrentCommit(project)
	if err != nil || after != head {
		t.Fatalf("project repo commit = %q (err=%v), want %q", after, err, head)
	}

	if err := os.WriteFile(filepath.Join(project, "build.go"), []byte("package main\nfunc Main(\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.CheckoutManifestLocalEntry(project, nil, entry); err == nil || !strings.Contains(err.Error(), "load build.go") {
		t.Fatalf("expected load build.go error, got %v", err)
	}
}
