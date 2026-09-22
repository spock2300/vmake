package toolchain

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeTool(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tool"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveToolPathKeepsInstalledToolsIsolated(t *testing.T) {
	root := t.TempDir()
	pathDir := filepath.Join(root, "path")
	writeTool(t, pathDir, "cross-gcc")
	t.Setenv("PATH", pathDir)
	install := filepath.Join(root, "install")
	if _, err := ResolveToolPath("cross-gcc", install); err == nil {
		t.Fatal("missing installed tool resolved from PATH")
	}
	want := writeTool(t, filepath.Join(install, "bin"), "cross-gcc")
	got, err := ResolveToolPath("cross-gcc", install)
	if err != nil || got != want {
		t.Fatalf("ResolveToolPath = %q, %v; want %q", got, err, want)
	}
	if _, err := ResolveToolPath(filepath.Join(root, "missing"), ""); err == nil {
		t.Fatal("missing absolute tool was accepted")
	}
}

func TestSelectToolchainValidatesConfiguredTools(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tc := &Toolchain{Name: "custom", TargetOS: "none", Tools: Tools{CC: exe, CXX: exe, AR: exe, LD: exe, OBJCOPY: filepath.Join(t.TempDir(), "missing")}}
	mgr := &Manager{extensions: map[string]*Toolchain{"custom": tc}}
	if _, err := mgr.SelectToolchain("custom"); err == nil || !strings.Contains(err.Error(), "objcopy") {
		t.Fatalf("selection error = %v", err)
	}
	tc.Tools.OBJCOPY = exe
	if got, err := mgr.SelectToolchain("custom"); err != nil || got != tc {
		t.Fatalf("selection = %v, %v", got, err)
	}
}

func TestBuiltinHostDoesNotRequireDefaultMake(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"gcc", "g++", "ar", "ld", "strip", "ranlib", "objcopy", "size", "objdump", "nm"} {
		writeTool(t, dir, name)
	}
	t.Setenv("PATH", dir)
	tc := GetBuiltinHost()
	mgr := &Manager{builtin: tc}
	if _, err := mgr.SelectToolchain("host"); err != nil {
		t.Fatalf("host without make: %v", err)
	}
	if tc.Tools.MAKE != "" || tc.MakeTool() != "make" {
		t.Fatalf("make configuration = %q, default = %q", tc.Tools.MAKE, tc.MakeTool())
	}
	tc.Tools.MAKE = "make"
	if _, err := mgr.SelectToolchain("host"); err == nil || !strings.Contains(err.Error(), "make") {
		t.Fatalf("missing explicit make accepted: %v", err)
	}
}

func TestManagerIsolatesDefinitionErrors(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	healthy := &Toolchain{Name: "healthy", TargetOS: runtime.GOOS, Tools: Tools{CC: exe, CXX: exe, AR: exe, LD: exe}}
	handlerCalled := false
	mgr := &Manager{
		builtin:    healthy,
		extensions: map[string]*Toolchain{"healthy": healthy, "broken": healthy},
		onMissing: map[string]OnMissingToolchain{"broken": func(string) (*Toolchain, error) {
			handlerCalled = true
			return healthy, nil
		}},
	}
	root := t.TempDir()
	badPath := filepath.Join(root, "a-broken", "toolchain.json")
	unknownPath := filepath.Join(root, "z-unknown", "toolchain.json")
	cause := errors.New("legacy field install")
	mgr.RegisterToolchainError("broken", badPath, cause)
	mgr.RegisterToolchainError("", unknownPath, errors.New("invalid JSON"))
	mgr.RegisterToolchainError("broken", badPath, cause)
	for _, lookup := range []func(string) (*Toolchain, error){mgr.GetToolchain, mgr.SelectToolchain} {
		for _, name := range []string{"", "host", "healthy"} {
			if got, err := lookup(name); err != nil || got != healthy {
				t.Fatalf("healthy selection %q: %v, %v", name, got, err)
			}
		}
		if got, err := lookup("broken"); got != nil || !errors.Is(err, cause) || !strings.Contains(err.Error(), badPath) {
			t.Fatalf("broken selection = %v, %v", got, err)
		}
		if got, err := lookup("unknown"); got != nil || err == nil || !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), unknownPath) || strings.Contains(err.Error(), badPath) {
			t.Fatalf("unidentified selection = %v, %v", got, err)
		}
	}
	if handlerCalled {
		t.Fatal("broken definition invoked installation handler")
	}
	listed, err := mgr.ListToolchains()
	if err != nil || len(listed) != 2 || listed["healthy"] == nil || listed["broken"] != nil {
		t.Fatalf("list = %v, %v", listed, err)
	}
	errs := mgr.ToolchainErrors()
	if len(errs) != 2 || errs[0].Path() != badPath || errs[1].Path() != unknownPath {
		t.Fatalf("diagnostics = %v", errs)
	}
	errs[0] = nil
	if mgr.ToolchainErrors()[0] == nil {
		t.Fatal("diagnostics snapshot changed manager state")
	}
}
