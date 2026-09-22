package main

import (
	"os"
	"path/filepath"
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
    "runtime"
    "github.com/spock2300/vmake/pkg/plugin"
    "github.com/spock2300/vmake/pkg/toolchain"
)
func Main(ctx *plugin.Context) {
    exe, err := os.Executable()
    if err != nil { panic(err) }
    tc := &toolchain.Toolchain{Name: "pending", TargetOS: runtime.GOOS, Tools: toolchain.Tools{CC: exe, CXX: exe, AR: exe, LD: exe}}
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
