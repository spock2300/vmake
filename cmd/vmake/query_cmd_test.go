package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
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
		wantSummary := "Warning: omitted targets for 1 package(s) because target declaration failed; fix: Fix the package OnBuild callback, then rerun vmake query."
		if !strings.HasSuffix(stderr, wantSummary+"\n") {
			t.Fatalf("%v stderr = %q, want the omission aggregate last", args, stderr)
		}
	}
}

func TestQueryReadOnlyOmissionSummary(t *testing.T) {
	project := writeQueryReadOnlyTargetsProject(t)
	wantSummary := "Warning: omitted targets for 2 package(s) (2 due to read-only writes); fix: Guard generated-file work with p.DryRun(), or run vmake query --allow-dry-run-writes when those writes are expected."
	for _, args := range [][]string{{"query"}, {"query", "targets"}} {
		state := t.TempDir()
		stdout, stderr, code := runInspectCommandWithState(t, project, state, args...)
		if code != 0 {
			t.Fatalf("%v exit = %d, want 0 (stderr %q)", args, code, stderr)
		}
		if got := strings.Count(stderr, "Warning: omitted targets for "); got != 1 {
			t.Fatalf("%v stderr has %d summary lines, want 1:\n%s", args, got, stderr)
		}
		if !strings.HasSuffix(stderr, wantSummary+"\n") {
			t.Fatalf("%v stderr = %q, want the omission summary last", args, stderr)
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

func writeQueryMixedOmissionProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("lib", "broken") })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app").SetKind(api.TargetBinary).AddDeps("lib:lib", "broken:broken")
    })
}
`)
	writeExtensionFile(t, filepath.Join(project, "lib", "build.go"), `package main
import (
    "os"
    "github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        _ = os.WriteFile("generated.txt", []byte("generated"), 0644)
        ctx.Target("lib").SetKind(api.TargetStatic)
    })
}
`)
	writeExtensionFile(t, filepath.Join(project, "broken", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) { panic("declaration failure") })
}
`)
	return project
}

func TestQueryOmissionAggregateCountsAllDeclarationFailures(t *testing.T) {
	project := writeQueryMixedOmissionProject(t)
	wantSummary := "Warning: omitted targets for 2 package(s) (1 due to read-only writes); fix: Guard generated-file work with p.DryRun(), or run vmake query --allow-dry-run-writes when those writes are expected."
	for _, args := range [][]string{{"query"}, {"query", "targets"}} {
		stdout, stderr, code := runInspectCommandWithState(t, project, t.TempDir(), args...)
		if code != 0 {
			t.Fatalf("%v exit = %d, want 0 (stderr %q)", args, code, stderr)
		}
		if got := strings.Count(stderr, "Warning: omitted targets for "); got != 1 {
			t.Fatalf("%v stderr has %d summary lines, want 1:\n%s", args, got, stderr)
		}
		if !strings.HasSuffix(stderr, wantSummary+"\n") {
			t.Fatalf("%v stderr = %q, want the aggregate summary last", args, stderr)
		}
		for _, name := range []string{"lib", "broken"} {
			if !strings.Contains(stderr, "Warning: cannot declare targets for "+name+":") {
				t.Fatalf("%v stderr is missing the %s per-package warning:\n%s", args, name, stderr)
			}
		}
		if !strings.Contains(stderr, "read-only inspection:") || !strings.Contains(stderr, "declaration failure") {
			t.Fatalf("%v stderr does not name both omission causes:\n%s", args, stderr)
		}
		if strings.Contains(stdout, "lib (static)") || strings.Contains(stdout, "broken (") {
			t.Fatalf("%v declared targets for omitted packages:\n%s", args, stdout)
		}
		if args[0] == "query" && !strings.Contains(stdout, "app (binary)") {
			t.Fatalf("%v is missing the declared root target:\n%s", args, stdout)
		}
		if len(args) > 1 && !strings.Contains(stdout, "proj:app (binary)") {
			t.Fatalf("%v is missing the declared root target:\n%s", args, stdout)
		}
		if _, err := os.Stat(filepath.Join(project, "lib", "generated.txt")); !os.IsNotExist(err) {
			t.Fatalf("%v wrote lib/generated.txt: %v", args, err)
		}
	}
}

func TestQueryTargetsJSONOmissionAggregateCountsAllDeclarationFailures(t *testing.T) {
	project := writeQueryMixedOmissionProject(t)
	stdout, stderr, code := runInspectCommandWithState(t, project, t.TempDir(), "query", "targets", "--json")
	if code != 0 {
		t.Fatalf("query targets --json exit = %d (stderr %q)", code, stderr)
	}
	if strings.Contains(stderr, "Warning:") {
		t.Fatalf("JSON mode printed human warnings on stderr:\n%s", stderr)
	}
	document := decodeQueryTargetsDocument(t, stdout)
	if document.Status != "ok" || document.Data == nil {
		t.Fatalf("unexpected envelope: %+v", document)
	}
	if !reflect.DeepEqual(document.Data.Targets, []queryTargetsTestEntry{{Package: "proj", Target: "app", Kind: "binary"}}) {
		t.Fatalf("targets = %#v, want only the root target", document.Data.Targets)
	}
	perPackage := 0
	var aggregate *queryTargetsTestDiagnostic
	for i := range document.Diagnostics {
		diagnostic := &document.Diagnostics[i]
		switch diagnostic.Code {
		case "VMAKE_W_TARGET_DECLARE":
			perPackage++
		case "VMAKE_W_OMITTED_TARGETS":
			aggregate = diagnostic
		default:
			t.Fatalf("unexpected diagnostic %+v", diagnostic)
		}
	}
	if perPackage != 2 || aggregate == nil {
		t.Fatalf("diagnostics = %+v, want two per-package warnings and one aggregate", document.Diagnostics)
	}
	packages := make(map[string]queryTargetsTestDiagnostic)
	for _, diagnostic := range document.Diagnostics {
		if diagnostic.Package != "" {
			packages[diagnostic.Package] = diagnostic
		}
	}
	if !strings.Contains(packages["lib"].Message, "read-only inspection:") {
		t.Fatalf("lib diagnostic = %+v", packages["lib"])
	}
	if !strings.Contains(packages["broken"].Message, "declaration failure") {
		t.Fatalf("broken diagnostic = %+v", packages["broken"])
	}
	if !strings.Contains(packages["broken"].Fix, "vmake query targets") {
		t.Fatalf("broken fix = %q", packages["broken"].Fix)
	}
	if aggregate.Message != "omitted targets for 2 package(s) (1 due to read-only writes)" {
		t.Fatalf("aggregate message = %q", aggregate.Message)
	}
	if !strings.Contains(aggregate.Fix, "vmake query --allow-dry-run-writes") {
		t.Fatalf("aggregate fix = %q", aggregate.Fix)
	}
	if _, err := os.Stat(filepath.Join(project, "lib", "generated.txt")); !os.IsNotExist(err) {
		t.Fatalf("query targets --json wrote lib/generated.txt: %v", err)
	}
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
		if strings.Contains(stderr, "cannot declare targets") || strings.Contains(stderr, "omitted targets for ") {
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

type queryTargetsTestEntry struct {
	Package string `json:"package"`
	Target  string `json:"target"`
	Kind    string `json:"kind"`
}

type queryTargetsTestDiagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Package  string `json:"package"`
	Fix      string `json:"fix"`
}

type queryTargetsTestDocument struct {
	SchemaVersion int    `json:"schemaVersion"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	Data          *struct {
		Targets []queryTargetsTestEntry `json:"targets"`
	} `json:"data"`
	Diagnostics []queryTargetsTestDiagnostic `json:"diagnostics"`
}

func decodeQueryTargetsDocument(t *testing.T, stdout string) *queryTargetsTestDocument {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(stdout))
	var document queryTargetsTestDocument
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout has content after one JSON document: %v\n%s", err, stdout)
	}
	return &document
}

func parseQueryTargetsHuman(t *testing.T, stdout string) []queryTargetsTestEntry {
	t.Helper()
	entries := make([]queryTargetsTestEntry, 0)
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		open := strings.Index(line, " (")
		if open < 0 || !strings.HasSuffix(line, ")") {
			t.Fatalf("unexpected human target line %q", line)
		}
		pkg, target, ok := strings.Cut(line[:open], ":")
		if !ok || pkg == "" || target == "" {
			t.Fatalf("unexpected human target line %q", line)
		}
		entries = append(entries, queryTargetsTestEntry{Package: pkg, Target: target, Kind: strings.TrimSuffix(line[open+2:], ")")})
	}
	return entries
}

func writeQueryTargetsProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("zlib", "alpha") })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("zeta").SetKind(api.TargetVoid)
        ctx.Target("app").SetKind(api.TargetBinary).AddDeps("alpha:alpha", "zlib:zlib")
    })
}
`)
	writeExtensionFile(t, filepath.Join(project, "alpha", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("beta").SetKind(api.TargetObject)
        ctx.Target("alpha").SetKind(api.TargetStatic)
    })
}
`)
	writeExtensionFile(t, filepath.Join(project, "zlib", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) { ctx.Target("zlib").SetKind(api.TargetStatic) })
}
`)
	return project
}

func TestQueryTargetsJSONDocumentAndHumanParity(t *testing.T) {
	project := writeQueryTargetsProject(t)
	state := t.TempDir()

	humanOut, humanErr, code := runInspectCommandWithState(t, project, state, "query", "targets")
	if code != 0 {
		t.Fatalf("human query targets exit = %d (stderr %q)", code, humanErr)
	}
	if humanErr != "" {
		t.Fatalf("human query targets stderr = %q, want none", humanErr)
	}
	human := parseQueryTargetsHuman(t, humanOut)
	sort.Slice(human, func(i, j int) bool {
		if human[i].Package != human[j].Package {
			return human[i].Package < human[j].Package
		}
		return human[i].Target < human[j].Target
	})

	stdout, stderr, code := runInspectCommandWithState(t, project, state, "query", "targets", "--json")
	if code != 0 {
		t.Fatalf("query targets --json exit = %d (stderr %q)", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("query targets --json stderr = %q, want none", stderr)
	}
	if strings.Contains(stdout, "(binary)") {
		t.Fatalf("JSON stdout contains human target lines:\n%s", stdout)
	}
	document := decodeQueryTargetsDocument(t, stdout)
	if document.SchemaVersion != 1 || document.Command != "query targets" || document.Status != "ok" {
		t.Fatalf("unexpected envelope: %+v", document)
	}
	if document.Diagnostics == nil || len(document.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want an empty array", document.Diagnostics)
	}
	want := []queryTargetsTestEntry{
		{Package: "alpha", Target: "alpha", Kind: "static"},
		{Package: "alpha", Target: "beta", Kind: "object"},
		{Package: "proj", Target: "app", Kind: "binary"},
		{Package: "proj", Target: "zeta", Kind: "void"},
		{Package: "zlib", Target: "zlib", Kind: "static"},
	}
	if document.Data == nil || !reflect.DeepEqual(document.Data.Targets, want) {
		t.Fatalf("targets = %#v, want %#v", document.Data, want)
	}
	if !reflect.DeepEqual(document.Data.Targets, human) {
		t.Fatalf("JSON targets = %#v, human targets = %#v", document.Data.Targets, human)
	}

	quietOut, quietErr, code := runInspectCommandWithState(t, project, state, "-q", "query", "targets", "--json")
	if code != 0 || quietErr != "" {
		t.Fatalf("-q query targets --json exit = %d, stderr = %q", code, quietErr)
	}
	if quietOut != stdout {
		t.Fatalf("-q changed the JSON document:\n%s\nwant:\n%s", quietOut, stdout)
	}
}

func TestQueryTargetsJSONReadOnlyOmissionDiagnostics(t *testing.T) {
	project := writeQueryReadOnlyTargetsProject(t)
	state := t.TempDir()

	stdout, stderr, code := runInspectCommandWithState(t, project, state, "query", "targets", "--json")
	if code != 0 {
		t.Fatalf("query targets --json exit = %d (stderr %q)", code, stderr)
	}
	if strings.Contains(stderr, "Warning:") {
		t.Fatalf("JSON mode printed human warnings on stderr:\n%s", stderr)
	}
	document := decodeQueryTargetsDocument(t, stdout)
	if document.Status != "ok" || document.Data == nil {
		t.Fatalf("unexpected envelope: %+v", document)
	}
	if !reflect.DeepEqual(document.Data.Targets, []queryTargetsTestEntry{{Package: "proj", Target: "app", Kind: "binary"}}) {
		t.Fatalf("targets = %#v, want only the root target", document.Data.Targets)
	}
	codes := make([]string, 0, len(document.Diagnostics))
	for _, diagnostic := range document.Diagnostics {
		codes = append(codes, diagnostic.Code)
		if diagnostic.Severity != "warning" {
			t.Fatalf("diagnostic %+v is not a warning", diagnostic)
		}
	}
	if !reflect.DeepEqual(codes, []string{"VMAKE_W_TARGET_DECLARE", "VMAKE_W_TARGET_DECLARE", "VMAKE_W_OMITTED_TARGETS"}) {
		t.Fatalf("diagnostic codes = %v", codes)
	}
	for i, name := range []string{"lib", "tool"} {
		diagnostic := document.Diagnostics[i]
		if diagnostic.Package != name || !strings.Contains(diagnostic.Message, "cannot declare targets for "+name+":") {
			t.Fatalf("diagnostic %d = %+v, want package %s", i, diagnostic, name)
		}
		if !strings.Contains(diagnostic.Fix, "vmake query --allow-dry-run-writes") {
			t.Fatalf("diagnostic %d fix = %q", i, diagnostic.Fix)
		}
	}
	summary := document.Diagnostics[2]
	if summary.Message != "omitted targets for 2 package(s) (2 due to read-only writes)" {
		t.Fatalf("summary diagnostic = %+v", summary)
	}
	for _, name := range []string{"lib", "tool"} {
		if _, err := os.Stat(filepath.Join(project, name, "generated.txt")); !os.IsNotExist(err) {
			t.Fatalf("query targets --json wrote %s/generated.txt: %v", name, err)
		}
	}

	allowOut, allowErr, code := runInspectCommandWithState(t, project, state, "query", "targets", "--json", "--allow-dry-run-writes")
	if code != 0 {
		t.Fatalf("query targets --json --allow-dry-run-writes exit = %d (stderr %q)", code, allowErr)
	}
	allowed := decodeQueryTargetsDocument(t, allowOut)
	if len(allowed.Diagnostics) != 0 {
		t.Fatalf("--allow-dry-run-writes diagnostics = %+v, want none", allowed.Diagnostics)
	}
	if allowed.Data == nil || len(allowed.Data.Targets) != 3 {
		t.Fatalf("--allow-dry-run-writes targets = %#v, want lib, proj and tool", allowed.Data)
	}
	for _, name := range []string{"lib", "tool"} {
		if _, err := os.Stat(filepath.Join(project, name, "generated.txt")); err != nil {
			t.Fatalf("--allow-dry-run-writes did not write %s/generated.txt: %v", name, err)
		}
	}
}

func TestQueryTargetsJSONMissingSourceDiagnostic(t *testing.T) {
	project := writeQueryMissingSourceProject(t)
	stdout, stderr, code := runInspectCommandWithState(t, project, t.TempDir(), "query", "targets", "--json")
	if code != 0 {
		t.Fatalf("query targets --json exit = %d (stderr %q)", code, stderr)
	}
	if strings.Contains(stderr, "Warning:") {
		t.Fatalf("JSON mode printed human warnings on stderr:\n%s", stderr)
	}
	document := decodeQueryTargetsDocument(t, stdout)
	if document.Status != "ok" || document.Data == nil {
		t.Fatalf("unexpected envelope: %+v", document)
	}
	if !reflect.DeepEqual(document.Data.Targets, []queryTargetsTestEntry{{Package: "proj", Target: "app", Kind: "binary"}}) {
		t.Fatalf("targets = %#v", document.Data.Targets)
	}
	if len(document.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want one missing-source warning", document.Diagnostics)
	}
	diagnostic := document.Diagnostics[0]
	if diagnostic.Severity != "warning" || diagnostic.Code != "VMAKE_W_SOURCE_MISSING" || diagnostic.Package != "lib" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	if !strings.Contains(diagnostic.Message, "not materialized") {
		t.Fatalf("diagnostic message = %q", diagnostic.Message)
	}
	if _, err := os.Stat(filepath.Join(project, ".vmake_deps")); !os.IsNotExist(err) {
		t.Fatalf("query targets --json fetched package sources: %v", err)
	}
}

func TestQueryTargetsJSONResolutionError(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnConfig(func(ctx *api.ConfigContext) { panic("configuration exploded") })
    p.OnBuild(func(ctx *api.BuildContext) { ctx.Target("app").SetKind(api.TargetBinary) })
}
`)
	stdout, stderr, code := runInspectCommandWithState(t, project, t.TempDir(), "query", "targets", "--json")
	if code != 1 {
		t.Fatalf("query targets --json exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	document := decodeQueryTargetsDocument(t, stdout)
	if document.SchemaVersion != 1 || document.Command != "query targets" || document.Status != "error" {
		t.Fatalf("unexpected envelope: %+v", document)
	}
	if document.Data != nil {
		t.Fatalf("error envelope carried a partial target document: %+v", document.Data)
	}
	if len(document.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want exactly one error", document.Diagnostics)
	}
	diagnostic := document.Diagnostics[0]
	if diagnostic.Severity != "error" || diagnostic.Code != "VMAKE_E_QUERY" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	if !strings.Contains(diagnostic.Message, "configuration exploded") {
		t.Fatalf("diagnostic message = %q", diagnostic.Message)
	}
	if !strings.Contains(stderr, "Error:") {
		t.Fatalf("stderr = %q, want one human error line", stderr)
	}
}

func TestQueryTargetsJSONDeclarationFailureDiagnostic(t *testing.T) {
	project := writeQueryPanickingSourceProject(t, true)
	stdout, stderr, code := runInspectCommandWithState(t, project, t.TempDir(), "query", "targets", "--json")
	if code != 0 {
		t.Fatalf("query targets --json exit = %d (stderr %q)", code, stderr)
	}
	if strings.Contains(stderr, "panic") || strings.Contains(stderr, "Warning:") {
		t.Fatalf("JSON mode printed declaration noise on stderr:\n%s", stderr)
	}
	document := decodeQueryTargetsDocument(t, stdout)
	if document.Status != "ok" || document.Data == nil {
		t.Fatalf("unexpected envelope: %+v", document)
	}
	if !reflect.DeepEqual(document.Data.Targets, []queryTargetsTestEntry{{Package: "proj", Target: "app", Kind: "binary"}}) {
		t.Fatalf("targets = %#v, want only the root target", document.Data.Targets)
	}
	if len(document.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v, want a declaration warning and the aggregate", document.Diagnostics)
	}
	diagnostic := document.Diagnostics[0]
	if diagnostic.Severity != "warning" || diagnostic.Code != "VMAKE_W_TARGET_DECLARE" || diagnostic.Package != "lib" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	if !strings.Contains(diagnostic.Message, "declaration failure") || !strings.Contains(diagnostic.Fix, "Fix the package OnBuild callback") {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	aggregate := document.Diagnostics[1]
	if aggregate.Code != "VMAKE_W_OMITTED_TARGETS" ||
		aggregate.Message != "omitted targets for 1 package(s) because target declaration failed" ||
		!strings.Contains(aggregate.Fix, "vmake query targets") {
		t.Fatalf("aggregate diagnostic = %+v", aggregate)
	}
}
