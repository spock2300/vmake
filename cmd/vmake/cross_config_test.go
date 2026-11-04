package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type compileCommandEntry struct {
	File string `json:"file"`
}

func writeCrossConfigProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "scratch")
	writeFile := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.AddRequires("dep")
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.GlobalOption("chip").SetDefault("scratch")
		ctx.Option("feature").SetDefault(true)
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).
			AddFiles("main.c").
			AddDeps("dep:*")
	})
}
`)
	writeFile(filepath.Join(project, "main.c"), "int main(void) { return 0; }\n")
	writeFile(filepath.Join(project, "dep", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		if !ctx.Bool("scratch/feature") || ctx.String("scratch/chip") != "scratch" {
			ctx.Target("dep").SetKind(api.TargetVoid)
			return
		}
		ctx.Target("dep").SetKind(api.TargetStatic).AddFiles("src/dep.c")
	})
}
`)
	writeFile(filepath.Join(project, "dep", "src", "dep.c"), "int dep_value(void);\nint dep_value(void) { return 1; }\n")
	return project
}

func writeBoundOwnerCrossProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "bound")
	writeFile := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.AddRequires("dep")
	p.OnBuild(func(ctx *api.BuildContext) {
		kind := api.TargetVoid
		if ctx.Bool("dep/feature") {
			kind = api.TargetBinary
		}
		target := ctx.Target("app").SetKind(kind)
		if kind == api.TargetBinary {
			target.AddFiles("main.c")
		}
	})
}
`)
	writeFile(filepath.Join(project, "main.c"), "int main(void) { return 0; }\n")
	writeFile(filepath.Join(project, "dep", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("feature").SetType(api.OptionBool).SetDefault(true)
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("dep").SetKind(api.TargetVoid)
	})
}
`)
	return project
}

func compileDatabaseHas(t *testing.T, project, fragment string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(project, "build", "compile_commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []compileCommandEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.File, fragment) {
			return true
		}
	}
	return false
}

func buildTargetKind(t *testing.T, project, pkg, target string) string {
	t.Helper()
	report := loadBuildReport(t, project)
	for _, entry := range report.Targets {
		if entry.Package == pkg && entry.Target == target {
			return entry.Kind
		}
	}
	t.Fatalf("target %s:%s missing from build report", pkg, target)
	return ""
}

func TestCrossPackageOptionReadsBoundOwner(t *testing.T) {
	project := writeBoundOwnerCrossProject(t)
	for _, tc := range []struct {
		feature string
		want    string
	}{
		{"false", "void"},
		{"true", "binary"},
	} {
		stdout, stderr, code := runBuildCommand(t, project, "build", "--set", "dep/feature="+tc.feature)
		if code != 0 {
			t.Fatalf("feature %s build exit = %d\nstdout:\n%s\nstderr:\n%s", tc.feature, code, stdout, stderr)
		}
		if got := buildTargetKind(t, project, "bound", "app"); got != tc.want {
			t.Fatalf("feature %s app kind = %q, want %q", tc.feature, got, tc.want)
		}
	}
}

func TestCrossPackageOptionGatesTarget(t *testing.T) {
	project := writeCrossConfigProject(t)

	stdout, stderr, code := runBuildCommand(t, project, "build", "--set", "scratch/feature=false")
	if code != 0 {
		t.Fatalf("feature off build exit = %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if got := buildTargetKind(t, project, "dep", "dep"); got != "void" {
		t.Fatalf("feature off dep kind = %q, want void", got)
	}
	if compileDatabaseHas(t, project, "dep.c") {
		t.Fatal("feature off compiled dep.c")
	}

	stdout, stderr, code = runBuildCommand(t, project, "build", "--set", "scratch/feature=true")
	if code != 0 {
		t.Fatalf("feature on build exit = %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if got := buildTargetKind(t, project, "dep", "dep"); got != "static" {
		t.Fatalf("feature on dep kind = %q, want static", got)
	}
	if !compileDatabaseHas(t, project, "dep.c") {
		t.Fatal("feature on did not compile dep.c")
	}
}
