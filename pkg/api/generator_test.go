package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageDeclarativeGeneratorsDeferDuringOnBuild(t *testing.T) {
	srcDir := t.TempDir()
	buildDir := filepath.Join(srcDir, "build", "key1")

	p := NewPackage().SetName("demo")
	p.SetDirs(PkgDirs{SourceDir: srcDir, BuildDir: buildDir})

	genCalls := 0
	var hdrPath, srcPath string
	p.OnBuild(func(ctx *BuildContext) {
		hdrPath = p.WriteBuildFile("generated/cfg.h", "#define DEMO_VAL 42\n")
		srcPath = ctx.GenerateFile("generated/gen.c", func() ([]byte, error) {
			genCalls++
			return []byte("int demo_val(void) { return 42; }\n"), nil
		})
		ctx.Generate(func() error {
			genCalls++
			p.WriteBuildFile("generated/extra.txt", "extra")
			return nil
		})
	})

	ctx := NewBuildContext("demo", nil).SetPackage(p)
	p.ExecBuildFuncs(srcDir, func(fn BuildFunc) { fn(ctx) })

	if genCalls != 0 {
		t.Fatalf("OnBuild executed %d generator callbacks during declaration, want 0", genCalls)
	}
	if _, err := os.Stat(buildDir); !os.IsNotExist(err) {
		t.Fatalf("OnBuild created buildDir during declaration: %v", err)
	}
	if want := filepath.Join(buildDir, "generated", "cfg.h"); hdrPath != want {
		t.Fatalf("hdrPath = %q, want %q", hdrPath, want)
	}
	if want := filepath.Join(buildDir, "generated", "gen.c"); srcPath != want {
		t.Fatalf("srcPath = %q, want %q", srcPath, want)
	}
	if !p.HasGenerators() {
		t.Fatal("HasGenerators() = false, want true")
	}

	written := 0
	if err := p.RunGenerators(srcDir, buildDir, func(string) { written++ }); err != nil {
		t.Fatalf("RunGenerators: %v", err)
	}
	if genCalls != 2 {
		t.Fatalf("RunGenerators ran %d callbacks, want 2", genCalls)
	}
	if written != 3 {
		t.Fatalf("RunGenerators wrote %d files, want 3", written)
	}
	data, err := os.ReadFile(hdrPath)
	if err != nil || string(data) != "#define DEMO_VAL 42\n" {
		t.Fatalf("cfg.h = %q, %v", string(data), err)
	}

	written = 0
	if err := p.RunGenerators(srcDir, buildDir, func(string) { written++ }); err != nil {
		t.Fatalf("second RunGenerators: %v", err)
	}
	if written != 0 {
		t.Fatalf("unchanged RunGenerators rewrote %d files, want 0", written)
	}
}

func TestBuildPathSymbolicWhenBuildDirEmpty(t *testing.T) {
	p := NewPackage().SetName("demo")
	if got := p.BuildPath("generated", "foo.h"); got != filepath.Join(BuildDirToken, "generated", "foo.h") {
		t.Fatalf("BuildPath without BuildDir = %q", got)
	}
	if got := p.CMakeInstallDir(); got != filepath.Join(BuildDirToken, "staging") {
		t.Fatalf("CMakeInstallDir without BuildDir = %q", got)
	}
}

func TestWriteBuildFileRejectsEscapingPath(t *testing.T) {
	p := NewPackage().SetName("demo")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for escaping path")
		}
		err, ok := r.(*BuildScriptError)
		if !ok || !strings.Contains(err.Error(), "escapes build directory") {
			t.Fatalf("unexpected panic: %T %v", r, r)
		}
	}()
	p.WriteBuildFile("../outside.h", "bad")
}
