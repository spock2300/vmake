package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunBuildEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no C compiler available")
	}
	root := t.TempDir()
	buildGo := `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
	})
}
`
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.go"), []byte(buildGo), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.c"), []byte("int main(void) { return 0; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := &RuntimeContext{
		WorkDir: root,
		Config:  emptyConfig(),
		Paths: &Paths{
			ProjectDir: root,
			DepsDir:    filepath.Join(root, ".vmake_deps"),
			CacheDir:   filepath.Join(root, "cache"),
			ReposDir:   filepath.Join(root, "repos"),
			LockPath:   filepath.Join(root, ".vmake", "vmake.lock"),
		},
	}
	if err := Require(ctx); err != nil {
		t.Fatal(err)
	}
	if err := Configure(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := RunBuild(ctx, BuildOptions{Jobs: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || len(result.Targets) != 1 {
		t.Fatalf("build result = %+v", result)
	}
	if result.Targets[0].Status == "" || len(result.Targets[0].Outputs) == 0 {
		t.Fatalf("target result = %+v", result.Targets[0])
	}
	if _, err := os.Stat(result.Targets[0].Outputs[0]); err != nil {
		t.Fatalf("target output missing: %v", err)
	}
	if len(ctx.Preparation().Needed) == 0 {
		t.Fatal("Preparation result is empty")
	}
	if ctx.Resolution().Resolver == nil || ctx.Configuration().AllOptions == nil {
		t.Fatalf("phase results missing: resolution=%+v configuration=%+v", ctx.Resolution(), ctx.Configuration())
	}
}
