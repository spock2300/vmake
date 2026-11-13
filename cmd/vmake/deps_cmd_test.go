package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/repo"
)

func createDepsTestGitRepo(t *testing.T, dir string, files map[string]string, tag string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	runGitLine(t, dir, "init", "-q", "-b", "main")
	runGitLine(t, dir, "config", "user.email", "test@example.com")
	runGitLine(t, dir, "config", "user.name", "test")
	for name, body := range files {
		writeExtensionFile(t, filepath.Join(dir, filepath.FromSlash(name)), body)
	}
	runGitLine(t, dir, "add", "-A")
	runGitLine(t, dir, "commit", "-q", "-m", "init")
	if tag != "" {
		runGitLine(t, dir, "tag", tag)
	}
	commit, err := repo.GetCurrentCommit(dir)
	if err != nil {
		t.Fatal(err)
	}
	return commit
}

func commitDepsTestGitRepo(t *testing.T, dir string, files map[string]string, tag string) string {
	t.Helper()
	for name, body := range files {
		writeExtensionFile(t, filepath.Join(dir, filepath.FromSlash(name)), body)
	}
	runGitLine(t, dir, "add", "-A")
	runGitLine(t, dir, "commit", "-q", "-m", "update")
	if tag != "" {
		runGitLine(t, dir, "tag", tag)
	}
	commit, err := repo.GetCurrentCommit(dir)
	if err != nil {
		t.Fatal(err)
	}
	return commit
}

func TestDepsLifecycleAndLockedBuild(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	state := filepath.Join(root, "state")
	cacheDir := filepath.Join(root, "cache")
	project := filepath.Join(root, "project")
	writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "healthy", "toolchain.json"), "healthy")

	libUpstream := filepath.Join(root, "upstreams", "libfoo")
	commitV1 := createDepsTestGitRepo(t, libUpstream, map[string]string{
		"build.go": `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("foo").SetKind(api.TargetVoid)
	})
}
`,
		"foo.h": "#define FOO_VER 1\n",
	}, "v1.0.0")
	commitV2 := commitDepsTestGitRepo(t, libUpstream, map[string]string{
		"foo.h": "#define FOO_VER 2\n",
	}, "v1.1.0")

	untaggedUpstream := filepath.Join(root, "upstreams", "blob")
	blobCommit1 := createDepsTestGitRepo(t, untaggedUpstream, map[string]string{
		"data.txt": "blob-v1\n",
	}, "")

	run := func(args ...string) (string, error) {
		t.Helper()
		return extensionCommandEnv(t, state, project, []string{
			"VMAKE_TRUST_ALL=1",
			"VMAKE_CACHE=" + cacheDir,
		}, args...)
	}
	mustRun := func(args ...string) string {
		t.Helper()
		out, err := run(args...)
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
		return out
	}

	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	mustRun("repo", "add", "--native", "testnative", filepath.ToSlash(filepath.Join(root, "upstreams"))+"/{name}")

	writeExtensionFile(t, filepath.Join(project, "blob", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("`+filepath.ToSlash(untaggedUpstream)+`")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("blob").SetKind(api.TargetVoid)
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.SetRoot(true)
	p.AddRequires("testnative/libfoo >= 1.0.0", "blob")
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetVoid).UseRequires()
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "configs", "default.json"), `{"version":"1","global":{"toolchain":"healthy","mode":"release"},"entries":{}}`)

	if out, err := run("build", "--locked"); err == nil || !strings.Contains(out, "--locked") {
		t.Fatalf("expected --locked to fail without lockfile, got: %v\n%s", err, out)
	}

	mustRun("deps", "sync")
	lock, err := lockfile.Load(filepath.Join(project, ".vmake", "vmake.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := lock.Get("testnative/libfoo"); !ok || got.Version != "1.1.0" || got.Commit != commitV2 {
		t.Fatalf("deps sync libfoo = %+v, want 1.1.0/%s", got, commitV2)
	}

	mustRun("deps", "pin", "testnative/libfoo@1.0.0")
	pins, err := config.LoadProjectPins(project)
	if err != nil || pins["testnative/libfoo"] != "1.0.0" {
		t.Fatalf("pins = %+v, %v", pins, err)
	}
	if out, err := run("deps", "update", "testnative/libfoo"); err == nil || !strings.Contains(out, "pinned to 1.0.0") {
		t.Fatalf("expected deps update on pinned package to fail, got: %v\n%s", err, out)
	}

	lock, err = lockfile.Load(filepath.Join(project, ".vmake", "vmake.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := lock.Get("testnative/libfoo"); !ok || got.Version != "1.0.0" || got.Commit != commitV1 {
		t.Fatalf("locked libfoo = %+v", got)
	}
	if got, ok := lock.Get("blob"); !ok || got.Version != "" || got.Commit != blobCommit1 || got.Source != "git" {
		t.Fatalf("locked untagged blob = %+v, want empty version and commit %s", got, blobCommit1)
	}

	mustRun("build", "--locked")

	statusJSON := mustRun("deps", "status", "--json")
	var statusEnv depsStatusEnvelope
	if err := json.Unmarshal([]byte(statusJSON), &statusEnv); err != nil {
		t.Fatalf("unmarshal status JSON: %v\n%s", err, statusJSON)
	}
	if statusEnv.Command != "deps status" || !statusEnv.LockExists || len(statusEnv.Packages) != 2 {
		t.Fatalf("unexpected status envelope: %+v", statusEnv)
	}

	mustRun("deps", "unpin", "testnative/libfoo")
	pins, err = config.LoadProjectPins(project)
	if err != nil || len(pins) != 0 {
		t.Fatalf("pins after unpin = %+v, %v", pins, err)
	}
	lock, err = lockfile.Load(filepath.Join(project, ".vmake", "vmake.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := lock.Get("testnative/libfoo"); got == nil || got.Version != "1.0.0" {
		t.Fatalf("unpin should keep locked version 1.0.0, got %+v", got)
	}

	if out, err := run("deps", "update", "--refresh-repos", "--dry-run"); err == nil || !strings.Contains(out, "--refresh-repos cannot be combined with --dry-run") {
		t.Fatalf("expected --refresh-repos --dry-run to fail, got: %v\n%s", err, out)
	}
	if out, err := run("deps", "fetch", "testnative/libfoo", "--all"); err == nil || !strings.Contains(out, "cannot combine") {
		t.Fatalf("expected deps fetch <pkg> --all to fail, got: %v\n%s", err, out)
	}

	dryOut := mustRun("deps", "update", "--dry-run", "--json")
	var dryEnv depsUpdateEnvelope
	if err := json.Unmarshal([]byte(dryOut), &dryEnv); err != nil {
		t.Fatalf("unmarshal dry-run JSON: %v\n%s", err, dryOut)
	}
	if !dryEnv.DryRun || len(dryEnv.Changes) != 1 || dryEnv.Changes[0].Name != "testnative/libfoo" || dryEnv.Changes[0].NewVersion != "1.1.0" {
		t.Fatalf("unexpected dry-run result: %+v", dryEnv)
	}
	lock, _ = lockfile.Load(filepath.Join(project, ".vmake", "vmake.lock"))
	if got, _ := lock.Get("testnative/libfoo"); got == nil || got.Version != "1.0.0" {
		t.Fatalf("dry-run mutated vmake.lock: %+v", got)
	}
	if data, err := os.ReadFile(filepath.Join(project, ".vmake_deps", "testnative", "libfoo", "src", "foo.h")); err != nil || !strings.Contains(string(data), "FOO_VER 1") {
		t.Fatalf("dry-run mutated .vmake_deps tree: %q, %v", data, err)
	}

	mustRun("deps", "update", "testnative/libfoo")
	lock, _ = lockfile.Load(filepath.Join(project, ".vmake", "vmake.lock"))
	if got, _ := lock.Get("testnative/libfoo"); got == nil || got.Version != "1.1.0" || got.Commit != commitV2 {
		t.Fatalf("deps update libfoo = %+v", got)
	}

	if err := config.SaveProjectPins(project, map[string]string{"testnative/libfoo": "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if out, err := run("build", "--locked"); err == nil || !strings.Contains(out, "pinned to 1.0.0") {
		t.Fatalf("expected build --locked to fail on pin mismatch, got: %v\n%s", err, out)
	}
	if err := config.SaveProjectPins(project, nil); err != nil {
		t.Fatal(err)
	}

	mustRun("deps", "pin", "testnative/libfoo@"+commitV1)
	lock, _ = lockfile.Load(filepath.Join(project, ".vmake", "vmake.lock"))
	if got, _ := lock.Get("testnative/libfoo"); got == nil || got.Version != "" || got.Commit != commitV1 {
		t.Fatalf("commit pin lock = %+v, want empty version and commit %s", got, commitV1)
	}

	mustRun("distclean", "--purge-sources")
	mustRun("deps", "fetch", "--offline")
	if _, err := os.Stat(filepath.Join(project, ".vmake_deps", "testnative", "libfoo", "src", "foo.h")); err != nil {
		t.Fatalf("offline fetch did not materialize libfoo: %v", err)
	}
	mustRun("build", "--locked", "--offline")
}

func TestDepsFetchReportsSourcesBeforeApplyingPatches(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	state := filepath.Join(root, "state")
	cacheDir := filepath.Join(root, "cache")
	project := filepath.Join(root, "project")
	writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "healthy", "toolchain.json"), "healthy")

	upstream := filepath.Join(root, "upstreams", "libfoo")
	createDepsTestGitRepo(t, upstream, map[string]string{
		"build.go": `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.AddPatches("fix.patch")
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("foo").SetKind(api.TargetVoid)
	})
}
`,
		"noted.txt": "line\n",
		"fix.patch": "--- a/noted.txt\n+++ b/noted.txt\n@@ -1 +1,2 @@\n line\n+patched\n",
	}, "v1.0.0")

	run := func(args ...string) (string, error) {
		t.Helper()
		return extensionCommandEnv(t, state, project, []string{
			"VMAKE_TRUST_ALL=1",
			"VMAKE_CACHE=" + cacheDir,
		}, args...)
	}
	mustRun := func(args ...string) string {
		t.Helper()
		out, err := run(args...)
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
		return out
	}
	assertReportedBeforePatches := func(step, out string) {
		t.Helper()
		fetched := strings.Index(out, "Fetched testnative/libfoo")
		patched := strings.Index(out, "Applying patches for testnative/libfoo")
		if fetched < 0 || patched < 0 {
			t.Fatalf("%s: missing fetch or patch progress lines:\n%s", step, out)
		}
		if fetched > patched {
			t.Fatalf("%s: Fetched line printed after patch application:\n%s", step, out)
		}
	}

	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	mustRun("repo", "add", "--native", "testnative", filepath.ToSlash(filepath.Join(root, "upstreams"))+"/{name}")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.SetRoot(true)
	p.AddRequires("testnative/libfoo")
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetVoid).UseRequires()
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "configs", "default.json"), `{"version":"1","global":{"toolchain":"healthy","mode":"release"},"entries":{}}`)

	mustRun("deps", "sync")
	mustRun("distclean", "--purge-sources")

	assertReportedBeforePatches("first fetch", mustRun("deps", "fetch", "--offline"))
	data, err := os.ReadFile(filepath.Join(project, ".vmake_deps", "testnative", "libfoo", "src", "noted.txt"))
	if err != nil || !strings.Contains(string(data), "patched") {
		t.Fatalf("fetch did not apply the package patch: %q, %v", data, err)
	}

	applied := mustRun("deps", "fetch", "--offline")
	assertReportedBeforePatches("already applied", applied)
	if !strings.Contains(applied, "(already applied)") {
		t.Fatalf("second fetch did not report the applied patch:\n%s", applied)
	}

	jsonOut := mustRun("deps", "fetch", "--offline", "--json")
	var env depsFetchEnvelope
	if err := json.Unmarshal([]byte(jsonOut), &env); err != nil {
		t.Fatalf("unmarshal fetch JSON: %v\n%s", err, jsonOut)
	}
	if len(env.Packages) != 1 || env.Packages[0].Name != "testnative/libfoo" {
		t.Fatalf("fetch JSON packages = %+v", env.Packages)
	}
	if strings.Contains(jsonOut, "Fetched ") {
		t.Fatalf("--json printed the human-readable fetch report:\n%s", jsonOut)
	}
}

func TestDepsUpdateDryRunDoesNotApplyPatchesToWorkingTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	state := filepath.Join(root, "state")
	cacheDir := filepath.Join(root, "cache")
	project := filepath.Join(root, "project")
	writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "healthy", "toolchain.json"), "healthy")

	upstream := filepath.Join(root, "upstreams", "libfoo")
	createDepsTestGitRepo(t, upstream, map[string]string{
		"noted.txt": "line\n",
	}, "")

	run := func(args ...string) (string, error) {
		t.Helper()
		return extensionCommandEnv(t, state, project, []string{
			"VMAKE_TRUST_ALL=1",
			"VMAKE_CACHE=" + cacheDir,
		}, args...)
	}
	mustRun := func(args ...string) string {
		t.Helper()
		out, err := run(args...)
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
		return out
	}

	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	writeExtensionFile(t, filepath.Join(project, "foo", "fix.patch"), "--- a/noted.txt\n+++ b/noted.txt\n@@ -1 +1,2 @@\n line\n+patched-v1\n")
	writeExtensionFile(t, filepath.Join(project, "foo", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("`+filepath.ToSlash(upstream)+`")
		p.AddPatches("fix.patch")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("foo").SetKind(api.TargetVoid)
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.SetRoot(true)
	p.AddRequires("foo")
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetVoid).UseRequires()
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "configs", "default.json"), `{"version":"1","global":{"toolchain":"healthy","mode":"release"},"entries":{}}`)

	mustRun("deps", "sync")
	treeFile := filepath.Join(project, ".vmake_deps", "local", "foo", "src", "noted.txt")
	if data, err := os.ReadFile(treeFile); err != nil || !strings.Contains(string(data), "patched-v1") {
		t.Fatalf("initial sync did not apply patched-v1: %q, %v", data, err)
	}

	writeExtensionFile(t, filepath.Join(project, "foo", "fix.patch"), "--- a/noted.txt\n+++ b/noted.txt\n@@ -1 +1,2 @@\n line\n+patched-v2\n")
	mustRun("deps", "update", "--dry-run")

	if data, err := os.ReadFile(treeFile); err != nil || !strings.Contains(string(data), "patched-v1") || strings.Contains(string(data), "patched-v2") {
		t.Fatalf("deps update --dry-run mutated working tree: %q, %v", data, err)
	}
}
