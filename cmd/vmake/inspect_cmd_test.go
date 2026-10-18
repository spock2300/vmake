package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestInspectBootstrapChild(t *testing.T) {
	dir := os.Getenv("VMAKE_TEST_INSPECT_DIR")
	if dir == "" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing command arguments")
	}
	vmakeDir = dir
	os.Args = append([]string{"vmake"}, os.Args[separator+1:]...)
	main()
	os.Exit(0)
}

func runInspectCommand(t *testing.T, projectDir string, args ...string) (string, string, int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commandArgs := append([]string{"-test.run=^TestInspectBootstrapChild$", "--"}, args...)
	cmd := exec.Command(exe, commandArgs...)
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(),
		"VMAKE_TEST_INSPECT_DIR="+t.TempDir(),
		"VMAKE_CACHE="+t.TempDir(),
		"VMAKE_TRUST_ALL=1",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run inspect: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

func decodeInspectEnvelope(t *testing.T, stdout string) *inspectEnvelope {
	t.Helper()
	var envelope inspectEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	return &envelope
}

func inspectPackageNamed(t *testing.T, data *inspectData, name string) *inspectPackage {
	t.Helper()
	for i := range data.Packages {
		if data.Packages[i].Name == name {
			return &data.Packages[i]
		}
	}
	t.Fatalf("package %q missing from inspect model", name)
	return nil
}

func inspectTargetNamed(t *testing.T, pkg *inspectPackage, name string) *inspectTarget {
	t.Helper()
	for i := range pkg.Targets {
		if pkg.Targets[i].Name == name {
			return &pkg.Targets[i]
		}
	}
	t.Fatalf("target %q missing from package %q", name, pkg.Name)
	return nil
}

func writeInspectProject(t *testing.T, dir string) string {
	t.Helper()
	project := filepath.Join(dir, "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("with_dep").
			SetType(api.OptionBool).
			SetDefault(true).
			SetDescription("Link the dependency")
		ctx.Option("retries").
			SetType(api.OptionInt).
			SetDefault(3)
		ctx.Option("label").
			SetType(api.OptionString).
			SetDefault("default")
	})
	p.OnRequire(func(ctx *api.RequireContext) {
		if ctx.When("with_dep", true) {
			ctx.AddRequires("dep")
		}
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		target := ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddIncludes("include").
			AddPublicIncludes("include").
			AddDefines("APP_DEFINE=1")
		if ctx.When("with_dep", true) {
			target.AddDeps("dep:deplib")
		}
		ctx.Target("docs").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error { return nil })
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	writeExtensionFile(t, filepath.Join(project, "include", "app.h"), "#define APP 1\n")
	writeExtensionFile(t, filepath.Join(project, "dep", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("dep_mode").
			SetType(api.OptionChoice).
			SetValues("fast", "slow").
			SetDefault("fast")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("deplib").
			SetKind(api.TargetStatic).
			AddFiles("src/*.c").
			AddPublicIncludes("include")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "dep", "src", "lib.c"), "int dep(void) { return 1; }\n")
	writeExtensionFile(t, filepath.Join(project, "dep", "include", "dep.h"), "#define DEP 1\n")
	return project
}

func TestInspectProjectModel(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %s", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("successful inspect wrote stderr: %q", stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.SchemaVersion != inspectSchemaVersion || envelope.Command != "inspect" || envelope.Status != "ok" {
		t.Fatalf("envelope = %+v", envelope)
	}
	if envelope.Data == nil {
		t.Fatal("successful inspect has no data")
	}
	if envelope.Diagnostics == nil || len(envelope.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want []", envelope.Diagnostics)
	}
	data := envelope.Data
	if data.Root != project || data.ConfigFile != "config.json" || data.Mode != "debug" || data.Toolchain != "host" {
		t.Fatalf("data header = %+v", data)
	}
	if want := filepath.Join(project, "build", "compile_commands.json"); data.CompileDatabase != want {
		t.Errorf("compileDatabase = %q, want %q", data.CompileDatabase, want)
	}
	if want := filepath.Join(project, "build", "vmake-build.json"); data.BuildReport != want {
		t.Errorf("buildReport = %q, want %q", data.BuildReport, want)
	}
	if !slices.Equal(data.Roots, []string{"proj"}) {
		t.Errorf("roots = %v", data.Roots)
	}
	if data.ToolchainInfo == nil || data.ToolchainInfo.Name != "host" || data.ToolchainInfo.CC == "" {
		t.Errorf("toolchainInfo = %+v", data.ToolchainInfo)
	}
	if data.Global["mode"] != "debug" || data.Global["toolchain"] != "host" {
		t.Errorf("global = %v", data.Global)
	}
	if data.Overrides != nil {
		t.Errorf("unexpected overrides: %v", data.Overrides)
	}
	if !slices.Equal(data.Graph, []inspectEdge{{From: "proj", To: "dep"}}) {
		t.Errorf("graph = %v", data.Graph)
	}

	proj := inspectPackageNamed(t, data, "proj")
	if !proj.Needed || proj.Kind != "local" || proj.Path != "." || proj.Toolchain != "host" {
		t.Errorf("root package = %+v", proj)
	}
	if !slices.Equal(proj.Deps, []string{"dep"}) {
		t.Errorf("root deps = %v", proj.Deps)
	}
	if proj.Options["with_dep"] != true || proj.Options["retries"] != float64(3) || proj.Options["label"] != "default" {
		t.Errorf("root options = %v", proj.Options)
	}
	app := inspectTargetNamed(t, proj, "app")
	if app.Kind != "binary" || !app.Default || app.Test || app.Output != "app" {
		t.Errorf("app target = %+v", app)
	}
	if !slices.Equal(app.Files, []string{"src/*.c"}) || !slices.Equal(app.Includes, []string{"include"}) ||
		!slices.Equal(app.PublicIncludes, []string{"include"}) || !slices.Equal(app.Defines, []string{"APP_DEFINE=1"}) ||
		!slices.Equal(app.Deps, []string{"dep:deplib"}) {
		t.Errorf("app target fields = %+v", app)
	}
	docs := inspectTargetNamed(t, proj, "docs")
	if docs.Kind != "void" || docs.Output != "" || docs.Files != nil {
		t.Errorf("docs target = %+v", docs)
	}

	dep := inspectPackageNamed(t, data, "dep")
	if !dep.Needed || dep.Kind != "local" || dep.Path != "dep" || dep.Toolchain != "host" {
		t.Errorf("dep package = %+v", dep)
	}
	if dep.Options["dep_mode"] != "fast" {
		t.Errorf("dep options = %v", dep.Options)
	}
	deplib := inspectTargetNamed(t, dep, "deplib")
	if deplib.Kind != "static" || deplib.Output != "libdeplib.a" || !slices.Equal(deplib.PublicIncludes, []string{"include"}) {
		t.Errorf("deplib target = %+v", deplib)
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("second inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("consecutive inspect runs are not byte-identical")
	}
	for _, dir := range []string{"build", "install"} {
		if _, err := os.Stat(filepath.Join(project, dir)); !os.IsNotExist(err) {
			t.Fatalf("inspect created %s: %v", dir, err)
		}
	}
}

func writeInspectStdoutProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import (
	"fmt"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		fmt.Println("INSPECT_ONCONFIG_STDOUT")
		ctx.Option("gate").SetType(api.OptionBool).SetDefault(true)
	})
	p.OnRequire(func(ctx *api.RequireContext) {
		fmt.Println("INSPECT_ONREQUIRE_STDOUT")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		fmt.Println("INSPECT_ONBUILD_STDOUT")
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	return project
}

func TestInspectSuppressesBuildscriptStdout(t *testing.T) {
	project := writeInspectStdoutProject(t)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, "INSPECT_") {
		t.Fatalf("buildscript stdout leaked into the JSON document:\n%s", stdout)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Data.Roots) != 1 || envelope.Data.Roots[0] != "proj" {
		t.Fatalf("roots = %v", envelope.Data.Roots)
	}

	stdout, stderr, code = runInspectCommand(t, project, "inspect", "--set", "proj/gate=maybe")
	if code != 1 {
		t.Fatalf("invalid --set exit = %d, want 1 (stderr %q)", code, stderr)
	}
	if strings.Contains(stdout, "INSPECT_") {
		t.Fatalf("buildscript stdout leaked into the error envelope:\n%s", stdout)
	}
	envelope = decodeInspectEnvelope(t, stdout)
	if envelope.Status != "error" || envelope.Data != nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_E_OPTION" {
		t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
	}
	if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "Error: ") {
		t.Fatalf("stderr = %q, want one human error line", stderr)
	}

	t.Run("configure_failure", func(t *testing.T) {
		broken := filepath.Join(t.TempDir(), "broken")
		writeExtensionFile(t, filepath.Join(broken, "build.go"), `package main

import (
	"fmt"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		fmt.Println("INSPECT_BROKEN_STDOUT")
		panic("configure failure")
	})
}
`)
		stdout, stderr, code := runInspectCommand(t, broken, "inspect")
		if code != 1 {
			t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
		}
		if strings.Contains(stdout, "INSPECT_") {
			t.Fatalf("buildscript stdout leaked into the error envelope:\n%s", stdout)
		}
		envelope := decodeInspectEnvelope(t, stdout)
		if envelope.Status != "error" || envelope.Data != nil {
			t.Fatalf("envelope = %+v", envelope)
		}
		if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_E_INSPECT" {
			t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
		}
		if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "Error: ") {
			t.Fatalf("stderr = %q, want one human error line", stderr)
		}
	})
}

func TestInspectSetOverridesAreHypothetical(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())
	configPath := filepath.Join(project, ".vmake", "config.json")

	stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/with_dep=false")
	if code != 0 || stderr != "" {
		t.Fatalf("override inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("override envelope = %+v", envelope)
	}
	data := envelope.Data
	if data.Overrides["proj/with_dep"] != false {
		t.Errorf("overrides = %v", data.Overrides)
	}
	proj := inspectPackageNamed(t, data, "proj")
	if proj.Options["with_dep"] != false {
		t.Errorf("effective with_dep = %v", proj.Options["with_dep"])
	}
	dep := inspectPackageNamed(t, data, "dep")
	if dep.Needed || dep.Targets != nil {
		t.Errorf("dep should not be needed: %+v", dep)
	}
	if len(data.Graph) != 0 {
		t.Errorf("graph = %v, want empty", data.Graph)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("%s was written by inspect --set: %v", configPath, err)
	}

	stdout, stderr, code = runInspectCommand(t, project, "inspect", "--set", "dep/dep_mode=slow")
	if code != 0 || stderr != "" {
		t.Fatalf("choice override exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	if data.Overrides["dep/dep_mode"] != "slow" {
		t.Errorf("overrides = %v", data.Overrides)
	}
	if got := inspectPackageNamed(t, data, "dep").Options["dep_mode"]; got != "slow" {
		t.Errorf("effective dep_mode = %v", got)
	}

	stdout, stderr, code = runInspectCommand(t, project, "inspect", "--set", "mode=release")
	if code != 0 || stderr != "" {
		t.Fatalf("global override exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	if data.Mode != "release" || data.Global["mode"] != "release" || data.Overrides["mode"] != "release" {
		t.Errorf("global override not applied: mode=%q global=%v overrides=%v", data.Mode, data.Global, data.Overrides)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("%s was written by inspect --set: %v", configPath, err)
	}
}

func TestInspectSetTypedCoercion(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())
	configPath := filepath.Join(project, ".vmake", "config.json")

	for _, raw := range []string{"42", "true", "on", "false", ""} {
		t.Run("string_"+raw, func(t *testing.T) {
			stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/label="+raw)
			if code != 0 || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			data := decodeInspectEnvelope(t, stdout).Data
			if data.Overrides["proj/label"] != raw {
				t.Errorf("overrides = %#v, want string %q", data.Overrides, raw)
			}
			if got := inspectPackageNamed(t, data, "proj").Options["label"]; got != raw {
				t.Errorf("effective label = %#v, want string %q", got, raw)
			}
		})
	}

	for _, test := range []struct {
		raw  string
		want bool
	}{
		{"true", true}, {"false", false},
		{"on", true}, {"off", false},
		{"1", true}, {"0", false},
	} {
		t.Run("bool_"+test.raw, func(t *testing.T) {
			stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/with_dep="+test.raw)
			if code != 0 || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			data := decodeInspectEnvelope(t, stdout).Data
			if data.Overrides["proj/with_dep"] != test.want {
				t.Errorf("overrides = %#v, want bool %v", data.Overrides, test.want)
			}
			if got := inspectPackageNamed(t, data, "proj").Options["with_dep"]; got != test.want {
				t.Errorf("effective with_dep = %#v, want bool %v", got, test.want)
			}
		})
	}

	stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/retries=42")
	if code != 0 || stderr != "" {
		t.Fatalf("int override exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	if data.Overrides["proj/retries"] != float64(42) {
		t.Errorf("overrides = %#v, want number 42", data.Overrides)
	}
	if got := inspectPackageNamed(t, data, "proj").Options["retries"]; got != float64(42) {
		t.Errorf("effective retries = %#v, want number 42", got)
	}

	first, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/label=42")
	if code != 0 || stderr != "" {
		t.Fatalf("first typed run exit = %d, stderr = %q", code, stderr)
	}
	again, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/label=42")
	if code != 0 || stderr != "" {
		t.Fatalf("second typed run exit = %d, stderr = %q", code, stderr)
	}
	if again != first {
		t.Fatal("consecutive inspect --set runs are not byte-identical")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("%s was written by inspect --set: %v", configPath, err)
	}
	for _, dir := range []string{"build", "install"} {
		if _, err := os.Stat(filepath.Join(project, dir)); !os.IsNotExist(err) {
			t.Fatalf("inspect --set created %s: %v", dir, err)
		}
	}
}

func TestCoerceInspectOverridesLeavesUndeclaredOptionsRaw(t *testing.T) {
	overrides, err := coerceInspectOverrides([]inspectOverride{
		{pkgName: "proj", optName: "lazy", raw: "on"},
		{pkgName: "proj", optName: "label", raw: "42"},
	}, &inspectOptionDeclarations{
		packageOptions: map[string]map[string]*api.Option{
			"proj": {"label": (&api.Option{}).SetType(api.OptionString)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if overrides[0].value != "on" {
		t.Errorf("undeclared option value = %#v, want raw string \"on\"", overrides[0].value)
	}
	if overrides[1].value != "42" {
		t.Errorf("declared string option value = %#v, want \"42\"", overrides[1].value)
	}

	overrides, err = coerceInspectOverrides([]inspectOverride{{pkgName: "proj", optName: "lazy", raw: "on"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if overrides[0].value != "on" {
		t.Errorf("nil declarations value = %#v, want raw string \"on\"", overrides[0].value)
	}
}

func TestInspectSetErrors(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())
	configPath := filepath.Join(project, ".vmake", "config.json")
	for _, test := range []struct {
		name string
		args []string
	}{
		{"syntax", []string{"--set", "proj/with_dep"}},
		{"unknown_package", []string{"--set", "nope/x=1"}},
		{"unknown_option", []string{"--set", "proj/nope=1"}},
		{"unknown_global", []string{"--set", "nope=1"}},
		{"wrong_type", []string{"--set", "proj/with_dep=maybe"}},
		{"wrong_type_string", []string{"--set", "proj/retries=quick"}},
		{"invalid_choice", []string{"--set", "dep/dep_mode=warp"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, code := runInspectCommand(t, project, append([]string{"inspect"}, test.args...)...)
			if code != 1 {
				t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
			}
			envelope := decodeInspectEnvelope(t, stdout)
			if envelope.Status != "error" || envelope.Data != nil {
				t.Fatalf("envelope = %+v", envelope)
			}
			if len(envelope.Diagnostics) != 1 {
				t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
			}
			diagnostic := envelope.Diagnostics[0]
			if diagnostic.Severity != "error" || diagnostic.Code != "VMAKE_E_OPTION" || diagnostic.Message == "" {
				t.Fatalf("diagnostic = %+v", diagnostic)
			}
			if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "Error: ") {
				t.Fatalf("stderr = %q, want one human error line", stderr)
			}
			if _, err := os.Stat(configPath); !os.IsNotExist(err) {
				t.Fatalf("%s was written by an invalid --set: %v", configPath, err)
			}
		})
	}
}

func TestInspectSetInvalidBoolValue(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("gate").
			SetType(api.OptionBool).
			SetDefault(true).
			SetOnApply(func(ctx *api.ConfigContext, val any) {
				if !ctx.Bool("gate") {
					return
				}
			})
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetVoid)
	})
}
`)
	stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/gate=maybe")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "error" || envelope.Data != nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_E_OPTION" || envelope.Diagnostics[0].Severity != "error" {
		t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
	}
	if !strings.Contains(envelope.Diagnostics[0].Message, "invalid bool value") {
		t.Fatalf("diagnostic = %+v, want an invalid bool value message", envelope.Diagnostics[0])
	}
	if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "Error: ") || strings.Contains(stderr, "panic:") {
		t.Fatalf("stderr = %q, want one human error line", stderr)
	}
}

func TestInspectScriptErrorEnvelope(t *testing.T) {
	for _, test := range []struct {
		name   string
		script string
	}{
		{"load", "package main\n\nfunc Main(p *api.Package) {\n"},
		{"configure", `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) { panic("configure failure") })
}
`},
	} {
		t.Run(test.name, func(t *testing.T) {
			project := filepath.Join(t.TempDir(), "broken")
			writeExtensionFile(t, filepath.Join(project, "build.go"), test.script)
			stdout, stderr, code := runInspectCommand(t, project, "inspect")
			if code != 1 {
				t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
			}
			envelope := decodeInspectEnvelope(t, stdout)
			if envelope.Status != "error" || envelope.Data != nil {
				t.Fatalf("envelope = %+v", envelope)
			}
			if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_E_INSPECT" || envelope.Diagnostics[0].Severity != "error" {
				t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
			}
			lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
			if len(lines) == 0 || strings.Count(stderr, "Error: ") != 1 || !strings.HasPrefix(lines[len(lines)-1], "Error: ") {
				t.Fatalf("stderr = %q, want one human error line", stderr)
			}
		})
	}
}

func TestInspectTargetDeclarationWarning(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("broken") })
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetVoid).AddDeps("broken:*")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "broken", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) { panic("declaration failure") })
}
`)
	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
	}
	diagnostic := envelope.Diagnostics[0]
	if diagnostic.Severity != "warning" || diagnostic.Code != "VMAKE_W_TARGET_DECLARE" ||
		!strings.Contains(diagnostic.Message, "broken") || diagnostic.Fix == "" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	broken := inspectPackageNamed(t, envelope.Data, "broken")
	if !broken.Needed || broken.Targets != nil {
		t.Fatalf("broken package = %+v", broken)
	}
	if !strings.Contains(stderr, "Warning: cannot declare targets for broken") {
		t.Fatalf("stderr = %q, want one warning line", stderr)
	}
}

func TestInspectFixtures(t *testing.T) {
	fixtures := []string{
		filepath.Join("..", "..", "test_data", "01_simple_c"),
		filepath.Join("..", "..", "test_data", "03_multi_target"),
		filepath.Join("..", "..", "test_data", "04_multi_module"),
	}
	if runtime.GOOS == "linux" {
		fixtures = append(fixtures, filepath.Join("..", "..", "test_linux", "17_firmware"))
	}
	for _, fixture := range fixtures {
		root, err := filepath.Abs(fixture)
		if err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			t.Skipf("fixture %s unavailable", root)
		}
		t.Run(filepath.Base(root), func(t *testing.T) {
			before, err := os.ReadFile(filepath.Join(root, ".vmake", "config.json"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			stdout, stderr, code := runInspectCommand(t, root, "inspect")
			if code != 0 || stderr != "" {
				t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
			}
			envelope := decodeInspectEnvelope(t, stdout)
			if envelope.SchemaVersion != 1 || envelope.Command != "inspect" || envelope.Status != "ok" || envelope.Data == nil {
				t.Fatalf("envelope = %+v", envelope)
			}
			data := envelope.Data
			if data.Root != root || data.Mode == "" || data.Toolchain == "" || data.CompileDatabase == "" || data.BuildReport == "" {
				t.Fatalf("data header = %+v", data)
			}
			if len(data.Packages) == 0 || len(data.Roots) == 0 {
				t.Fatalf("empty model: roots=%v packages=%d", data.Roots, len(data.Packages))
			}
			for _, pkg := range data.Packages {
				if pkg.Name == "" || pkg.Kind == "" || pkg.SourceDir == "" || pkg.Deps == nil {
					t.Fatalf("incomplete package entry: %+v", pkg)
				}
				for _, target := range pkg.Targets {
					if target.Name == "" || target.Kind == "" {
						t.Fatalf("incomplete target entry: %+v", target)
					}
				}
			}
			if data.ToolchainInfo == nil || data.ToolchainInfo.Name == "" || data.ToolchainInfo.CC == "" {
				t.Fatalf("toolchainInfo = %+v", data.ToolchainInfo)
			}
			if data.Global == nil || data.Graph == nil {
				t.Fatalf("global = %v, graph = %v", data.Global, data.Graph)
			}
			again, stderr, code := runInspectCommand(t, root, "inspect")
			if code != 0 || stderr != "" {
				t.Fatalf("second inspect exit = %d, stderr = %q", code, stderr)
			}
			if again != stdout {
				t.Fatal("consecutive inspect runs are not byte-identical")
			}
			after, err := os.ReadFile(filepath.Join(root, ".vmake", "config.json"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("inspect modified .vmake/config.json")
			}
		})
	}
}

func TestInspectCommandRegistration(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"inspect"})
	if err != nil {
		t.Fatalf("inspect not registered: %v", err)
	}
	if cmd.Name() != "inspect" {
		t.Fatalf("command = %q", cmd.Name())
	}
	if cmd.Flags().Lookup("set") == nil {
		t.Fatal("inspect has no --set flag")
	}
	ref := generateCLIRef(RootCmd)
	if !strings.Contains(ref, "vmake inspect") || !strings.Contains(ref, "--set <value>") {
		t.Fatalf("CLI reference missing inspect:\n%s", ref)
	}
}
