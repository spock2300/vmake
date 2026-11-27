package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/config"
)

func TestGitTagUsesSelectedProject(t *testing.T) {
	dir := t.TempDir()
	caller, selected := filepath.Join(dir, "caller"), filepath.Join(dir, "selected")
	createDepsTestGitRepo(t, caller, map[string]string{"vmake.json": "{}"}, "v1.4.0")
	writeExtensionFile(t, filepath.Join(caller, "pending"), "staged change belongs only to caller")
	runGitLine(t, caller, "add", "pending")
	want := createDepsTestGitRepo(t, selected, map[string]string{"vmake.json": "{ }"}, "v1.3.0")
	out, stderr, code := runCLI(t, caller, filepath.Join(dir, "state"), nil, "-C", selected, "git", "tag", "--no-push", "-y")
	if code != 0 {
		t.Fatalf("git tag exit = %d:\n%s%s", code, out, stderr)
	}
	if !strings.Contains(out, want[:7]) {
		t.Fatalf("preview omits selected commit %s:\n%s", want, out)
	}
	for _, name := range []string{"v1.3.1", "latest"} {
		got, err := exec.Command("git", "-C", selected, "rev-parse", name+"^{commit}").Output()
		if err != nil || strings.TrimSpace(string(got)) != want {
			t.Fatalf("selected %s = %q (%v), want %s", name, got, err, want)
		}
	}
	got, err := exec.Command("git", "-C", caller, "tag", "--list").Output()
	if err != nil || strings.TrimSpace(string(got)) != "v1.4.0" {
		t.Fatalf("caller tags changed: %s (%v)", got, err)
	}
}

func TestConcurrentDepsPinsKeepBothEntries(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package){ p.AddRequires("fixture/a","fixture/b") }`)
	manifest, err := json.Marshal(map[string]any{
		"repos":   map[string]any{"fixture": map[string]string{"native": filepath.ToSlash(filepath.Join(dir, "{name}"))}},
		"modules": map[string]string{"fixture/a": "1.0.0", "fixture/b": "1.0.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeExtensionFile(t, filepath.Join(project, "vmake.json"), string(manifest))
	var commands []*exec.Cmd
	var outputs [2]bytes.Buffer
	for i, name := range []string{"a", "b"} {
		createDepsTestGitRepo(t, filepath.Join(dir, name), map[string]string{
			"build.go": `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package){}`,
		}, "v1.0.0")
		cmd := newSubprocess(t, "TestExtensionBootstrapChild", project, []string{
			"VMAKE_TEST_EXT_DIR=" + filepath.Join(dir, "state"), "VMAKE_CACHE=" + filepath.Join(dir, "cache"), "VMAKE_TRUST_ALL=1",
		}, "deps", "pin", "fixture/"+name+"@1.0.0")
		cmd.Stdout, cmd.Stderr = &outputs[i], &outputs[i]
		commands = append(commands, cmd)
	}
	for _, cmd := range commands {
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("pin: %v\n%s", err, &outputs[i])
		}
	}
	got, err := config.LoadProjectPins(project)
	if err != nil || got["fixture/a"] != "1.0.0" || got["fixture/b"] != "1.0.0" {
		t.Fatalf("pins = %v (%v), want both dependencies pinned at 1.0.0", got, err)
	}
}

func TestDistcleanRejectsExtraArgumentsBeforeCleaning(t *testing.T) {
	root := t.TempDir()
	writeExtensionFile(t, filepath.Join(root, "vmake.json"), "{}")
	outFile := filepath.Join(root, "install", "bin", "app")
	writeExtensionFile(t, outFile, "prior output")
	_, stderr, code := runCLI(t, root, t.TempDir(), nil, "distclean", "module-only")
	if code != 2 {
		t.Errorf("exit = %d, want usage error: %s", code, stderr)
	}
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("rejected clean touched install: %v", err)
	}
}

func TestCleanAllFindsInactivePackageOutputsAndPreservesUserFiles(t *testing.T) {
	dir := t.TempDir()
	project, state := filepath.Join(dir, "project"), filepath.Join(dir, "state")
	writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "healthy", "toolchain.json"), "healthy")
	writeExtensionFile(t, filepath.Join(project, "vmake.json"), `{"roots":["//beta"]}`)
	createDepsTestGitRepo(t, filepath.Join(project, "alpha"), map[string]string{"note": "nested checkout"}, "")
	writeExtensionFile(t, filepath.Join(project, "alpha", "sub", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package){ p.OnBuild(func(c *api.BuildContext){c.Target("sublib").SetKind(api.TargetStatic).SetPrebuilt("input.a")}) }`)
	writeExtensionFile(t, filepath.Join(project, "alpha", "sub", "input.a"), "prebuilt sub")
	for _, name := range []string{"alpha", "beta"} {
		requires := ""
		useReq := ""
		if name == "alpha" {
			requires = `p.AddRequires("//alpha/sub");`
			useReq = `.UseRequires()`
		}
		writeExtensionFile(t, filepath.Join(project, "configs", name+".json"), `{"version":"1","global":{"toolchain":"healthy"},"root":"//`+name+`"}`)
		writeExtensionFile(t, filepath.Join(project, name, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package){ `+requires+` p.OnBuild(func(c *api.BuildContext){c.Target("lib").SetKind(api.TargetStatic).SetPrebuilt("input.a")`+useReq+`}) }`)
		writeExtensionFile(t, filepath.Join(project, name, "input.a"), "prebuilt "+name)
		if out, err := extensionCommand(t, state, project, "build", "-c", name); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
		matches, err := filepath.Glob(filepath.Join(project, name, "build", "*", "*.a"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("expected one %s archive before clean: %v (%v)", name, matches, err)
		}
	}
	subMatches, err := filepath.Glob(filepath.Join(project, "alpha", "sub", "build", "*", "*.a"))
	if err != nil || len(subMatches) != 1 {
		t.Fatalf("expected one alpha/sub archive before clean: %v (%v)", subMatches, err)
	}
	nested := filepath.Join(project, "vendor", "independent")
	writeExtensionFile(t, filepath.Join(nested, "build.go"), "package main")
	writeExtensionFile(t, filepath.Join(nested, "vmake.json"), "{}")
	saved := []string{filepath.Join(nested, "build", "1234567890abcdef", "out"), filepath.Join(project, "alpha", "build", "notes", "keep")}
	for _, p := range saved {
		writeExtensionFile(t, p, "keep")
	}
	if out, err := extensionCommand(t, state, project, "clean", "-c", "alpha"); err != nil {
		t.Fatalf("clean alpha: %v\n%s", err, out)
	}
	if out, err := extensionCommand(t, state, project, "clean", "--all", "-c", "beta"); err != nil {
		t.Fatalf("clean: %v\n%s", err, out)
	}
	for _, rel := range []string{"alpha", filepath.Join("alpha", "sub"), "beta"} {
		matches, err := filepath.Glob(filepath.Join(project, rel, "build", "*", "*.a"))
		if err != nil || len(matches) > 0 {
			t.Fatalf("%s leftover archives: %v (%v)", rel, matches, err)
		}
	}
	for _, p := range saved {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("clean removed user file %s: %v", p, err)
		}
	}
}
