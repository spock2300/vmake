package buildscript

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func writeScriptPackage(t *testing.T, marker string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "who.txt"), []byte(marker), 0644); err != nil {
		t.Fatal(err)
	}
	script := `package main

import (
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		data, err := os.ReadFile("who.txt")
		if err != nil {
			panic(err)
		}
		wd, _ := os.Getwd()
		out := filepath.Join(wd, "seen.txt")
		os.WriteFile(out, []byte(string(data)+"|"+wd), 0644)
	})
}
`
	if err := os.WriteFile(filepath.Join(dir, "build.go"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runBuildCallbackFromForeignCwd executes the package's build callback
// without any process chdir: the test binary's cwd (the package source dir)
// is foreign to the script dir, so relative IO only succeeds if the ScriptFS
// resolution is in effect. Safe to call from non-test goroutines: failures
// are reported via t.Errorf and the bool sentinel stops further work.
func runBuildCallbackFromForeignCwd(t *testing.T, dir string) bool {
	src := Source{Name: "p", Path: filepath.Join(dir, "build.go"), Dir: dir, Origin: api.SourceLocal}
	pkg, err := LoadBuildScript(src)
	if err != nil {
		t.Errorf("LoadBuildScript %s: %v", dir, err)
		return false
	}
	buildCtx := api.NewBuildContext("p", nil)
	buildCtx.SetPackage(pkg)
	pkg.ExecBuildFuncs("", func(fn api.BuildFunc) { fn(buildCtx) })
	return true
}

func TestScriptRelativeFileIOInCallbacks(t *testing.T) {
	dir := writeScriptPackage(t, "pkgA")
	if !runBuildCallbackFromForeignCwd(t, dir) {
		return
	}

	got, err := os.ReadFile(filepath.Join(dir, "seen.txt"))
	if err != nil {
		t.Fatalf("callback did not write seen.txt in script dir: %v", err)
	}
	want := "pkgA|" + dir
	if string(got) != want {
		t.Errorf("seen.txt = %q, want %q", got, want)
	}
}

func TestScriptRelativeFileIOConcurrentPackages(t *testing.T) {
	dirA := writeScriptPackage(t, "pkgA")
	dirB := writeScriptPackage(t, "pkgB")

	var wg sync.WaitGroup
	wg.Add(2)
	for _, dir := range []string{dirA, dirB} {
		go func(d string) {
			defer wg.Done()
			runBuildCallbackFromForeignCwd(t, d)
		}(dir)
	}
	wg.Wait()

	for dir, want := range map[string]string{dirA: "pkgA", dirB: "pkgB"} {
		got, err := os.ReadFile(filepath.Join(dir, "seen.txt"))
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if string(got) != want+"|"+dir {
			t.Errorf("%s: seen.txt = %q, want %q", dir, got, want+"|"+dir)
		}
	}
}

func TestLoadTimeScriptRelativeIO(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "maindata.txt"), []byte("main"), 0644); err != nil {
		t.Fatal(err)
	}
	script := `package main

import (
	"os"

	"github.com/spock2300/vmake/pkg/api"
)

var MainPayload = "none"

func Main(p *api.Package) {
	data, err := os.ReadFile("maindata.txt")
	if err != nil {
		panic(err)
	}
	MainPayload = string(data)
	p.OnRequire(func(ctx *api.RequireContext) {})
}
`
	if err := os.WriteFile(filepath.Join(dir, "build.go"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}

	src := Source{Name: "p", Path: filepath.Join(dir, "build.go"), Dir: dir, Origin: api.SourceLocal}
	if _, err := LoadBuildScript(src); err != nil {
		t.Fatalf("LoadBuildScript: %v", err)
	}
}

func TestChdirRejectedInScript(t *testing.T) {
	dir := t.TempDir()
	script := `package main

import (
	"os"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		if err := os.Chdir("/tmp"); err == nil {
			os.WriteFile(` + strconv.Quote(filepath.Join(dir, "chdir-ok")) + `, []byte("x"), 0644)
		}
	})
}
`
	if err := os.WriteFile(filepath.Join(dir, "build.go"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	src := Source{Name: "p", Path: filepath.Join(dir, "build.go"), Dir: dir, Origin: api.SourceLocal}
	pkg, err := LoadBuildScript(src)
	if err != nil {
		t.Fatalf("LoadBuildScript: %v", err)
	}
	buildCtx := api.NewBuildContext("p", nil)
	pkg.ExecBuildFuncs("", func(fn api.BuildFunc) { fn(buildCtx) })
	if _, err := os.Stat(filepath.Join(dir, "chdir-ok")); err == nil {
		t.Error("os.Chdir must be rejected in interpreted scripts")
	}
}
