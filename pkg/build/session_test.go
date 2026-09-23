package build

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestPipelineUsesPackageToolchainAndCanonicalDependencyPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell compiler wrapper")
	}
	for _, sameName := range []bool{false, true} {
		t.Run(map[bool]string{false: "different-names", true: "same-name-different-tools"}[sameName], func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			writeAssemblyFixture(t, dir, "lib.c", "int base(void) { return 1; }\n")
			writeAssemblyFixture(t, dir, "main.c", "#include <stdio.h>\nint base(void); int main(void) { printf(\"%d\\n\", base() + VALUE); return 0; }\n")
			library := makeTargetWithDeps("library").SetKind(api.TargetStatic).AddFiles("lib.c")
			app := makeTargetWithDeps("app", "p:library").SetKind(api.TargetBinary).AddFiles("main.c")
			native := nativeTestScheduler(t, dir, library)
			alternate := *native.toolchain
			if !sameName {
				alternate.Name += "-alternate"
			}
			wrapper := filepath.Join(dir, "compiler.sh")
			command := "#!/bin/sh\nexec '" + strings.ReplaceAll(alternate.Tools.CC, "'", "'\\''") + "' -DVALUE=41 \"$@\"\n"
			if err := os.WriteFile(wrapper, []byte(command), 0755); err != nil {
				t.Fatal(err)
			}
			alternate.Tools.CC = wrapper
			graph, err := NewBuildGraph(map[string]map[string]*api.Target{"p": {"library": library}, "q": {"app": app}}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			pipeline := NewBuildPipeline(graph, native.toolchain, map[string]*api.PkgDirs{"p": {SourceDir: dir}, "q": {SourceDir: dir}}, api.ModeDebug, nil, api.Platform{OS: runtime.GOOS})
			pipeline.RootDir = dir
			pipeline.PackageToolchains = map[string]*toolchain.Toolchain{"q": &alternate}
			pipeline.Session = NewSession(nil)
			scheduler, err := pipeline.Run()
			if err != nil {
				t.Fatal(err)
			}
			output := resolveWorkPath(dir, scheduler.getTargetOutputPath(graph.Nodes["q:app"]))
			result, err := exec.Command(output).CombinedOutput()
			if err != nil || strings.TrimSpace(string(result)) != "42" {
				t.Fatalf("package compiler result %q: %v", result, err)
			}
			var commands []CompileCommand
			if err := jsonio.Load(filepath.Join(dir, "build", "compile_commands.json"), &commands); err != nil {
				t.Fatal(err)
			}
			if len(commands) != 2 {
				t.Fatalf("lost cross-toolchain compilation commands: %d", len(commands))
			}
		})
	}
}

func TestSubGraphAndMainPipelineShareTargetSession(t *testing.T) {
	dir := t.TempDir()
	var executed []string
	makeVoid := func(name string, deps ...string) *api.Target {
		return makeTargetWithDeps(name, deps...).SetKind(api.TargetVoid).SetBuildFunc(func(*api.Package) error {
			executed = append(executed, name)
			return nil
		})
	}
	first, second := makeVoid("first"), makeVoid("second", "p:first")
	native := nativeTestScheduler(t, dir, first)
	targets := map[string]map[string]*api.Target{"p": {"first": first}, "q": {"second": second}}
	dirs := map[string]*api.PkgDirs{
		"p": {SourceDir: dir, BuildDir: filepath.Join(dir, "p-build")},
		"q": {SourceDir: dir, BuildDir: filepath.Join(dir, "q-build")},
	}
	packages := map[string]*api.Package{"p": api.NewPackage().SetDirs(*dirs["p"]), "q": api.NewPackage().SetDirs(*dirs["q"])}
	meta := map[string]PkgBuildMeta{"p": {}, "q": {Deps: []string{"p"}}}
	session := NewSession(nil)
	params := &SubGraphParams{
		AllTargets: targets, PkgMeta: meta, PkgDirs: dirs, Packages: packages,
		Needed: map[string]bool{"p": true, "q": true}, Platform: api.Platform{OS: runtime.GOOS},
		Session: session, RootDir: dir,
		PackageToolchains: map[string]*toolchain.Toolchain{"unrelated": {Name: "missing-toolchain", Tools: toolchain.Tools{CC: filepath.Join(dir, "missing-cc")}}},
	}
	for _, root := range []string{"p", "q"} {
		if err := BuildSubGraph(root, native.toolchain, native.toolchain.Name, api.ModeDebug, params, nil); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := NewBuildGraph(targets, meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	pipeline := NewBuildPipeline(graph, native.toolchain, dirs, api.ModeDebug, nil, params.Platform)
	pipeline.Session, pipeline.RootDir, pipeline.Packages = session, dir, packages
	if _, err := pipeline.Run(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(executed, []string{"first", "second"}) {
		t.Fatalf("targets rerun across subgraphs and main graph: %v", executed)
	}
}

func TestSessionRetainsFailuresAndRejectsNestedTargetExecution(t *testing.T) {
	session := NewSession(nil)
	want := errors.New("external build failed")
	if err := session.Execute("p:failure", func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("first failure = %v", err)
	}
	if err := session.Execute("p:failure", func() error { t.Fatal("failed target reran"); return nil }); !errors.Is(err, want) {
		t.Fatalf("cached failure = %v", err)
	}
	if err := session.Execute("p:outer", func() error {
		return session.Execute("q:inner", func() error { t.Fatal("nested target ran"); return nil })
	}); err == nil || !strings.Contains(err.Error(), "while target p:outer is running") {
		t.Fatalf("nested execution = %v", err)
	}
	if session.ActiveTarget() != "" {
		t.Fatal("failed target retained active state")
	}
}
