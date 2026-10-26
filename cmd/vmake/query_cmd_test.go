package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestQueryCMakeTargetDirectories(t *testing.T) {
	for _, test := range []struct {
		name    string
		setters string
	}{
		{"default", ""},
		{"relative", `p.SetCMakeBuildDir("cmake objects").SetCMakeInstallDir("cmake stage")`},
		{"absolute", `p.SetCMakeBuildDir(filepath.Join(p.SourceDir(), "cmake objects")).SetCMakeInstallDir(filepath.Join(p.SourceDir(), "cmake stage"))`},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			state := filepath.Join(dir, "state")
			repo := filepath.Join(state, "extensions", "fixture")
			writeExtensionLoader(t, repo)
			writeHealthyDefinition(t, filepath.Join(repo, "healthy", "toolchain.json"), "healthy")
			project := filepath.Join(dir, "工程 space")
			writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), `{"version":"1","global":{"toolchain":"healthy"},"entries":{}}`)
			writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("libc") })
    p.OnBuild(func(ctx *api.BuildContext) { ctx.Target("app").SetKind(api.TargetBinary).AddDeps("libc:libc") })
}
`)
			writeExtensionFile(t, filepath.Join(project, "libc", "build.go"), strings.ReplaceAll(`package main
import (
    "os"
    "path/filepath"
    "github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        __SETTERS__
        include, err := filepath.Rel(p.SourceDir(), filepath.Join(p.CMakeInstallDir(), "include"))
        if err != nil { panic(err) }
        if !filepath.IsAbs(p.CMakeBuildDir()) || !filepath.IsAbs(p.BuildDir()) {
            panic("missing absolute build directory")
        }
        ctx.Target("cmake_build").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error {
            return os.WriteFile(filepath.Join(pkg.SourceDir(), "unexpected-build"), []byte("executed"), 0644)
        })
        ctx.Target("libc").SetKind(api.TargetStatic).AddPublicIncludes(include).
            SetPrebuilt(filepath.Join(p.CMakeInstallDir(), "lib", "libc.a")).AddDeps("cmake_build")
    })
}
`, "__SETTERS__", test.setters))
			for _, args := range [][]string{{"query"}, {"query", "targets"}} {
				out, err := extensionCommand(t, state, project, args...)
				if err != nil {
					t.Fatalf("%v: %v\n%s", args, err, out)
				}
				for _, want := range []string{"app (binary)", "cmake_build (void)", "libc (static)"} {
					if !strings.Contains(out, want) {
						t.Errorf("%v missing %q:\n%s", args, want, out)
					}
				}
			}
			for _, path := range []string{"build", "libc/build", "libc/cmake objects", "libc/cmake stage", "libc/unexpected-build"} {
				if _, err := os.Stat(filepath.Join(project, filepath.FromSlash(path))); !os.IsNotExist(err) {
					t.Errorf("query created %s: %v", path, err)
				}
			}
		})
	}
}

func TestQueryDoesNotInstallToolchains(t *testing.T) {
	for _, test := range []struct {
		name    string
		tools   string
		missing string
	}{
		{"healthy", "", ""},
		{"missing_compiler", `tc.Tools.CC = filepath.Join(ctx.VMakeDir, "missing-cc")`, "missing-cc"},
		{"missing_optional_tool", `tc.Tools.MAKE = filepath.Join(ctx.VMakeDir, "missing-make")`, "missing-make"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			state := filepath.Join(dir, "state")
			pluginDir := filepath.Join(state, "extensions", "fixture", "loader")
			writeExtensionFile(t, filepath.Join(pluginDir, "plugin.json"), `{"name":"fixture","entry":"main.go","enabled":true}`)
			writeExtensionFile(t, filepath.Join(pluginDir, "main.go"), strings.ReplaceAll(`package main
import (
    "fmt"
    "os"
    "path/filepath"
    "github.com/spock2300/vmake/pkg/plugin"
    "github.com/spock2300/vmake/pkg/toolchain"
)
func Main(ctx *plugin.Context) {
    exe, err := os.Executable()
    if err != nil { panic(err) }
    tc := &toolchain.Toolchain{Name: "pending", Tools: toolchain.Tools{CC: exe, CXX: exe, AR: exe, LD: exe}}
    __TOOLS__
    ctx.RegisterToolchain("pending", tc)
    ctx.SetOnMissing("pending", func(name string) (*toolchain.Toolchain, error) {
        if err := os.WriteFile(filepath.Join(ctx.VMakeDir, "installer-called"), []byte("called"), 0644); err != nil {
            return nil, err
        }
        return nil, fmt.Errorf("installer must not run during query")
    })
}
`, "__TOOLS__", test.tools))
			project := filepath.Join(dir, "project")
			writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), `{"version":"1","global":{"toolchain":"pending"},"entries":{}}`)
			writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnBuild(func(ctx *api.BuildContext) { ctx.Target("ready").SetKind(api.TargetVoid) })
}
`)
			for _, args := range [][]string{{"query"}, {"query", "targets"}} {
				out, err := extensionCommand(t, state, project, args...)
				if test.missing == "" {
					if err != nil || !strings.Contains(out, "ready (void)") {
						t.Errorf("%v: %v\n%s", args, err, out)
					}
				} else if err == nil || !strings.Contains(out, test.missing) || !strings.Contains(out, `invalid toolchain "pending"`) {
					t.Errorf("%v did not report unavailable selected tool %q: %v\n%s", args, test.missing, err, out)
				}
				if _, err := os.Stat(filepath.Join(state, "installer-called")); !os.IsNotExist(err) {
					t.Errorf("%v invoked toolchain installer: %v", args, err)
				}
			}
		})
	}
}

func TestQuerySkipsUnmaterializedLocalGitSources(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	repo := filepath.Join(state, "extensions", "fixture")
	writeExtensionLoader(t, repo)
	writeHealthyDefinition(t, filepath.Join(repo, "healthy", "toolchain.json"), "healthy")
	project := filepath.Join(dir, "project")
	writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), `{"version":"1","global":{"toolchain":"healthy"},"entries":{}}`)
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("lib") })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").SetKind(api.TargetBinary).AddDeps("lib:lib")
    })
}
`)
	writeExtensionFile(t, filepath.Join(project, "lib", "build.go"), `package main
import (
    "os"
    "path/filepath"
    "github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
    p.OnPackage(func(p *api.Package) {
        p.SetGit("https://example.invalid/lib.git")
        p.AddVersion("1.0.0", "v1.0.0")
    })
    p.OnBuild(func(ctx *api.BuildContext) {
        if p.BuildDir() == "" {
            return
        }
        if _, err := os.ReadFile(filepath.Join(p.SourceDir(), "src", "lib.c")); err != nil {
            panic(err)
        }
        ctx.Target("lib").SetKind(api.TargetStatic)
    })
}
`)
	for _, args := range [][]string{{"query"}, {"query", "targets"}} {
		out, err := extensionCommand(t, state, project, args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		if strings.Contains(out, "lib (static)") {
			t.Errorf("%v declared targets from an unmaterialized source tree:\n%s", args, out)
		}
		if _, err := os.Stat(filepath.Join(project, ".vmake_deps")); !os.IsNotExist(err) {
			t.Fatalf("%v fetched package sources: %v", args, err)
		}
	}
}

func TestQueryTargetsMatchInspectForRemotePackages(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	state := t.TempDir()
	repoDir := filepath.Join(state, "repos", "official")
	writeExtensionFile(t, filepath.Join(repoDir, "packages", "d", "demo", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://example.invalid/demo.git")
		p.AddVersion("1.0.0", "v1.0.0")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("demo").SetKind(api.TargetStatic).AddFiles("*.c")
	})
}
`)
	runGitLine(t, repoDir, "init", "-q")
	runGitLine(t, repoDir, "config", "user.email", "test@example.com")
	runGitLine(t, repoDir, "config", "user.name", "test")
	runGitLine(t, repoDir, "add", "-A")
	runGitLine(t, repoDir, "commit", "-q", "-m", "registry")

	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("official/demo") })
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddDeps("official/demo:*")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, ".vmake_deps", "official", "demo", "src", "demo.c"), "int demo(void) { return 0; }\n")

	inspected, stderr, code := runInspectCommandWithState(t, project, state, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, inspected)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	demo := inspectPackageNamed(t, envelope.Data, "official/demo")
	if !demo.Needed || !demo.SourcePresent {
		t.Fatalf("remote package = %+v", demo)
	}
	if len(demo.Targets) != 1 || demo.Targets[0].Name != "demo" || demo.Targets[0].Kind != string(api.TargetStatic) {
		t.Fatalf("remote targets = %+v", demo.Targets)
	}

	want := make(map[string]bool)
	for _, pkg := range envelope.Data.Packages {
		if !pkg.Needed {
			continue
		}
		for _, target := range pkg.Targets {
			want[pkg.Name+":"+target.Name+" ("+target.Kind+")"] = true
		}
	}
	listed, stderr, code := runInspectCommandWithState(t, project, state, "query", "targets")
	if code != 0 {
		t.Fatalf("query targets exit = %d, stderr = %q", code, stderr)
	}
	got := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(listed), "\n") {
		if line != "" {
			got[line] = true
		}
	}
	if len(got) != len(want) {
		t.Fatalf("query targets = %v, want %v", got, want)
	}
	for line := range want {
		if !got[line] {
			t.Errorf("query targets missing %q:\n%s", line, listed)
		}
	}

	tree, stderr, code := runInspectCommandWithState(t, project, state, "query")
	if code != 0 {
		t.Fatalf("query exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(tree, "official/demo:demo (static)") {
		t.Errorf("query tree does not label the remote target:\n%s", tree)
	}
}

func TestQueryWarnsAboutMissingNeededSources(t *testing.T) {
	for _, test := range []struct {
		name               string
		args               []string
		missingStdout      []string
		materializedStdout []string
	}{
		{
			name:               "tree",
			args:               []string{"query"},
			missingStdout:      []string{"app (binary) [.]", "└── lib [lib]"},
			materializedStdout: []string{"app (binary) [.]", "└── lib (static) [lib]"},
		},
		{
			name:               "targets",
			args:               []string{"query", "targets"},
			missingStdout:      []string{"proj:app (binary)"},
			materializedStdout: []string{"proj:app (binary)", "lib:lib (static)"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			project := writeQueryMissingSourceProject(t)
			state := t.TempDir()
			warning := "Warning: package lib source is not materialized; the next build will fetch https://example.invalid/lib.git (version 1.0.0)\n"

			stdout, stderr, code := runInspectCommandWithState(t, project, state, test.args...)
			if code != 0 {
				t.Fatalf("%v exit = %d, stderr = %q", test.args, code, stderr)
			}
			if stderr != warning {
				t.Fatalf("%v stderr = %q, want %q", test.args, stderr, warning)
			}
			if strings.Contains(stdout, "Warning:") {
				t.Fatalf("%v wrote the warning to stdout:\n%s", test.args, stdout)
			}
			for _, want := range test.missingStdout {
				if !strings.Contains(stdout, want) {
					t.Errorf("%v stdout missing %q:\n%s", test.args, want, stdout)
				}
			}
			if strings.Contains(stdout, "lib (static)") {
				t.Errorf("%v declared targets from an unmaterialized source tree:\n%s", test.args, stdout)
			}
			if _, err := os.Stat(filepath.Join(project, ".vmake_deps")); !os.IsNotExist(err) {
				t.Fatalf("%v fetched package sources: %v", test.args, err)
			}

			writeExtensionFile(t, filepath.Join(project, "lib", "src", "lib.c"), "int lib(void) { return 1; }\n")
			if _, err := exec.LookPath("git"); err != nil {
				t.Skip("git not on PATH")
			}
			src := filepath.Join(project, "lib", "src")
			runGitLine(t, src, "init", "-q")
			runGitLine(t, src, "config", "user.email", "test@example.com")
			runGitLine(t, src, "config", "user.name", "test")
			runGitLine(t, src, "add", "-A")
			runGitLine(t, src, "commit", "-q", "-m", "materialized")
			stdout, stderr, code = runInspectCommandWithState(t, project, state, test.args...)
			if code != 0 {
				t.Fatalf("%v materialized exit = %d, stderr = %q", test.args, code, stderr)
			}
			if stderr != "" {
				t.Fatalf("%v materialized stderr = %q, want none", test.args, stderr)
			}
			for _, want := range test.materializedStdout {
				if !strings.Contains(stdout, want) {
					t.Errorf("%v materialized stdout missing %q:\n%s", test.args, want, stdout)
				}
			}
		})
	}
}

func writeQueryMissingSourceProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("lib") })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").SetKind(api.TargetBinary).AddDeps("lib:lib")
    })
}
`)
	writeExtensionFile(t, filepath.Join(project, "lib", "build.go"), `package main
import (
    "os"
    "path/filepath"
    "github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
    p.OnPackage(func(p *api.Package) {
        p.SetGit("https://example.invalid/lib.git")
        p.AddVersion("1.0.0", "v1.0.0")
    })
    p.OnBuild(func(ctx *api.BuildContext) {
        if p.BuildDir() == "" {
            return
        }
        if _, err := os.ReadFile(filepath.Join(p.SourceDir(), "src", "lib.c")); err != nil {
            panic(err)
        }
        ctx.Target("lib").SetKind(api.TargetStatic).AddFiles("src/*.c")
    })
}
`)
	return project
}

func TestQueryCompactTreeRendering(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	repo := filepath.Join(state, "extensions", "fixture")
	writeExtensionLoader(t, repo)
	writeHealthyDefinition(t, filepath.Join(repo, "healthy", "toolchain.json"), "healthy")
	project := filepath.Join(dir, "project")
	writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), `{"version":"1","global":{"toolchain":"healthy"},"entries":{}}`)
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnRequire(func(ctx *api.RequireContext) {
        ctx.AddRequires("kernel", "lib", "multi")
        ctx.AddRequires("kernel")
    })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").SetKind(api.TargetBinary)
    })
}
`)
	writeExtensionFile(t, filepath.Join(project, "kernel", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("kernel").SetKind(api.TargetObject)
    })
}
`)
	writeExtensionFile(t, filepath.Join(project, "lib", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("kernel", "crypto") })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("helper").SetKind(api.TargetVoid)
        ctx.Target("lib").SetKind(api.TargetStatic)
    })
}
`)
	writeExtensionFile(t, filepath.Join(project, "multi", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("kernel", "crypto") })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("multi").SetKind(api.TargetVoid)
    })
}
`)
	writeExtensionFile(t, filepath.Join(project, "crypto", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("libcrypto").SetKind(api.TargetStatic)
    })
}
`)
	writeExtensionFile(t, filepath.Join(project, "empty", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
}
`)

	out, err := extensionCommand(t, state, project, "query")
	if err != nil {
		t.Fatalf("query: %v\n%s", err, out)
	}
	want := strings.Join([]string{
		"app (binary) [.]",
		"├── kernel (object) [kernel]",
		"├── lib (static) +helper (void) [lib]",
		"│   ├── kernel (object) (*)",
		"│   └── crypto:libcrypto (static) [crypto]",
		"└── multi (void) [multi]",
		"    ├── kernel (object) (*)",
		"    └── crypto:libcrypto (static) (*)",
	}, "\n")
	if !strings.Contains(out, want) {
		t.Errorf("compact tree missing expected block:\n%s\ngot:\n%s", want, out)
	}
	if strings.Contains(out, "empty") {
		t.Errorf("empty root package was printed:\n%s", out)
	}
	if got := strings.Count(out, "kernel (object) [kernel]"); got != 1 {
		t.Errorf("kernel expanded %d times, want once:\n%s", got, out)
	}
	if got := strings.Count(out, "crypto:libcrypto (static) [crypto]"); got != 1 {
		t.Errorf("crypto expanded %d times, want once:\n%s", got, out)
	}
}

func TestQueryToleratesMissingSourceTargetPanic(t *testing.T) {
	project := writeQueryPanickingSourceProject(t, false)
	state := t.TempDir()
	for _, args := range [][]string{{"query"}, {"query", "targets"}} {
		stdout, stderr, code := runInspectCommandWithState(t, project, state, args...)
		if code != 0 {
			t.Fatalf("%v exit = %d, want 0 (stderr %q)", args, code, stderr)
		}
		if !strings.Contains(stdout, "app (binary)") && !strings.Contains(stdout, "proj:app (binary)") {
			t.Errorf("%v stdout is missing the declared root target:\n%s", args, stdout)
		}
		if strings.Contains(stdout, "lib (static)") {
			t.Errorf("%v declared targets from an unmaterialized source tree:\n%s", args, stdout)
		}
		if strings.Contains(stderr, "panic") || strings.Contains(stderr, "cannot declare targets") {
			t.Errorf("%v ran OnBuild for a package whose source is missing:\n%s", args, stderr)
		}
		if !strings.Contains(stderr, "Warning: package lib source is not materialized") {
			t.Errorf("%v stderr is missing the source warning:\n%s", args, stderr)
		}
		if _, err := os.Stat(filepath.Join(project, ".vmake_deps")); !os.IsNotExist(err) {
			t.Fatalf("%v fetched package sources: %v", args, err)
		}
	}
}

func TestQueryToleratesTargetDeclarationFailure(t *testing.T) {
	project := writeQueryPanickingSourceProject(t, true)
	state := t.TempDir()
	for _, args := range [][]string{{"query"}, {"query", "targets"}} {
		stdout, stderr, code := runInspectCommandWithState(t, project, state, args...)
		if code != 0 {
			t.Fatalf("%v exit = %d, want 0 (stderr %q)", args, code, stderr)
		}
		if !strings.Contains(stdout, "app (binary)") && !strings.Contains(stdout, "proj:app (binary)") {
			t.Errorf("%v stdout is missing the declared root target:\n%s", args, stdout)
		}
		warning := "Warning: cannot declare targets for lib:"
		if !strings.Contains(stderr, warning) {
			t.Fatalf("%v stderr is missing %q:\n%s", args, warning, stderr)
		}
		if got := strings.Count(stderr, warning); got != 1 {
			t.Errorf("%v wrote %d %q warnings, want 1:\n%s", args, got, warning, stderr)
		}
		if strings.Contains(stderr, "Error:") {
			t.Errorf("%v failed instead of degrading:\n%s", args, stderr)
		}
	}
}

func TestQueryReadOnlyWarnings(t *testing.T) {
	project := writeQueryReadOnlyTargetsProject(t)
	for _, args := range [][]string{{"query"}, {"query", "targets"}} {
		state := t.TempDir()
		stdout, stderr, code := runInspectCommandWithState(t, project, state, args...)
		if code != 0 {
			t.Fatalf("%v exit = %d, want 0 (stderr %q)", args, code, stderr)
		}
		for _, name := range []string{"lib", "tool"} {
			if !strings.Contains(stderr, "Warning: cannot declare targets for "+name+":") {
				t.Fatalf("%v stderr is missing the %s per-package warning:\n%s", args, name, stderr)
			}
		}
		if args[0] == "query" && !strings.Contains(stdout, "app (binary)") {
			t.Fatalf("%v is missing the declared root target:\n%s", args, stdout)
		}
		if len(args) > 1 && !strings.Contains(stdout, "proj:app (binary)") {
			t.Fatalf("%v is missing the declared root target:\n%s", args, stdout)
		}
		if strings.Contains(stdout, "lib (static)") || strings.Contains(stdout, "tool (static)") {
			t.Fatalf("%v declared targets for read-only omitted packages:\n%s", args, stdout)
		}
		for _, name := range []string{"lib", "tool"} {
			if _, err := os.Stat(filepath.Join(project, name, "generated.txt")); !os.IsNotExist(err) {
				t.Fatalf("%v wrote %s/generated.txt: %v", args, name, err)
			}
		}
	}
}

func writeQueryReadOnlyTargetsProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("lib", "tool") })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").SetKind(api.TargetBinary).AddDeps("lib:lib", "tool:tool")
    })
}
`)
	for _, name := range []string{"lib", "tool"} {
		writeExtensionFile(t, filepath.Join(project, name, "build.go"), `package main
import (
    "os"
    "github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        _ = os.WriteFile("generated.txt", []byte("generated"), 0644)
        ctx.Target("`+name+`").SetKind(api.TargetStatic)
    })
}
`)
	}
	return project
}

func TestQueryIgnoresBlockedWritesOutsideTargetDeclaration(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import (
    "os"
    "github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnConfig(func(ctx *api.ConfigContext) {
        _ = os.WriteFile("config-generated.txt", []byte("config"), 0644)
    })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").SetKind(api.TargetBinary)
    })
}
`)
	for _, args := range [][]string{{"query"}, {"query", "targets"}} {
		stdout, stderr, code := runInspectCommandWithState(t, project, t.TempDir(), args...)
		if code != 0 {
			t.Fatalf("%v exit = %d, want 0 (stderr %q)", args, code, stderr)
		}
		if !strings.Contains(stdout, "app (binary)") {
			t.Fatalf("%v is missing the declared target:\n%s", args, stdout)
		}
		if strings.Contains(stderr, "cannot declare targets") {
			t.Fatalf("%v attributed a resolution-time blocked write to a package declaration:\n%s", args, stderr)
		}
		if _, err := os.Stat(filepath.Join(project, "config-generated.txt")); !os.IsNotExist(err) {
			t.Fatalf("%v wrote config-generated.txt: %v", args, err)
		}
	}
}

func writeQueryPanickingSourceProject(t *testing.T, materialize bool) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("lib") })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").SetKind(api.TargetBinary).AddDeps("lib:lib")
    })
}
`)
	libDir := filepath.Join(project, "lib")
	pkgDecl := ""
	if !materialize {
		pkgDecl = `    p.OnPackage(func(p *api.Package) {
        p.SetGit("https://example.invalid/lib.git")
        p.AddVersion("1.0.0", "v1.0.0")
    })
`
	}
	writeExtensionFile(t, filepath.Join(libDir, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
`+pkgDecl+`    p.OnBuild(func(ctx *api.BuildContext) {
        panic("declaration failure")
    })
}
`)
	return project
}
