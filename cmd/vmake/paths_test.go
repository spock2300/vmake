package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func setPathsTestVmakeDir(t *testing.T) {
	t.Helper()
	previous := vmakeDir
	vmakeDir = t.TempDir()
	t.Cleanup(func() { vmakeDir = previous })
}

func writePathsTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func makePathsTestDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
}

func TestSearchProjectDirFromPackageSubdir(t *testing.T) {
	setPathsTestVmakeDir(t)
	root := t.TempDir()
	makePathsTestDir(t, filepath.Join(root, ".vmake"))
	writePathsTestFile(t, filepath.Join(root, "build.go"), "package main\n")
	writePathsTestFile(t, filepath.Join(root, "firmware", "build.go"), "package main\n")

	got, ok := searchProjectDir(filepath.Join(root, "firmware"))
	if !ok || got != root {
		t.Fatalf("searchProjectDir(firmware) = %q, %v; want %q, true", got, ok, root)
	}
}

func TestSearchProjectDirFromProjectRoot(t *testing.T) {
	setPathsTestVmakeDir(t)
	root := t.TempDir()
	makePathsTestDir(t, filepath.Join(root, ".vmake"))
	writePathsTestFile(t, filepath.Join(root, "build.go"), "package main\n")

	got, ok := searchProjectDir(root)
	if !ok || got != root {
		t.Fatalf("searchProjectDir(root) = %q, %v; want %q, true", got, ok, root)
	}
}

func TestSearchProjectDirNestedProjectWins(t *testing.T) {
	setPathsTestVmakeDir(t)
	outer := t.TempDir()
	makePathsTestDir(t, filepath.Join(outer, ".vmake"))
	writePathsTestFile(t, filepath.Join(outer, "build.go"), "package main\n")
	nested := filepath.Join(outer, "nested")
	makePathsTestDir(t, filepath.Join(nested, ".vmake"))
	writePathsTestFile(t, filepath.Join(nested, "build.go"), "package main\n")
	writePathsTestFile(t, filepath.Join(nested, "pkg", "build.go"), "package main\n")

	for _, dir := range []string{nested, filepath.Join(nested, "pkg")} {
		got, ok := searchProjectDir(dir)
		if !ok || got != nested {
			t.Errorf("searchProjectDir(%q) = %q, %v; want %q, true", dir, got, ok, nested)
		}
	}
}

func TestSearchProjectDirStandaloneBuildGo(t *testing.T) {
	setPathsTestVmakeDir(t)
	dir := t.TempDir()
	writePathsTestFile(t, filepath.Join(dir, "build.go"), "package main\n")

	got, ok := searchProjectDir(dir)
	if !ok || got != dir {
		t.Fatalf("searchProjectDir(standalone) = %q, %v; want %q, true", got, ok, dir)
	}
}

func TestSearchProjectDirNearestBuildGoWins(t *testing.T) {
	setPathsTestVmakeDir(t)
	dir := t.TempDir()
	writePathsTestFile(t, filepath.Join(dir, "build.go"), "package main\n")
	pkg := filepath.Join(dir, "pkg")
	writePathsTestFile(t, filepath.Join(pkg, "build.go"), "package main\n")

	got, ok := searchProjectDir(pkg)
	if !ok || got != pkg {
		t.Fatalf("searchProjectDir(pkg) = %q, %v; want %q, true", got, ok, pkg)
	}
}

func TestSearchProjectDirChildBuildGoFallback(t *testing.T) {
	setPathsTestVmakeDir(t)
	dir := t.TempDir()
	writePathsTestFile(t, filepath.Join(dir, "system", "build.go"), "package main\n")

	got, ok := searchProjectDir(dir)
	if !ok || got != dir {
		t.Fatalf("searchProjectDir(root with child build.go) = %q, %v; want %q, true", got, ok, dir)
	}
}

func TestSearchProjectDirIgnoresContainerVmake(t *testing.T) {
	setPathsTestVmakeDir(t)

	t.Run("without_build_go", func(t *testing.T) {
		container := t.TempDir()
		makePathsTestDir(t, filepath.Join(container, ".vmake"))
		fixture := filepath.Join(container, "fixture")
		writePathsTestFile(t, filepath.Join(fixture, "build.go"), "package main\n")

		got, ok := searchProjectDir(fixture)
		if !ok || got != fixture {
			t.Fatalf("searchProjectDir(fixture) = %q, %v; want %q, true", got, ok, fixture)
		}
	})

	t.Run("with_only_child_build_go", func(t *testing.T) {
		container := t.TempDir()
		makePathsTestDir(t, filepath.Join(container, ".vmake"))
		fixture := filepath.Join(container, "fixture")
		writePathsTestFile(t, filepath.Join(fixture, "build.go"), "package main\n")
		writePathsTestFile(t, filepath.Join(container, "other", "build.go"), "package main\n")

		got, ok := searchProjectDir(fixture)
		if !ok || got != fixture {
			t.Fatalf("searchProjectDir(fixture) = %q, %v; want %q, true", got, ok, fixture)
		}
	})
}

func TestSearchProjectDirIgnoresGlobalVmakeDir(t *testing.T) {
	home := t.TempDir()
	previous := vmakeDir
	vmakeDir = filepath.Join(home, ".vmake")
	t.Cleanup(func() { vmakeDir = previous })
	makePathsTestDir(t, vmakeDir)

	if got, ok := searchProjectDir(home); ok {
		t.Fatalf("searchProjectDir(home) = %q, true; global .vmake must not be a project marker", got)
	}
}

func TestSearchProjectDirNone(t *testing.T) {
	setPathsTestVmakeDir(t)
	if got, ok := searchProjectDir(t.TempDir()); ok {
		t.Fatalf("searchProjectDir(empty dir) = %q, true; want false", got)
	}
}

func TestLoadResolveInputsUsesProjectRootFromPackageSubdir(t *testing.T) {
	resetResolveTestGlobals(t)
	root := t.TempDir()
	makePathsTestDir(t, filepath.Join(root, ".vmake"))
	writePathsTestFile(t, filepath.Join(root, "build.go"), "package main\n")
	sub := filepath.Join(root, "firmware")
	writePathsTestFile(t, filepath.Join(sub, "build.go"), "package main\n")
	t.Chdir(sub)

	inputs, err := loadResolveInputs(false, false)
	if err != nil {
		t.Fatalf("loadResolveInputs: %v", err)
	}
	if inputs.workDir != root {
		t.Errorf("workDir = %q, want project root %q", inputs.workDir, root)
	}
	if inputs.paths.ProjectDir != root {
		t.Errorf("ProjectDir = %q, want project root %q", inputs.paths.ProjectDir, root)
	}
	if _, err := os.Stat(filepath.Join(sub, ".gitignore")); !os.IsNotExist(err) {
		t.Errorf("loadResolveInputs wrote .gitignore in the package subdirectory: %v", err)
	}
}

func TestInspectFromPackageSubdirUsesProjectRoot(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	makePathsTestDir(t, filepath.Join(project, ".vmake"))
	writePathsTestFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
}
`)
	packageDir := filepath.Join(project, "firmware")
	writePathsTestFile(t, filepath.Join(packageDir, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("system")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddDeps("system:system").AddFiles("src/*.c")
	})
}
`)
	writePathsTestFile(t, filepath.Join(project, "system", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("system").SetKind(api.TargetStatic).AddFiles("src/*.c")
	})
}
`)

	stdout, stderr, code := runInspectCommand(t, packageDir, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %s", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if envelope.Data.Root != project {
		t.Fatalf("inspect root = %q, want %q", envelope.Data.Root, project)
	}
	if !slices.Contains(envelope.Data.Roots, "firmware") {
		t.Errorf("roots = %v, want the firmware package", envelope.Data.Roots)
	}
	if !inspectPackageNamed(t, envelope.Data, "system").Needed {
		t.Error("sibling package system was not resolved from the project root")
	}
	if !inspectPackageNamed(t, envelope.Data, "firmware").Needed {
		t.Error("firmware package is not needed")
	}
	for _, name := range []string{".vmake", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(packageDir, name)); !os.IsNotExist(err) {
			t.Errorf("inspect created %s in the package subdirectory: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(project, ".gitignore")); err != nil {
		t.Errorf("inspect did not initialize the project root: %v", err)
	}
}

func TestSearchProjectDirGitRootFallback(t *testing.T) {
	setPathsTestVmakeDir(t)
	root := t.TempDir()
	makePathsTestDir(t, filepath.Join(root, ".git"))
	writePathsTestFile(t, filepath.Join(root, "build.go"), "package main\n")
	writePathsTestFile(t, filepath.Join(root, "firmware", "build.go"), "package main\n")

	got, ok := searchProjectDir(filepath.Join(root, "firmware"))
	if !ok || got != root {
		t.Fatalf("searchProjectDir(firmware) = %q, %v; want git root %q, true", got, ok, root)
	}
}

func TestSearchProjectDirGitFileFallback(t *testing.T) {
	setPathsTestVmakeDir(t)
	root := t.TempDir()
	writePathsTestFile(t, filepath.Join(root, ".git"), "gitdir: elsewhere\n")
	writePathsTestFile(t, filepath.Join(root, "build.go"), "package main\n")
	writePathsTestFile(t, filepath.Join(root, "pkg", "build.go"), "package main\n")

	got, ok := searchProjectDir(filepath.Join(root, "pkg"))
	if !ok || got != root {
		t.Fatalf("searchProjectDir(pkg) = %q, %v; want git root %q, true", got, ok, root)
	}
}

func TestSearchProjectDirGitRootWithoutBuildGoIgnored(t *testing.T) {
	setPathsTestVmakeDir(t)
	root := t.TempDir()
	makePathsTestDir(t, filepath.Join(root, ".git"))
	fixture := filepath.Join(root, "test_data", "fixture")
	writePathsTestFile(t, filepath.Join(fixture, "build.go"), "package main\n")

	got, ok := searchProjectDir(fixture)
	if !ok || got != fixture {
		t.Fatalf("searchProjectDir(fixture) = %q, %v; want %q, true", got, ok, fixture)
	}
}

func TestSearchProjectDirNestedVmakeBeatsGitRoot(t *testing.T) {
	setPathsTestVmakeDir(t)
	root := t.TempDir()
	makePathsTestDir(t, filepath.Join(root, ".git"))
	writePathsTestFile(t, filepath.Join(root, "build.go"), "package main\n")
	nested := filepath.Join(root, "nested")
	makePathsTestDir(t, filepath.Join(nested, ".vmake"))
	writePathsTestFile(t, filepath.Join(nested, "build.go"), "package main\n")
	writePathsTestFile(t, filepath.Join(nested, "pkg", "build.go"), "package main\n")

	got, ok := searchProjectDir(filepath.Join(nested, "pkg"))
	if !ok || got != nested {
		t.Fatalf("searchProjectDir(nested/pkg) = %q, %v; want nested project %q, true", got, ok, nested)
	}
}

func TestLoadResolveInputsUsesProjectOverride(t *testing.T) {
	resetResolveTestGlobals(t)
	project := t.TempDir()
	makePathsTestDir(t, filepath.Join(project, ".vmake"))
	writePathsTestFile(t, filepath.Join(project, "build.go"), "package main\n")
	outside := t.TempDir()
	t.Chdir(outside)
	projectOverride = project

	inputs, err := loadResolveInputs(false, false)
	if err != nil {
		t.Fatalf("loadResolveInputs: %v", err)
	}
	if inputs.workDir != project {
		t.Errorf("workDir = %q, want project override %q", inputs.workDir, project)
	}
	if inputs.paths.ProjectDir != project {
		t.Errorf("ProjectDir = %q, want project override %q", inputs.paths.ProjectDir, project)
	}
	if _, err := os.Stat(filepath.Join(outside, ".gitignore")); !os.IsNotExist(err) {
		t.Errorf("loadResolveInputs initialized the invocation directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".gitignore")); err != nil {
		t.Errorf("loadResolveInputs did not initialize the project override: %v", err)
	}
}

func TestFindProjectDirOverrideRelative(t *testing.T) {
	resetResolveTestGlobals(t)
	parent := t.TempDir()
	project := filepath.Join(parent, "工程 space")
	makePathsTestDir(t, filepath.Join(project, ".vmake"))
	writePathsTestFile(t, filepath.Join(project, "build.go"), "package main\n")
	t.Chdir(parent)
	projectOverride = "工程 space"

	if got := findProjectDir(); got != project {
		t.Fatalf("findProjectDir() = %q, want %q", got, project)
	}
	if got := findProjectDirSoft(); got != project {
		t.Fatalf("findProjectDirSoft() = %q, want %q", got, project)
	}
}

func TestProjectFlagRegistration(t *testing.T) {
	flag := RootCmd.PersistentFlags().Lookup("project")
	if flag == nil {
		t.Fatal("root command has no --project flag")
	}
	if flag.Shorthand != "C" {
		t.Errorf("--project shorthand = %q, want C", flag.Shorthand)
	}
	if !strings.Contains(generateCLIRef(RootCmd), "--project <value> -C") {
		t.Error("CLI reference is missing the --project flag")
	}
}

func TestInspectProjectOverrideFromOutside(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writePathsTestFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary)
	})
}
`)
	outside := t.TempDir()
	stdout, stderr, code := runInspectCommand(t, outside, "inspect", "--project", project)
	if code != 0 {
		t.Fatalf("inspect --project exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if envelope.Data.Root != project {
		t.Fatalf("inspect root = %q, want project override %q", envelope.Data.Root, project)
	}
	if _, err := os.Stat(filepath.Join(outside, ".vmake")); !os.IsNotExist(err) {
		t.Errorf("inspect initialized the invocation directory: %v", err)
	}
}

func TestInspectProjectOverrideRelative(t *testing.T) {
	parent := t.TempDir()
	project := filepath.Join(parent, "工程 space")
	writePathsTestFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary)
	})
}
`)
	stdout, stderr, code := runInspectCommand(t, parent, "-C", "工程 space", "inspect")
	if code != 0 {
		t.Fatalf("inspect -C exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil || envelope.Data.Root != project {
		t.Fatalf("envelope root = %+v, want %q", envelope.Data, project)
	}
}

func TestInspectProjectOverrideInvalid(t *testing.T) {
	outside := t.TempDir()
	empty := t.TempDir()
	file := filepath.Join(outside, "build.go.txt")
	writePathsTestFile(t, file, "not a directory\n")

	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{"missing", filepath.Join(outside, "missing"), "does not exist"},
		{"empty", empty, "is not a vmake project"},
		{"file", file, "is not a directory"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, stderr, code := runInspectCommand(t, outside, "inspect", "--project", test.path)
			if code != 2 {
				t.Fatalf("inspect --project %s exit = %d, want 2 (stderr %q)", test.name, code, stderr)
			}
			if !strings.Contains(stderr, test.want) {
				t.Fatalf("stderr = %q, want %q", stderr, test.want)
			}
		})
	}
}

func TestInspectFromPackageSubdirUsesGitRoot(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	makePathsTestDir(t, filepath.Join(project, ".git"))
	writePathsTestFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
}
`)
	packageDir := filepath.Join(project, "firmware")
	writePathsTestFile(t, filepath.Join(packageDir, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("system")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddDeps("system:system")
	})
}
`)
	writePathsTestFile(t, filepath.Join(project, "system", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("system").SetKind(api.TargetStatic)
	})
}
`)

	check := func(t *testing.T, wantRootInitialized bool) {
		t.Helper()
		stdout, stderr, code := runInspectCommand(t, packageDir, "inspect")
		if code != 0 {
			t.Fatalf("inspect exit = %d, stderr = %s", code, stderr)
		}
		envelope := decodeInspectEnvelope(t, stdout)
		if envelope.Status != "ok" || envelope.Data == nil {
			t.Fatalf("envelope = %+v", envelope)
		}
		if envelope.Data.Root != project {
			t.Fatalf("inspect root = %q, want git root %q", envelope.Data.Root, project)
		}
		if !slices.Contains(envelope.Data.Roots, "firmware") {
			t.Errorf("roots = %v, want the firmware package", envelope.Data.Roots)
		}
		if !inspectPackageNamed(t, envelope.Data, "system").Needed {
			t.Error("sibling package system was not resolved from the git root")
		}
		for _, name := range []string{".vmake", ".gitignore"} {
			if _, err := os.Stat(filepath.Join(packageDir, name)); !os.IsNotExist(err) {
				t.Errorf("inspect created %s in the package subdirectory: %v", name, err)
			}
		}
		if wantRootInitialized {
			if _, err := os.Stat(filepath.Join(project, ".vmake")); err != nil {
				t.Errorf("inspect did not initialize the git root: %v", err)
			}
			if _, err := os.Stat(filepath.Join(project, ".gitignore")); err != nil {
				t.Errorf("inspect did not initialize the git root: %v", err)
			}
		}
	}

	check(t, true)
	check(t, true)
}
