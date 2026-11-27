package main

import (
	"context"
	"encoding/binary"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/pipeline"
	repoPkg "github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

// Windows 8.3 names and Unix directory symlinks give one project two spellings.
func TestBuildInstallFromProjectAlias(t *testing.T) {
	root := canonicalTempDir(t)
	project := filepath.Join(root, "Project With A Long Name")
	state := filepath.Join(root, "state")
	writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "healthy", "toolchain.json"), "healthy")
	writeExtensionFile(t, filepath.Join(project, "vmake.json"), `{"roots":["//","//lib"]}`)
	for _, dir := range []string{project, filepath.Join(project, "lib")} {
		writeExtensionFile(t, filepath.Join(dir, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("item").SetKind(api.TargetStatic).SetPrebuilt("input.a")
    })
}
`)
		writeExtensionFile(t, filepath.Join(dir, "input.a"), "fixture")
	}
	alias := filepath.Join(root, "alias")
	if runtime.GOOS == "windows" {
		// Ask cmd for the real DOS alias without inserting any user path into a command.
		cmd := exec.Command("cmd.exe", "/d", "/u", "/c", "for %I in (.) do @echo %~fsI")
		cmd.Dir = project
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		// /u writes UTF-16LE even when the path uses characters outside the OEM code page.
		units := make([]uint16, len(out)/2)
		n, err := binary.Decode(out, binary.LittleEndian, units)
		if err != nil || n != len(out) {
			t.Fatalf("invalid UTF-16LE short path: %d bytes consumed, %d supplied, %v", n, len(out), err)
		}
		alias = strings.TrimSpace(string(utf16.Decode(units)))
		if strings.EqualFold(alias, project) {
			t.Skip("volume does not supply an 8.3 alias")
		}
	} else if err := os.Symlink(project, alias); err != nil {
		t.Fatal(err)
	}

	for i, invocation := range []struct {
		cwd  string
		args []string
	}{
		{root, []string{"-C", alias}},
		{project, nil},
		{filepath.Join(alias, "lib"), nil},
	} {
		args := append(invocation.args, "build", "-i", "-y", "--toolchain", "healthy")
		stdout, stderr, code := runSubprocess(t, invocation.cwd, state, []string{"PWD=" + invocation.cwd, "VMAKE_TRUST_ALL=1"}, args...)
		if code != 0 {
			t.Fatalf("build from %q with %v exited %d:\n%s%s", invocation.cwd, args, code, stdout, stderr)
		}
		manifest, err := pipeline.LoadInstallManifest(filepath.Join(project, "install", "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		got := make(map[string]string)
		for _, pkg := range manifest.Packages {
			got[pkg.Name] = pkg.Path
		}
		if !maps.Equal(got, map[string]string{"//": ".", "//lib": "lib"}) {
			t.Fatalf("manifest package paths = %v", got)
		}
		if i > 0 {
			summary := loadBuildReport(t, project).Summary
			if summary == nil || summary.TargetsBuilt != 0 || summary.TargetsUpToDate != 2 {
				t.Fatalf("unchanged project through another path: %+v", summary)
			}
		}
	}
}

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
