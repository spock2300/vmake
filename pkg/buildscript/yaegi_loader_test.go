package buildscript

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestLoadBuildScript_Simple(t *testing.T) {
	dir, _ := filepath.Abs("../../test_data/01_simple_c")
	src := Source{
		Name:   "01_simple_c",
		Path:   filepath.Join(dir, "build.go"),
		Dir:    dir,
		Origin: api.SourceLocal,
	}

	pkg, err := LoadBuildScript(src)
	if err != nil {
		t.Fatalf("LoadBuildScript failed: %v", err)
	}
	if pkg == nil {
		t.Fatal("LoadBuildScript returned nil package")
	}

	t.Logf("package name: %s", pkg.Name)
	t.Logf("options: %d", len(pkg.Options))
	t.Logf("requireFuncs: %d", len(pkg.GetRequireFuncs()))

	buildCtx := api.NewBuildContext("01_simple_c", nil)
	buildCtx.SetPackage(pkg)

	pkg.ExecBuildFuncs(dir, func(fn api.BuildFunc) {
		fn(buildCtx)
	})

	targets := buildCtx.GetTargets()
	t.Logf("targets after exec: %d", len(targets))

	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	tgt, ok := targets["hello"]
	if !ok {
		t.Fatalf("expected target named 'hello', got keys: %v", targetKeys(targets))
	}
	if tgt.Kind() != api.TargetBinary {
		t.Errorf("expected target kind TargetBinary, got %v", tgt.Kind())
	}
}

func TestRuntimeFilesystemUsesPackageWorkspace(t *testing.T) {
	seed, workspace := t.TempDir(), t.TempDir()
	script := `package main
import (
	"os"
	"github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		if err := os.WriteFile("generated.txt", []byte("workspace"), 0644); err != nil { panic(err) }
	})
}
`
	path := filepath.Join(seed, "build.go")
	if err := os.WriteFile(path, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	pkg, err := LoadBuildScript(*NewSource("native/sample", path, seed, api.SourceRemote))
	if err != nil {
		t.Fatal(err)
	}
	pkg.SetDirs(api.PkgDirs{SourceDir: workspace})
	pkg.ExecBuildFuncs(workspace, func(fn api.BuildFunc) { fn(api.NewBuildContext("native/sample", nil)) })
	if _, err := os.Stat(filepath.Join(seed, "generated.txt")); !os.IsNotExist(err) {
		t.Fatalf("runtime write reached source seed: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(workspace, "generated.txt")); err != nil || string(data) != "workspace" {
		t.Fatalf("runtime write missed workspace: %q, %v", data, err)
	}
}

func targetKeys(m map[string]*api.Target) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestLoadBuildScript_MultiFile(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.WriteFile(filepath.Join(dir, "build.go"), []byte(`package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddCFlags(getFlags())
	})
}
`), 0644)
	os.WriteFile(filepath.Join(dir, "helpers.go"), []byte(`package main

func getFlags() []string {
	return []string{"-Wall", "-O2"}
}
`), 0644)
	os.WriteFile(filepath.Join(dir, "src", "main.c"), []byte("int main(void){return 0;}\n"), 0644)

	src := Source{
		Name:   "multifile_test",
		Path:   filepath.Join(dir, "build.go"),
		Dir:    dir,
		Origin: api.SourceLocal,
	}

	pkg, err := LoadBuildScript(src)
	if err != nil {
		t.Fatalf("LoadBuildScript failed: %v", err)
	}

	buildCtx := api.NewBuildContext("multifile_test", nil)
	buildCtx.SetPackage(pkg)
	pkg.ExecBuildFuncs(dir, func(fn api.BuildFunc) {
		fn(buildCtx)
	})

	targets := buildCtx.GetTargets()
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d: %v", len(targets), targetKeys(targets))
	}
	tgt := targets["app"]
	if tgt == nil {
		t.Fatal("expected target 'app'")
	}
	cflags := tgt.CFlags()
	found := false
	for _, f := range cflags {
		if f == "-Wall" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected -Wall in cflags from helpers.go, got: %v", cflags)
	}
}

func TestLoadBuildScript_CompleteAPI(t *testing.T) {
	dir, _ := filepath.Abs("../../test_data/06_complete_api")
	src := Source{
		Name:   "06_complete_api",
		Path:   filepath.Join(dir, "build.go"),
		Dir:    dir,
		Origin: api.SourceLocal,
	}

	pkg, err := LoadBuildScript(src)
	if err != nil {
		t.Fatalf("LoadBuildScript failed: %v", err)
	}

	cfgCtx := api.NewConfigContextWithPackage("06_complete_api", pkg)
	pkg.ExecConfigFuncs(dir, func(fn api.ConfigFunc) {
		fn(cfgCtx)
	})

	opts := cfgCtx.GetOptions()
	if len(opts) != 12 {
		t.Errorf("expected 12 options, got %d", len(opts))
	}

	buildCtx := api.NewBuildContext("06_complete_api", nil)
	buildCtx.Options = opts
	buildCtx.SetPackage(pkg)
	buildCtx.SetOptions(opts)
	pkg.ExecBuildFuncs(dir, func(fn api.BuildFunc) {
		fn(buildCtx)
	})

	targets := buildCtx.GetTargets()
	if len(targets) < 4 {
		t.Errorf("expected at least 4 targets, got %d: %v", len(targets), targetKeys(targets))
	}
}
