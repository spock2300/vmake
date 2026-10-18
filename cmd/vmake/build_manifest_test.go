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
	repoPkg "github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestManifestRecordsNativeOwnerWithoutSubpackageVersions(t *testing.T) {
	r := resolver.NewResolver(nil, t.TempDir())
	r.Graph().Order = []string{"native/root/sub", "native/root"}
	for _, name := range r.Graph().Order {
		r.Graph().Packages[name] = resolver.NewPackageNode(name,
			buildscript.NewSource(name, "build.go", t.TempDir(), api.SourceRemote), api.NewPackage())
	}
	r.Graph().Packages["native/root"].WithNative("https://example.invalid/root.git", map[string]string{"1.0.0": "v1.0.0"}, "1.0.0")
	r.SubParents()["native/root/sub"] = "native/root"
	ctx := &RuntimeContext{Resolver: r, DepGraph: r.Graph()}
	result := &BuildResult{InstalledPkgs: map[string]*api.InstalledPackage{
		"native/root":     {Version: "1.0.0"},
		"native/root/sub": {Version: "1.0.0"},
	}}
	prefix := t.TempDir()
	if err := writeManifest(ctx, result, prefix); err != nil {
		t.Fatal(err)
	}
	var manifest installManifest
	if err := jsonio.Load(filepath.Join(prefix, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Packages) != 1 || manifest.Packages[0].Name != "native/root" || manifest.Packages[0].Source != "native" {
		t.Fatalf("manifest must pin the native owner only: %+v", manifest.Packages)
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

	managed := resolver.NewPackageNode("local/managed",
		buildscript.NewSource("local/managed", "build.go", project, api.SourceLocal), api.NewPackage().
			SetGit("https://example.invalid/upstream.git"))
	plain := resolver.NewPackageNode("local/plain",
		buildscript.NewSource("local/plain", "build.go", project, api.SourceLocal), api.NewPackage())
	ctx := &RuntimeContext{Context: context.Background(), WorkDir: root, DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
		"local/managed": managed,
		"local/plain":   plain,
	}}}

	rel, err := filepath.Rel(root, project)
	if err != nil {
		t.Fatal(err)
	}
	mf := installManifest{Packages: []installManifestEntry{
		{Name: "local/managed", Source: "local", Ref: strings.Repeat("0", 40), Path: rel},
		{Name: "local/plain", Source: "local", Ref: first, Path: rel},
	}}
	manifestPath := filepath.Join(root, "manifest.json")
	if err := jsonio.Save(manifestPath, &mf); err != nil {
		t.Fatal(err)
	}

	if err := checkoutManifestLocals(ctx, &mf); err != nil {
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
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveManifestEntryPath(project, filepath.Join("src", "pkg"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, "src", "pkg"); got != want {
		t.Fatalf("resolved path = %q, want %q", got, want)
	}
	got, err = resolveManifestEntryPath(project, filepath.Join("src", "..", "lib"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, "lib"); got != want {
		t.Fatalf("normalized path = %q, want %q", got, want)
	}
	if _, err := resolveManifestEntryPath(project, filepath.Join("..", "outside")); err == nil {
		t.Fatal("path escaping the project was accepted")
	}
}

func TestCheckoutLocalRejectsEscapingPaths(t *testing.T) {
	project := t.TempDir()
	for _, path := range []string{filepath.Join("..", "outside"), ""} {
		entry := installManifestEntry{Name: "local/pkg", Source: "local", Ref: strings.Repeat("0", 40), Path: path}
		if err := checkoutLocal(project, entry); err == nil {
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

	entry := installManifestEntry{Name: "app", Source: "local", Ref: first, Path: "."}
	if err := checkoutLocal(project, entry); err != nil {
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
		"local/plain": resolver.NewPackageNode("local/plain", buildscript.NewSource("local/plain", "build.go", project, api.SourceLocal), api.NewPackage()),
	}}}
	entry := installManifestEntry{Name: "local/plain", Source: "local", Ref: strings.Repeat("0", 40), Path: "other"}
	if err := checkoutManifestLocals(ctx, &installManifest{Packages: []installManifestEntry{entry}}); err == nil {
		t.Fatal("manifest path not matching the package source was accepted")
	}
}
