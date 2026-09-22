package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestMain(m *testing.M) {
	if os.Getenv("VMAKE_TEST_EXT_DIR") != "" && len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("VMake test compiler 1")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestExtensionBootstrapChild(t *testing.T) {
	dir := os.Getenv("VMAKE_TEST_EXT_DIR")
	if dir == "" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing command arguments")
	}
	vmakeDir = dir
	os.Args = append([]string{"vmake"}, os.Args[separator+1:]...)
	main()
	os.Exit(0)
}

func extensionCommand(t *testing.T, dir, workDir string, args ...string) (string, error) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commandArgs := append([]string{"-test.run=^TestExtensionBootstrapChild$", "--"}, args...)
	cmd := exec.Command(exe, commandArgs...)
	cmd.Env = append(os.Environ(), "VMAKE_TEST_EXT_DIR="+dir)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeExtensionFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeExtensionLoader(t *testing.T, repo string) {
	t.Helper()
	writeExtensionFile(t, filepath.Join(repo, "loader", "plugin.json"), `{"name":"test-extension-loader","entry":"main.go","enabled":true}`)
	writeExtensionFile(t, filepath.Join(repo, "loader", "main.go"), `package main
import (
    "fmt"
    "github.com/spock2300/vmake/pkg/plugin"
    "github.com/spock2300/vmake/pkg/toolchain"
)
func Main(ctx *plugin.Context) {
    fmt.Println("PLUGIN_EXECUTED")
    for _, failure := range toolchain.GetManager().ToolchainErrors() {
        var definitionError *toolchain.DefinitionError = failure
        if definitionError.Path() == "" || definitionError.Unwrap() == nil {
            panic("missing definition error context")
        }
    }
}
`)
}

func writeHealthyDefinition(t *testing.T, path, name string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	def := toolchain.ToolchainDef{Name: name, Tools: toolchain.Tools{CC: exe, CXX: exe, AR: exe, LD: exe}}
	data, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	writeExtensionFile(t, path, string(data))
}

func TestExtensionDefinitionsFailIndependently(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "extensions", "fixture")
	writeExtensionLoader(t, repo)
	writeHealthyDefinition(t, filepath.Join(repo, "healthy", "toolchain.json"), "healthy")
	writeExtensionFile(t, filepath.Join(repo, "legacy", "toolchain.json"), `{"name":"broken","host":"old-triple"}`)
	writeExtensionFile(t, filepath.Join(repo, "unknown", "toolchain.json"), `{"name":"untrustworthy",`)
	writeExtensionFile(t, filepath.Join(repo, "unsupported", "toolchain.json"), `{"name":"unsupported","version":"1","installations":{"unsupported-host/amd64":{"method":"lfs","file":"archive.zip","root_dir":"."}}}`)
	out, err := extensionCommand(t, dir, dir, "toolchain", "list")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	for _, want := range []string{"healthy", "Unavailable toolchains:", "broken [fixture/legacy/toolchain.json]", "fixture/unknown/toolchain.json [unidentified]", "no installation for host " + runtime.GOOS + "/" + runtime.GOARCH} {
		if !strings.Contains(out, want) {
			t.Fatalf("list missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "Unavailable toolchain: broken") != 1 {
		t.Fatalf("duplicate startup diagnostic:\n%s", out)
	}
	project := filepath.Join(dir, "project")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnBuild(func(ctx *api.BuildContext) { ctx.Target("ready").SetKind(api.TargetVoid) })
}
`)
	configPath := filepath.Join(project, ".vmake", "config.json")
	writeExtensionFile(t, configPath, `{"version":"1","global":{"toolchain":"healthy"},"entries":{}}`)
	if out, err := extensionCommand(t, dir, project, "build"); err != nil {
		t.Fatalf("healthy build blocked by unrelated definitions: %v\n%s", err, out)
	}
	for _, name := range []string{"broken", "unsupported", "untrustworthy"} {
		out, err := extensionCommand(t, dir, project, "toolchain", "show", name)
		if err == nil || !strings.Contains(out, name) || !strings.Contains(out, "toolchain.json") {
			t.Fatalf("show %s did not fail precisely: %v\n%s", name, err, out)
		}
		data, _ := json.Marshal(map[string]any{"version": "1", "global": map[string]string{"toolchain": name}, "entries": map[string]any{}})
		writeExtensionFile(t, configPath, string(data))
		for _, command := range [][]string{{"query"}, {"query", "targets"}, {"build"}} {
			out, err := extensionCommand(t, dir, project, command...)
			if err == nil || !strings.Contains(out, name) || !strings.Contains(out, "toolchain.json") {
				t.Fatalf("%v with %s did not fail precisely: %v\n%s", command, name, err, out)
			}
			if strings.Contains(out, "continuing without toolchain") {
				t.Fatalf("query fell back after toolchain failure:\n%s", out)
			}
		}
	}
}

func TestExtensionRecoverySkipsPluginExecution(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	upstream := filepath.Join(dir, "upstream")
	writeExtensionLoader(t, upstream)
	manifest := filepath.Join(upstream, "toolchain", "toolchain.json")
	writeExtensionFile(t, manifest, `{"name":"broken","install":null}`)
	runGit := func(workDir string, args ...string) {
		t.Helper()
		cmd := exec.Command(git, append([]string{"-c", "user.name=VMake Test", "-c", "user.email=vmake@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = workDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit(upstream, "init")
	runGit(upstream, "add", ".")
	runGit(upstream, "commit", "-m", "broken definition")
	state := filepath.Join(dir, "state")
	clone := filepath.Join(state, "extensions", "fixture")
	if err := os.MkdirAll(filepath.Dir(clone), 0755); err != nil {
		t.Fatal(err)
	}
	runGit(dir, "clone", upstream, clone)
	for _, args := range [][]string{{"--verbose", "ext", "list"}, {"ext", "--help"}} {
		out, err := extensionCommand(t, state, dir, args...)
		if err != nil || strings.Contains(out, "PLUGIN_EXECUTED") {
			t.Fatalf("recovery %v: %v\n%s", args, err, out)
		}
	}
	writeHealthyDefinition(t, manifest, "recovered")
	runGit(upstream, "add", ".")
	runGit(upstream, "commit", "-m", "repair definition")
	out, err := extensionCommand(t, state, dir, "-q", "ext", "update", "fixture")
	if err != nil || strings.Contains(out, "PLUGIN_EXECUTED") {
		t.Fatalf("update: %v\n%s", err, out)
	}
	out, err = extensionCommand(t, state, dir, "toolchain", "list")
	if err != nil || !strings.Contains(out, "recovered") || strings.Contains(out, "Unavailable toolchain") {
		t.Fatalf("repaired startup: %v\n%s", err, out)
	}
	writeExtensionFile(t, filepath.Join(clone, "toolchain", "toolchain.json"), `{"name":`)
	out, err = extensionCommand(t, state, dir, "--verbose", "ext", "remove", "fixture")
	if err != nil || strings.Contains(out, "PLUGIN_EXECUTED") {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	if _, err := os.Stat(clone); !os.IsNotExist(err) {
		t.Fatalf("extension was not removed: %v", err)
	}
}

func TestRegisterRepoToolchainsRetainsDanglingManifest(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "dangling", "toolchain.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repo, "missing.json"), path); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symbolic links unavailable: %v", err)
		}
		t.Fatal(err)
	}
	mgr := toolchain.GetManager()
	mgr.RegisterRepo(repo, t.TempDir())
	for _, err := range mgr.ToolchainErrors() {
		if err.Path() == path {
			if err.Name() != "" || !os.IsNotExist(err.Unwrap()) {
				t.Fatalf("dangling manifest diagnostic = %v", err)
			}
			return
		}
	}
	t.Fatal("dangling manifest was silently skipped")
}
